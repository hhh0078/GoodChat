/** 文件作用说明：在浏览器会话存储中保留完整待确认消息，以原会话和稳定编号恢复重试，禁止保存凭证。 */
export type PendingMessage = {
  client_message_id: string;
  content: string;
  message_type: string;
  audience: string;
  attachment_id?: string;
  reply_to_id?: string;
};
type MessageStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;
const maxMessages = 100;
const maxCharacters = 1024 * 1024;

/** cleanMessage 严格提取可重试消息字段，存储中不会混入令牌或其他私有请求配置。 */
function cleanMessage(value: unknown): PendingMessage {
  if (!value || typeof value !== "object")
    throw new Error("待确认消息格式无效，请保留当前页面并联系管理员。");
  const item = value as Record<string, unknown>;
  if (
    typeof item.client_message_id !== "string" ||
    !item.client_message_id ||
    item.client_message_id.length > 100 ||
    typeof item.content !== "string" ||
    typeof item.message_type !== "string" ||
    !["text", "image", "audio", "file"].includes(item.message_type) ||
    typeof item.audience !== "string" ||
    !["public", "internal"].includes(item.audience)
  )
    throw new Error("待确认消息格式无效，请保留当前页面并联系管理员。");
  return {
    client_message_id: item.client_message_id,
    content: item.content,
    message_type: item.message_type,
    audience: item.audience,
    ...(typeof item.attachment_id === "string" ? { attachment_id: item.attachment_id } : {}),
    ...(typeof item.reply_to_id === "string" ? { reply_to_id: item.reply_to_id } : {}),
  };
}
/** readPending 读取原会话完整暂存，损坏或受限存储明确报错而不默默丢弃内容。 */
export function readPending(storage: MessageStorage, key: string): PendingMessage[] {
  let raw: string | null;
  try {
    raw = storage.getItem(key);
  } catch {
    throw new Error("浏览器禁止消息暂存，请允许站点存储后重试，避免刷新丢失未确认消息。");
  }
  if (!raw) return [];
  let items: unknown;
  try {
    items = JSON.parse(raw);
  } catch {
    throw new Error("待确认消息暂存已损坏，请保留页面并联系管理员，暂未覆盖原数据。");
  }
  if (!Array.isArray(items)) throw new Error("待确认消息暂存格式无效，暂未覆盖原数据。");
  return items.map(cleanMessage);
}
/** writePending 在容量边界内同步保存，写入失败时保持原消息和草稿可恢复。 */
function writePending(storage: MessageStorage, key: string, messages: PendingMessage[]) {
  const encoded = JSON.stringify(messages.map(cleanMessage));
  if (messages.length > maxMessages || encoded.length > maxCharacters)
    throw new Error("待确认消息暂存已满，请先重试并确认旧消息后再发送。");
  try {
    if (messages.length) storage.setItem(key, encoded);
    else storage.removeItem(key);
  } catch {
    throw new Error("无法保存待确认消息，请允许站点存储或释放空间后重试；当前草稿会保留。");
  }
}
/** stagePending 先保存完整正文与稳定编号，同一编号不同内容拒绝覆盖原消息。 */
export function stagePending(storage: MessageStorage, key: string, message: PendingMessage) {
  const clean = cleanMessage(message);
  const messages = readPending(storage, key);
  const existing = messages.find((item) => item.client_message_id === clean.client_message_id);
  if (existing && JSON.stringify(existing) !== JSON.stringify(clean))
    throw new Error("消息编号冲突，原待确认消息已保留，请刷新后重试。");
  if (!existing) messages.push(clean);
  writePending(storage, key, messages);
}
/** acknowledgePending 只清除服务端已确认编号，旧会话请求完成不会改写当前会话暂存。 */
export function acknowledgePending(storage: MessageStorage, key: string, ids: Iterable<string>) {
  const confirmed = new Set(ids);
  const messages = readPending(storage, key);
  const remaining = messages.filter((item) => !confirmed.has(item.client_message_id));
  if (remaining.length !== messages.length) writePending(storage, key, remaining);
}
