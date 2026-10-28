/** 文件作用说明：定义富快捷回复接口并按个人、公共及当前成员真实使用次数分组排序。 */
export type QuickReplyOption = { id: string; label: string; value: string };
export type QuickReply = {
  id: string;
  title: string;
  content: string;
  message_type: "text" | "image" | "audio" | "file";
  attachment_id?: string | null;
  attachment?: { id: string; file_name: string; content_type: string; size_bytes: number };
  options: QuickReplyOption[];
  scope: "personal" | "public";
  version: string;
  use_count: number;
  enabled: boolean;
  created_at?: string;
};

/** groupQuickReplies 只显示有效话术，各组按成功发送次数排序，相同次数保留服务端稳定顺序。 */
export function groupQuickReplies(items: QuickReply[]) {
  const enabled = items.filter((item) => item.enabled !== false);
  const sort = (left: QuickReply, right: QuickReply) =>
    Number(right.use_count || 0) - Number(left.use_count || 0);
  return {
    personal: enabled.filter((item) => item.scope === "personal").sort(sort),
    shared: enabled.filter((item) => item.scope === "public").sort(sort),
  };
}
