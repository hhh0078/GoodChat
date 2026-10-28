/** 文件作用说明：默认接收新消息提醒，以音量和系统通知偏好控制实际播放，并在首次交互解锁浏览器声音。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
export type ReminderPreferences = {
  sound_enabled: boolean;
  speech_enabled: boolean;
  system_enabled: boolean;
  content_preview: boolean;
  muted: boolean;
  volume: number;
};
type ReminderEvent = {
  id?: string;
  conversation_id?: string;
  event_type?: string;
  payload?: { sender_kind?: string; content?: string };
};
/** useReminders 将音量与隐私偏好用于真实访客新消息，不再设置工作台启用开关。 */
export function useReminders(tenantId: string) {
  const prefs = useRef<ReminderPreferences | null>(null);
  const [preferences, setPreferences] = useState<ReminderPreferences | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [audioReady, setAudioReady] = useState(false);
  const audio = useRef<AudioContext | null>(null);
  const seen = useRef(new Set<string>());
  useEffect(() => {
    const controller = new AbortController();
    prefs.current = null;
    setPreferences(null);
    seen.current.clear();
    setError(null);
    api<ReminderPreferences>("/tenant/notification-preferences", {
      tenantId,
      signal: controller.signal,
    })
      .then((value) => {
        if (!controller.signal.aborted) {
          prefs.current = value;
          setPreferences(value);
        }
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      });
    return () => controller.abort();
  }, [tenantId]);
  useEffect(() => {
    /** unlock 由真实用户交互解锁声音，不自动申请系统通知或播放测试音。 */
    async function unlock() {
      if (typeof AudioContext === "undefined") return;
      try {
        audio.current ||= new AudioContext();
        if (audio.current.state === "suspended") await audio.current.resume();
        setAudioReady(audio.current.state === "running");
      } catch {
        setAudioReady(false);
      }
    }
    document.addEventListener("pointerdown", unlock);
    document.addEventListener("keydown", unlock);
    return () => {
      document.removeEventListener("pointerdown", unlock);
      document.removeEventListener("keydown", unlock);
      void audio.current?.close();
      audio.current = null;
      if (typeof speechSynthesis !== "undefined") speechSynthesis.cancel();
    };
  }, []);
  /** updatePreferences 保存真实偏好，音量调整后提示音可用，零音量即静音。 */
  async function updatePreferences(values: Partial<ReminderPreferences>) {
    const saved = await api<ReminderPreferences>("/tenant/notification-preferences", {
      tenantId,
      method: "PATCH",
      bodyJSON: values,
    });
    prefs.current = saved;
    setPreferences(saved);
    setError(null);
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
    await updatePreferences({ system_enabled: enabled });
  }
  /** receive 按事件编号去重，零音量不播放声音但仍遵循独立系统通知选择。 */
  function receive(event: ReminderEvent) {
    const settings = prefs.current;
    if (
      !settings ||
      event.event_type !== "message.created" ||
      event.payload?.sender_kind !== "visitor" ||
      !event.id ||
      seen.current.has(event.id)
    )
      return;
    seen.current.add(event.id);
    if (seen.current.size > 500) seen.current.delete(seen.current.values().next().value!);
    if (settings.muted) return;
    const preview = settings.content_preview
      ? event.payload.content?.slice(0, 100) || "访客发送了附件消息"
      : "有访客发送了新消息，请回到工作台查看。";
    if (
      settings.system_enabled &&
      document.visibilityState !== "visible" &&
      typeof Notification !== "undefined" &&
      Notification.permission === "granted"
    )
      new Notification("GoodChat 新咨询消息", { body: preview, tag: event.conversation_id });
    const volume = Math.max(0, Math.min(settings.volume, 1));
    if (volume > 0 && settings.sound_enabled && audio.current?.state === "running") {
      const oscillator = audio.current.createOscillator();
      const gain = audio.current.createGain();
      oscillator.frequency.value = 660;
      gain.gain.value = volume * 0.12;
      oscillator.connect(gain);
      gain.connect(audio.current.destination);
      oscillator.onended = () => {
        oscillator.disconnect();
        gain.disconnect();
      };
      oscillator.start();
      oscillator.stop(audio.current.currentTime + 0.12);
    }
    if (
      volume > 0 &&
      settings.speech_enabled &&
      typeof speechSynthesis !== "undefined" &&
      !speechSynthesis.speaking
    ) {
      const speech = new SpeechSynthesisUtterance(
        settings.content_preview ? preview : "你有一条新的访客消息",
      );
      speech.lang = "zh-CN";
      speech.volume = volume;
      speechSynthesis.speak(speech);
    }
  }
  return { preferences, error, audioReady, updatePreferences, setSystemNotifications, receive };
}
