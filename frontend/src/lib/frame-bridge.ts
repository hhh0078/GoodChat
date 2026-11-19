/** 文件作用说明：校验装修运行来源与握手边界，避免跨窗口或协议伪造获得访客桥能力。 */
import { preferenceHeaders, translate as tr } from "../i18n/runtime.ts";
import { ApiError } from "./api.ts";
import { messageDisplay, type MessageDisplayContext } from "./message-display.ts";
import type { ChatMessage } from "./types";
type BridgeHello = {
  type: "goodchat:init";
  version: 1;
  nonce: string;
  release_id: string;
};
const bridgeFields: Record<string, readonly string[]> = {
  getState: [],
  getAvatar: ["message_id"],
  submitForm: ["message_id", "values", "client_message_id"],
  sendText: ["text", "content", "client_message_id", "reply_to_id"],
  withdrawMessage: ["message_id"],
  typing: ["active", "is_typing"],
  upload: ["file", "blob", "name"],
  download: ["attachment_id"],
  saveAttachment: ["attachment_id"],
  read: ["last_read_seq"],
  loadOlder: [],
  openLeaveMessage: [],
  openEvaluation: [],
};
/** validateBridgeRequest 限制RPC方法和参数，引用和撤回只能指向当前授权可见消息。 */
export function validateBridgeRequest(
  method: string,
  payload: Record<string, unknown>,
  messages: ChatMessage[],
  visitorId: string,
  displayContext?: MessageDisplayContext,
): void {
  if (!Object.hasOwn(bridgeFields, method)) throw new Error(tr("当前页面无权调用此方法"));
  if (Object.keys(payload).some((key) => !bridgeFields[method].includes(key)))
    throw new Error(tr("当前方法包含未授权参数"));
  if (method === "getAvatar") {
    visibleBridgeAvatar(messages, payload.message_id, displayContext);
    return;
  }
  if (method === "download" || method === "saveAttachment") {
    visibleBridgeAttachment(messages, payload.attachment_id);
    return;
  }
  if (method === "submitForm") {
    const target = messages.find(
      (m) =>
        m.id === payload.message_id &&
        m.audience !== "internal" &&
        !m.withdrawn_at &&
        m.message_type === "form",
    );
    if (
      !target ||
      !payload.values ||
      typeof payload.values !== "object" ||
      Array.isArray(payload.values) ||
      Object.keys(payload.values).length > 20 ||
      Object.values(payload.values).some((v) => typeof v !== "string" || v.length > 10000)
    )
      throw new Error(tr("表单提交数据无效"));
    return;
  }
  const target = method === "withdrawMessage" ? payload.message_id : payload.reply_to_id;
  if (method !== "withdrawMessage" && target === undefined) return;
  const item = messages.find(
    (message) => message.id === target && message.audience !== "internal" && !message.withdrawn_at,
  );
  if (typeof target !== "string" || !item) throw new Error(tr("目标消息不属于当前可见会话"));
  if (
    method === "withdrawMessage" &&
    (item.sender_kind !== "visitor" || item.sender_id !== visitorId)
  )
    throw new Error(tr("只能撤回本人消息"));
}
/** visibleBridgeAvatar 将头像读取绑定到当前公开消息，只允许平台内置或本平台受控上传图片路径。 */
export function visibleBridgeAvatar(
  messages: ChatMessage[],
  messageId: unknown,
  displayContext?: MessageDisplayContext,
): string {
  const item = messages.find(
    (message) => message.id === messageId && message.audience === "public" && !message.withdrawn_at,
  );
  const value = item
    ? messageDisplay(item, { kind: "visitor", actorId: "" }, displayContext).avatarURL
    : undefined;
  if (
    typeof messageId !== "string" ||
    typeof value !== "string" ||
    !(
      value === "/logo/icon-192.png" ||
      /^\/images\/avatars\/default-(0[1-9]|[1-4][0-9]|50)\.png$/.test(value) ||
      /^\/api\/v1\/avatar-assets\/[a-f\d]{8}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{12}$/i.test(
        value,
      )
    )
  )
    throw new Error(tr("头像不属于当前可见消息或平台受控图片"));
  return value;
}
/** requestBridgeAvatar 在可信父层读取公开PNG字节，使隔离页面可创建本来源Blob，无需放宽图片CSP。 */
export async function requestBridgeAvatar(
  messageId: string,
  getMessages: () => ChatMessage[],
  signal?: AbortSignal,
  getDisplayContext?: () => MessageDisplayContext | undefined,
): Promise<Blob> {
  signal?.throwIfAborted();
  const path = visibleBridgeAvatar(getMessages(), messageId, getDisplayContext?.());
  const timeout = AbortSignal.timeout(30000);
  const response = await fetch(path, {
    credentials: "omit",
    cache: "force-cache",
    redirect: "error",
    signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
  });
  if (!response.ok || Number(response.headers.get("content-length")) > 256 * 1024)
    throw new Error(tr("头像暂时无法读取"));
  const blob = await response.blob();
  signal?.throwIfAborted();
  if (
    !blob.size ||
    blob.size > 256 * 1024 ||
    blob.type.split(";")[0] !== "image/png" ||
    visibleBridgeAvatar(getMessages(), messageId, getDisplayContext?.()) !== path
  )
    throw new Error(tr("头像格式或当前消息权限已变化"));
  return blob;
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
    throw new Error(tr("附件不属于当前可见会话，或已经撤回"));
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
    headers: { ...preferenceHeaders(), ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
  });
  if (!response.ok) {
    const error = await response.json().catch(() => null);
    throw new ApiError(
      error?.message || tr("附件已撤回、不可用或无权读取"),
      response.status,
      error?.code || "ATTACHMENT_DOWNLOAD_FAILED",
      error?.request_id,
    );
  }
  const blob = await response.blob();
  signal?.throwIfAborted();
  visibleBridgeAttachment(getMessages(), attachmentId);
  if (!blob.size) throw new Error(tr("附件内容为空，无法发起下载"));
  return { blob, name: attachment.file_name, content_type: attachment.content_type };
}
/** safeRenderURL 校验渲染地址，同域仅开放发布路径；所有调用方必须使用无同源权限的沙箱。 */
export function safeRenderURL(value: string, platformOrigin: string): URL | null {
  try {
    const url = new URL(value);
    if (
      !["http:", "https:"].includes(url.protocol) ||
      (url.origin === platformOrigin &&
        !/^\/render\/[a-f\d]{8}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{12}$/i.test(
          url.pathname,
        )) ||
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
/** validBridgeHandshake 校验不透明沙箱来源、精确窗口、协议版本、发布编号与随机值。 */
export function validBridgeHandshake(
  input: unknown,
  origin: string,
  expectedOrigin: string,
  sourceMatches: boolean,
  releaseId: string,
): input is BridgeHello {
  if (
    origin !== "null" ||
    expectedOrigin !== "null" ||
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
/** publicAvatarURL 仅导出受控平台头像或不带凭证的HTTPS展示地址。 */
function publicAvatarURL(value: unknown): string | undefined {
  if (typeof value !== "string" || !value) return undefined;
  if (
    value === "/logo/icon-192.png" ||
    /^\/images\/avatars\/default-\d{2}\.png$/.test(value) ||
    /^\/api\/v1\/avatar-assets\/[a-f\d-]{36}$/i.test(value)
  )
    return value;
  try {
    const url = new URL(value);
    return url.protocol === "https:" && !url.username && !url.password ? url.href : undefined;
  } catch {
    return undefined;
  }
}
/** publicBridgeMessage 只导出访客可见消息及本人标记，不暴露后台身份或附件凭证。 */
export function publicBridgeMessage(
  item: ChatMessage,
  visitorId: string,
  displayContext?: MessageDisplayContext,
) {
  const display = messageDisplay(item, { kind: "visitor", actorId: visitorId }, displayContext);
  return {
    id: item.id,
    seq: item.seq || item.event_seq,
    is_own: item.sender_kind === "visitor" && item.sender_id === visitorId,
    client_message_id:
      item.sender_kind === "visitor" && item.sender_id === visitorId
        ? item.client_message_id
        : undefined,
    sender_kind: display.senderKind,
    sender_name: display.senderName,
    sender_avatar_url: publicAvatarURL(display.avatarURL),
    message_type: item.message_type,
    form_fields: item.withdrawn_at ? undefined : item.form_fields,
    form_request_id: item.form_request_id,
    form_values: item.withdrawn_at ? undefined : item.form_values,
    content: item.withdrawn_at ? "" : item.content,
    text: item.withdrawn_at ? "" : item.content,
    created_at: item.created_at,
    withdrawn_at: item.withdrawn_at,
    source: !display.automatic && typeof item.source === "string" ? item.source : undefined,
    options:
      !item.withdrawn_at &&
      (item.sender_kind === "system" || item.sender_kind === "agent") &&
      Array.isArray(item.options)
        ? item.options
            .slice(0, 8)
            .filter(
              (option) =>
                option &&
                typeof option.id === "string" &&
                typeof option.label === "string" &&
                typeof option.value === "string" &&
                typeof option.client_message_id === "string" &&
                /^[\da-f]{8}-[\da-f]{4}-[\da-f]{4}-[\da-f]{4}-[\da-f]{12}$/i.test(
                  option.client_message_id,
                ),
            )
            .map((option) => ({
              id: option.id,
              label: option.label,
              value: option.value,
              client_message_id: option.client_message_id,
            }))
        : undefined,
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
