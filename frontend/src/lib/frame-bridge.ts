/** 文件作用说明：校验装修运行来源与握手边界，避免跨窗口或协议伪造获得访客桥能力。 */
import type { ChatMessage } from "./types";
import { ApiError } from "./api.ts";
type BridgeHello = { type: "goodchat:init"; version: 1; nonce: string; release_id: string };
const bridgeFields: Record<string, readonly string[]> = {
  getState: [],
  sendText: ["text", "content", "client_message_id", "reply_to_id"],
  withdrawMessage: ["message_id"],
  typing: ["active", "is_typing"],
  upload: ["file", "blob", "name"],
  download: ["attachment_id"],
  saveAttachment: ["attachment_id"],
  read: ["last_read_seq"],
  loadOlder: [],
};
/** validateBridgeRequest 限制RPC方法和参数，引用和撤回只能指向当前授权可见消息。 */
export function validateBridgeRequest(
  method: string,
  payload: Record<string, unknown>,
  messages: ChatMessage[],
  visitorId: string,
): void {
  if (!Object.hasOwn(bridgeFields, method)) throw new Error("当前页面无权调用此方法");
  if (Object.keys(payload).some((key) => !bridgeFields[method].includes(key)))
    throw new Error("当前方法包含未授权参数");
  if (method === "download" || method === "saveAttachment") {
    visibleBridgeAttachment(messages, payload.attachment_id);
    return;
  }
  const target = method === "withdrawMessage" ? payload.message_id : payload.reply_to_id;
  if (method !== "withdrawMessage" && target === undefined) return;
  const item = messages.find(
    (message) => message.id === target && message.audience !== "internal" && !message.withdrawn_at,
  );
  if (typeof target !== "string" || !item) throw new Error("目标消息不属于当前可见会话");
  if (
    method === "withdrawMessage" &&
    (item.sender_kind !== "visitor" || item.sender_id !== visitorId)
  )
    throw new Error("只能撤回本人消息");
}
/** visibleBridgeAttachment 只授权当前会话已加载、公开且未撤回消息中的附件。 */
export function visibleBridgeAttachment(messages: ChatMessage[], attachmentId: unknown) {
  const item = messages.find(
    (message) =>
      !message.withdrawn_at &&
      message.audience === "public" &&
      message.attachment?.id === attachmentId,
  );
  if (typeof attachmentId !== "string" || !item?.attachment)
    throw new Error("附件不属于当前可见会话，或已经撤回");
  return item.attachment;
}
/** requestBridgeAttachment 在用户确认后重新核对最新消息，凭证只放请求头并支持撤销请求。 */
export async function requestBridgeAttachment(
  attachmentId: string,
  getMessages: () => ChatMessage[],
  token?: string,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const attachment = visibleBridgeAttachment(getMessages(), attachmentId);
  const timeout = AbortSignal.timeout(30000);
  const response = await fetch(`/api/v1/attachments/${encodeURIComponent(attachmentId)}`, {
    credentials: "same-origin",
    cache: "no-store",
    headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
  });
  if (!response.ok) {
    const error = await response.json().catch(() => null);
    throw new ApiError(
      error?.message || "附件已撤回、不可用或无权读取",
      response.status,
      error?.code || "ATTACHMENT_DOWNLOAD_FAILED",
      error?.request_id,
    );
  }
  const blob = await response.blob();
  signal?.throwIfAborted();
  visibleBridgeAttachment(getMessages(), attachmentId);
  if (!blob.size) throw new Error("附件内容为空，无法发起下载");
  return { blob, name: attachment.file_name, content_type: attachment.content_type };
}
/** safeRenderURL 只接受无内嵌账号且与可信父页面不同源的 HTTP 页面。 */
export function safeRenderURL(value: string, platformOrigin: string): URL | null {
  try {
    const url = new URL(value);
    if (
      !["http:", "https:"].includes(url.protocol) ||
      url.origin === platformOrigin ||
      (platformOrigin.startsWith("https:") && url.protocol !== "https:") ||
      url.username ||
      url.password
    )
      return null;
    return url;
  } catch {
    return null;
  }
}
/** validBridgeHandshake 同时检查精确来源、窗口、协议版本、发布编号与随机握手值。 */
export function validBridgeHandshake(
  input: unknown,
  origin: string,
  expectedOrigin: string,
  sourceMatches: boolean,
  releaseId: string,
): input is BridgeHello {
  if (
    origin !== expectedOrigin ||
    !sourceMatches ||
    !input ||
    typeof input !== "object" ||
    Array.isArray(input)
  )
    return false;
  const value = input as Record<string, unknown>;
  return (
    value.type === "goodchat:init" &&
    value.version === 1 &&
    value.release_id === releaseId &&
    typeof value.nonce === "string" &&
    /^[A-Za-z0-9+/_=-]{16,128}$/.test(value.nonce)
  );
}
/** publicBridgeMessage 只导出访客可见消息及本人标记，不暴露后台身份或附件凭证。 */
export function publicBridgeMessage(item: ChatMessage, visitorId: string) {
  return {
    id: item.id,
    seq: item.seq || item.event_seq,
    is_own: item.sender_kind === "visitor" && item.sender_id === visitorId,
    sender_kind: item.sender_kind,
    sender_name: item.sender_name,
    message_type: item.message_type,
    content: item.withdrawn_at ? "" : item.content,
    text: item.withdrawn_at ? "" : item.content,
    created_at: item.created_at,
    withdrawn_at: item.withdrawn_at,
    reply: item.reply
      ? {
          id: item.reply.id,
          content: item.reply.withdrawn ? "" : item.reply.content,
          withdrawn: item.reply.withdrawn,
        }
      : undefined,
    attachment:
      item.withdrawn_at || !item.attachment
        ? undefined
        : {
            id: item.attachment.id,
            file_name: item.attachment.file_name,
            content_type: item.attachment.content_type,
            size_bytes: item.attachment.size_bytes,
          },
  };
}
