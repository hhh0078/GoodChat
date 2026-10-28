/** 文件作用说明：用持久消息序号识别新人工客服消息，历史分页、初始快照及重复补拉不触发提示音。 */
import type { ChatMessage } from "./types";

/** reminderWatermark 返回当前最大消息序号与是否出现新人工消息，不把字符串序号转为可能丢精度的Number。 */
export function reminderWatermark(messages: ChatMessage[], previous: string | null) {
  const before = previous === null ? null : BigInt(previous);
  let latest = before || 0n;
  let notify = false;
  for (const message of messages) {
    const value = String(message.seq || message.event_seq || "0");
    if (!/^\d+$/.test(value)) continue;
    const seq = BigInt(value);
    if (seq > latest) latest = seq;
    if (
      before !== null &&
      seq > before &&
      message.sender_kind === "agent" &&
      message.audience === "public" &&
      !message.withdrawn_at
    )
      notify = true;
  }
  return { latest: latest.toString(), notify };
}
