/** 文件作用说明：读取成员提醒偏好，在用户主动启用本机后执行真实通知、提示音和语音播报。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
type Preferences = {
  sound_enabled: boolean;
  speech_enabled: boolean;
  system_enabled: boolean;
  content_preview: boolean;
  muted: boolean;
  volume: number;
};
/** useReminders 将免打扰、音量与隐私预览统一应用到实际新消息事件。 */
export function useReminders(tenantId: string) {
  const prefs = useRef<Preferences | null>(null);
  const audio = useRef<AudioContext | null>(null);
  const enabled = useRef(false);
  const seen = useRef(new Set<string>());
  const [active, setActive] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    prefs.current = null;
    seen.current.clear();
    api<Preferences>("/tenant/notification-preferences", { tenantId, signal: controller.signal })
      .then((value) => {
        prefs.current = value;
      })
      .catch(() => {
        prefs.current = null;
      });
    return () => {
      controller.abort();
    };
  }, [tenantId]);
  useEffect(
    () => () => {
      void audio.current?.close();
      if (typeof speechSynthesis !== "undefined") speechSynthesis.cancel();
    },
    [],
  );
  /** enable 只在明确点击时解锁声音与申请浏览器通知权限，不播放测试声音。 */
  async function enable() {
    if (typeof AudioContext !== "undefined") {
      audio.current ||= new AudioContext();
      await audio.current.resume();
    }
    if (typeof Notification !== "undefined" && Notification.permission === "default")
      await Notification.requestPermission();
    enabled.current = true;
    setActive(true);
  }
  /** receive 对真实访客新消息去重，后台通知受浏览器权限和服务端偏好共同约束。 */
  function receive(event: {
    id?: string;
    conversation_id?: string;
    event_type?: string;
    payload?: { sender_kind?: string; content?: string };
  }) {
    const settings = prefs.current;
    if (
      !enabled.current ||
      !settings ||
      settings.muted ||
      event.event_type !== "message.created" ||
      event.payload?.sender_kind !== "visitor" ||
      !event.id ||
      seen.current.has(event.id)
    )
      return;
    seen.current.add(event.id);
    if (seen.current.size > 500) seen.current.delete(seen.current.values().next().value!);
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
    if (settings.sound_enabled && audio.current?.state === "running") {
      const oscillator = audio.current.createOscillator();
      const gain = audio.current.createGain();
      oscillator.frequency.value = 660;
      gain.gain.value = Math.max(0, Math.min(settings.volume, 1)) * 0.12;
      oscillator.connect(gain);
      gain.connect(audio.current.destination);
      oscillator.start();
      oscillator.stop(audio.current.currentTime + 0.12);
    }
    if (
      settings.speech_enabled &&
      typeof speechSynthesis !== "undefined" &&
      !speechSynthesis.speaking
    ) {
      const speech = new SpeechSynthesisUtterance(
        settings.content_preview ? preview : "你有一条新的访客消息",
      );
      speech.lang = "zh-CN";
      speech.volume = Math.max(0, Math.min(settings.volume, 1));
      speechSynthesis.speak(speech);
    }
  }
  return { active, enable, receive };
}
