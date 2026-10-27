/** 文件作用说明：按服务端字符串序号合并聊天消息，保留更新并避免数字精度损失。 */
/** mergeMessages 以稳定编号去重，排序不要求可见消息序号连续。 */
export function mergeMessages<T extends { id: string; seq?: string; event_seq?: string }>(
  previous: T[],
  incoming: T[],
): T[] {
  const items = new Map(previous.map((item) => [item.id, item]));
  incoming.forEach((item) => items.set(item.id, item));
  return [...items.values()].sort((a, b) => {
    const left = BigInt(a.seq || a.event_seq || 0),
      right = BigInt(b.seq || b.event_seq || 0);
    return left < right ? -1 : left > right ? 1 : 0;
  });
}
/** withdrawMessages 在服务端确认撤回后同步清理正文、附件及所有已加载引用摘要。 */
export function withdrawMessages<
  T extends {
    id: string;
    content: string;
    attachment?: unknown;
    withdrawn_at?: string;
    reply?: { id: string; content: string; withdrawn: boolean };
  },
>(messages: T[], messageId: string, withdrawnAt: string): (T & { withdrawn_at?: string })[] {
  return messages.map((item) => ({
    ...item,
    ...(item.id === messageId
      ? { content: "", attachment: undefined, withdrawn_at: withdrawnAt }
      : {}),
    ...(item.reply?.id === messageId
      ? { reply: { ...item.reply, content: "", withdrawn: true } }
      : {}),
  }));
}
