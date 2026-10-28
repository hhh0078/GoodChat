/** 文件作用说明：访客初始历史不响铃，首次交互后只为新收到的公开人工客服消息播放租户提示音。 */
"use client";
import { useEffect, useRef } from "react";
import type { ChatMessage } from "@/lib/types";
import { useNotificationAudio } from "./use-notification-audio";
import { reminderWatermark } from "@/lib/message-reminders";

/** useVisitorReminders 按会话与消息编号去重，断线补拉新消息可提示，切换会话和历史加载不追播。 */
export function useVisitorReminders(
  messages: ChatMessage[],
  options: { enabled: boolean; loading: boolean; conversationId: string; sound?: string },
) {
  const audio = useNotificationAudio(options.enabled);
  const scope = useRef("");
  const watermark = useRef<string | null>(null);
  useEffect(() => {
    if (scope.current !== options.conversationId) {
      audio.stop();
      scope.current = options.conversationId;
      watermark.current = null;
    }
    if (!options.enabled || options.loading) return;
    const result = reminderWatermark(messages, watermark.current);
    watermark.current = result.latest;
    if (result.notify) void audio.play(options.sound, 0.55);
  }, [
    messages,
    options.enabled,
    options.loading,
    options.conversationId,
    options.sound,
    audio.play,
    audio.stop,
  ]);
}
