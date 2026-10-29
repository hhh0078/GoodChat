/** 文件作用说明：按服务端字符串序号合并聊天消息，保留更新并避免数字精度损失。 */
type MessageState = {
  id: string;
  seq?: string;
  event_seq?: string;
  withdrawn_at?: string;
  reply?: { id: string; content: string; withdrawn: boolean } | null;
};
/** mergeMessages 以稳定编号去重并保留不可逆撤回，延迟HTTP响应不能恢复正文、媒体、选项或引用。 */
export function mergeMessages<T extends MessageState>(
  previous: T[],
  incoming: T[],
  knownWithdrawals: ReadonlyMap<string, string> = new Map(),
): (T & { withdrawn_at?: string })[] {
  const withdrawn = new Map(knownWithdrawals);
  const withdrawnReplies = new Set<string>();
  for (const item of [...previous, ...incoming]) {
    if (item.withdrawn_at) withdrawn.set(item.id, item.withdrawn_at);
    if (item.reply?.withdrawn) withdrawnReplies.add(item.reply.id);
  }
  const items = new Map(previous.map((item) => [item.id, item]));
  incoming.forEach((item) => items.set(item.id, item));
  return [...items.values()]
    .map((item) => ({
      ...item,
      ...(withdrawn.has(item.id)
        ? {
            withdrawn_at: withdrawn.get(item.id),
            content: "",
            attachment: undefined,
            attachment_id: undefined,
            options: [],
          }
        : {}),
      ...(item.reply && (withdrawn.has(item.reply.id) || withdrawnReplies.has(item.reply.id))
        ? { reply: { ...item.reply, content: "", withdrawn: true } }
        : {}),
    }))
    .sort((a, b) => {
      const left = BigInt(a.seq || a.event_seq || 0),
        right = BigInt(b.seq || b.event_seq || 0);
      return left < right ? -1 : left > right ? 1 : 0;
    });
}
/** withdrawMessages 在服务端确认撤回后同步清理正文、附件及所有已加载引用摘要。 */
export function withdrawMessages<T extends MessageState>(
  messages: T[],
  messageId: string,
  withdrawnAt: string,
): (T & { withdrawn_at?: string })[] {
  return mergeMessages(messages, [], new Map([[messageId, withdrawnAt]]));
}
