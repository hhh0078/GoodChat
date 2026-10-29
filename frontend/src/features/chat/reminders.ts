/** 文件作用说明：按服务端持久未读原子领取提醒，离开工作台仍重复提示，遵守成员音量和通知偏好。 */
"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import {
  createReminderRequests,
  reminderEventInvalidates,
  systemNotificationPreferences,
} from "@/lib/reminder-state";
import type { RealtimeEvent } from "@/lib/realtime/client";
import { useNotificationAudio } from "./use-notification-audio";
export type ReminderPreferences = {
  sound_enabled: boolean;
  system_enabled: boolean;
  content_preview: boolean;
  muted: boolean;
  volume: number;
};
type ReminderItem = { conversation_id: string; reminder_key: string; preview: string };
type Claimed = {
  items: ReminderItem[];
  pending_count: number;
  repeat_unread_seconds: number;
  notification_sound_id: string;
};

/** useReminders 由真实未读与归属决定是否提醒，多个标签通过服务端领取避免同一周期叠响。 */
export function useReminders(tenantId: string, enabled = true, memberId = "") {
  const prefs = useRef<ReminderPreferences | null>(null);
  const [preferences, setPreferences] = useState<ReminderPreferences | null>(null);
  const [error, setError] = useState<unknown>(null);
  const audio = useNotificationAudio(enabled);
  const poll = useRef<() => Promise<void>>(async () => {});
  const epoch = useRef(0),
    consumed = useRef(0);
  const seen = useRef(new Set<string>());
  const eventReceiver = useRef<(event: RealtimeEvent) => void>(() => {});
  const activeScope = useRef<{
    tenantId: string;
    memberId: string;
    controller: AbortController;
    requests: ReturnType<typeof createReminderRequests>;
    cancel: () => void;
  } | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    const requests = createReminderRequests();
    const pendingConversations = new Set<string>();
    /** cancel 立即停止声音和旧领取响应，保留服务端持久未读供后续核验。 */
    function cancel() {
      requests.invalidateAttention();
      audio.stop();
    }
    const scope = { tenantId, memberId, controller, requests, cancel };
    activeScope.current = scope;
    prefs.current = null;
    setPreferences(null);
    setError(null);
    seen.current.clear();
    epoch.current = consumed.current = 0;
    if (!enabled || !tenantId)
      return () => {
        controller.abort();
        cancel();
        if (activeScope.current === scope) activeScope.current = null;
      };
    let running = false;
    let claiming = false;
    let rerun = false;
    let preferencesCheckedAt = 0;
    /** notify 将同周期多个咨询合成一次提示音和通知，不把客服静音改成开启。 */
    function notify(result: Claimed) {
      const settings = prefs.current;
      if (result.pending_count === 0) {
        pendingConversations.clear();
        cancel();
        return;
      }
      if (!settings || settings.muted || !result.items.length) return;
      for (const item of result.items) pendingConversations.add(item.conversation_id);
      const preview = settings.content_preview
        ? result.items[0].preview || "访客发送了附件消息"
        : "有咨询尚未查看，请回到工作台处理。";
      const body =
        result.items.length > 1 ? `${preview}（共${result.items.length}条待处理咨询）` : preview;
      if (
        settings.system_enabled &&
        document.visibilityState !== "visible" &&
        typeof Notification !== "undefined" &&
        Notification.permission === "granted"
      ) {
        try {
          new Notification("GoodChat 接待提醒", { body, tag: "goodchat-pending-attention" });
        } catch (problem) {
          // 单个通知通道故障不能阻断已可用的声音通道。
          setError(problem);
        }
      }
      const volume = Math.max(0, Math.min(1, settings.volume));
      if (volume > 0 && settings.sound_enabled)
        void audio.play(result.notification_sound_id, volume);
    }
    /** claim 使用服务端最早未读期限，断网后重新领取，不依赖浏览器本地计时起点。 */
    async function claim() {
      if (controller.signal.aborted || !enabled || !tenantId || requests.preferenceChanging) return;
      if (running) {
        rerun = true;
        return;
      }
      running = true;
      const version = epoch.current;
      try {
        if (!prefs.current || Date.now() - preferencesCheckedAt >= 30000) {
          const currentRead = requests.preferenceRead();
          const value = await api<ReminderPreferences>("/tenant/notification-preferences", {
            tenantId,
            signal: controller.signal,
          });
          if (controller.signal.aborted || !currentRead()) return;
          prefs.current = value;
          preferencesCheckedAt = Date.now();
          setPreferences(value);
          if (value.muted || value.volume <= 0 || !value.sound_enabled) audio.stop();
        }
        const settings = prefs.current;
        const canSound =
          !!settings && settings.volume > 0 && audio.isReady() && settings.sound_enabled;
        const canNotify =
          !!settings?.system_enabled &&
          document.visibilityState !== "visible" &&
          typeof Notification !== "undefined" &&
          Notification.permission === "granted";
        // 无可用播放通道的标签不抢占提醒，让已解锁的另一标签或设备领取。
        if (!settings || settings.muted || (!canSound && !canNotify)) return;
        const currentClaim = requests.attentionRead();
        claiming = true;
        const result = await api<Claimed>("/agent/attention-reminders/claim", {
          tenantId,
          method: "POST",
          signal: controller.signal,
          bodyJSON: { initial: version !== consumed.current },
        });
        if (controller.signal.aborted || !currentClaim()) return;
        consumed.current = version;
        setError(null);
        notify(result);
      } catch (problem) {
        if (!controller.signal.aborted) setError(problem);
      } finally {
        claiming = false;
        running = false;
        if (rerun) {
          rerun = false;
          void claim();
        }
      }
    }
    poll.current = claim;
    /** receiveEvent 处理本人已读和咨询失效，不能用旧领取响应重新播放已取消的声音。 */
    function receiveEvent(event: RealtimeEvent) {
      if (!event.id || seen.current.has(event.id)) return;
      const invalidates = reminderEventInvalidates(event, memberId);
      const refreshes =
        event.event_type === "assignment.changed" ||
        (event.event_type === "message.created" && event.payload?.sender_kind === "visitor");
      if (!invalidates && !refreshes) return;
      seen.current.add(event.id);
      if (seen.current.size > 500) seen.current.delete(seen.current.values().next().value!);
      if (invalidates) {
        if (
          event.event_type === "read.updated" ||
          claiming ||
          !event.conversation_id ||
          pendingConversations.has(event.conversation_id)
        )
          cancel();
        if (event.conversation_id) pendingConversations.delete(event.conversation_id);
      }
      if (refreshes) epoch.current++;
      void claim();
    }
    eventReceiver.current = receiveEvent;
    void claim();
    let worker: Worker | undefined;
    let timer: ReturnType<typeof setInterval> | undefined;
    /** fallbackClock 在Worker不可用时保留普通定时核验，前台恢复仍会立即补查。 */
    function fallbackClock() {
      worker?.terminate();
      worker = undefined;
      timer ||= setInterval(() => void claim(), 2000);
    }
    try {
      worker = new Worker(new URL("./attention.worker.ts", import.meta.url), {
        type: "module",
        name: "goodchat-attention-clock",
      });
      worker.onmessage = () => void claim();
      worker.onerror = fallbackClock;
    } catch {
      fallbackClock();
    }
    /** resume 前台恢复或网络回归时立即读持久状态，避免后台定时器被限流后还等待旧计时。 */
    function resume() {
      void claim();
    }
    /** preferencesChanged 立即应用同页面设置更改，其他设备偏好由周期刷新收敛。 */
    function preferencesChanged(event: Event) {
      if ((event as CustomEvent<{ tenantId: string }>).detail?.tenantId !== tenantId) return;
      const finish = requests.preferenceChange();
      finish();
      prefs.current = null;
      setPreferences(null);
      cancel();
      preferencesCheckedAt = 0;
      void claim();
    }
    window.addEventListener("online", resume);
    window.addEventListener("focus", resume);
    document.addEventListener("visibilitychange", resume);
    window.addEventListener("goodchat:notification-preferences", preferencesChanged);
    return () => {
      controller.abort();
      cancel();
      if (timer) clearInterval(timer);
      worker?.terminate();
      poll.current = async () => {};
      eventReceiver.current = () => {};
      if (activeScope.current === scope) activeScope.current = null;
      window.removeEventListener("online", resume);
      window.removeEventListener("focus", resume);
      document.removeEventListener("visibilitychange", resume);
      window.removeEventListener("goodchat:notification-preferences", preferencesChanged);
    };
  }, [tenantId, memberId, enabled, audio.play, audio.stop, audio.isReady]);
  useEffect(() => {
    if (audio.ready) void poll.current();
  }, [audio.ready]);
  /** updatePreferences 等服务端确认保存成员偏好，之后的提醒使用新音量。 */
  async function updatePreferences(values: Partial<ReminderPreferences>) {
    const scope = activeScope.current;
    if (!scope || scope.tenantId !== tenantId || scope.memberId !== memberId) return;
    const finish = scope.requests.preferenceChange();
    scope.cancel();
    try {
      const saved = await api<ReminderPreferences>("/tenant/notification-preferences", {
        tenantId,
        method: "PATCH",
        signal: scope.controller.signal,
        bodyJSON: values,
      });
      if (activeScope.current !== scope || scope.controller.signal.aborted || !finish()) return;
      prefs.current = saved;
      setPreferences(saved);
      setError(null);
    } catch (problem) {
      if (activeScope.current === scope && !scope.controller.signal.aborted && finish()) {
        // PATCH失败可能是响应丢失，重新读服务端事实，不能盲目恢复旧设置。
        prefs.current = null;
        setPreferences(null);
        setError(problem);
      }
      throw problem;
    } finally {
      finish();
      if (activeScope.current === scope && !scope.controller.signal.aborted) void poll.current();
    }
  }
  /** setSystemNotifications 仅在用户主动勾选时申请系统通知权限。 */
  async function setSystemNotifications(enabled: boolean) {
    if (enabled) {
      if (typeof Notification === "undefined") throw new Error("当前浏览器不支持系统通知");
      const permission =
        Notification.permission === "default"
          ? await Notification.requestPermission()
          : Notification.permission;
      if (permission !== "granted")
        throw new Error("系统通知尚未授权，请在浏览器站点设置中允许通知");
    }
    await updatePreferences(systemNotificationPreferences(enabled, prefs.current?.muted));
  }
  /** receive 将新消息与分配变更作为立即核验提示，不能凭WS消息替任何客服构造待处理状态。 */
  const receive = useCallback((event: RealtimeEvent) => eventReceiver.current(event), []);
  /** recover 让共享实时连接恢复后重新检查持久提醒。 */
  const recover = useCallback(() => poll.current(), []);
  return {
    preferences,
    error,
    audioReady: audio.ready,
    updatePreferences,
    setSystemNotifications,
    receive,
    recover,
  };
}
