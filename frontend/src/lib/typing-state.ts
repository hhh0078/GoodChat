/** 文件作用说明：按服务端固定期限和当前身份计算临时输入态，刷新不会延长AI等待期限。 */
import type { Conversation } from "./types";

/** peerTypingState 隐藏已到期、已暂停、已关闭及访客不可见的辅助草稿输入态。 */
export function peerTypingState(
  conversation: Conversation | null,
  kind: string,
  humanUntil: number,
  now: number,
) {
  const thinking = conversation?.context?.ai_thinking as
    { expires_at?: string; mode?: string } | undefined;
  if (!conversation || conversation.status === "closed") return { until: 0, isAIThinking: false };
  const aiUntil =
    thinking && !conversation.ai_paused && (kind === "agent" || thinking.mode === "auto")
      ? Date.parse(thinking.expires_at || "") || 0
      : 0;
  return {
    until: Math.max(humanUntil > now ? humanUntil : 0, aiUntil > now ? aiUntil : 0),
    isAIThinking: aiUntil > now,
  };
}
