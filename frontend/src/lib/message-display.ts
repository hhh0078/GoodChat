/** 文件作用说明：统一自动客服回复的显示身份和气泡方向，保留原始消息归属与人工操作权限。 */
import type { ChatMessage, Conversation } from "./types";

export type MessageDisplayContext = Pick<
  Conversation,
  "agent_name" | "agent_avatar_url" | "first_agent"
>;
type Viewer = { kind: string; actorId: string };

/** isAutomaticReply 仅将具有明确欢迎或关键词来源的公开系统消息视为客服自动答复。 */
export function isAutomaticReply(message: ChatMessage): boolean {
  return (
    message.sender_kind === "system" &&
    message.audience !== "internal" &&
    (message.source === "welcome" || message.source === "keyword")
  );
}

/** messageDisplay 根据观看身份生成展示信息，自动回复的右侧排布不会赋予本人撤回或人工已读权限。 */
export function messageDisplay(
  message: ChatMessage,
  viewer: Viewer,
  context?: MessageDisplayContext,
) {
  const automatic = isAutomaticReply(message);
  const own = message.sender_kind === viewer.kind && message.sender_id === viewer.actorId;
  const automaticAgent = message.automation_agent || context?.first_agent;
  const senderName = automatic
    ? automaticAgent?.name || (automaticAgent ? "客服" : context?.agent_name) || "客服"
    : message.sender_name ||
      ({ visitor: "访客", agent: "客服", system: "系统" } as Record<string, string>)[
        message.sender_kind
      ] ||
      "消息";
  const avatarURL = automatic
    ? automaticAgent?.avatar_url ||
      (automaticAgent ? undefined : context?.agent_avatar_url) ||
      undefined
    : message.sender_avatar_url;
  return {
    automatic,
    own,
    alignRight: own || (viewer.kind === "agent" && automatic),
    senderName,
    avatarURL,
    senderKind: automatic && viewer.kind === "visitor" ? "agent" : message.sender_kind,
    sourceLabel: automatic
      ? viewer.kind === "agent"
        ? `系统自动 · ${message.source === "welcome" ? "欢迎语" : "关键词回复"}`
        : ""
      : message.sender_kind === "system" && message.source
        ? "系统消息"
        : "",
  };
}
