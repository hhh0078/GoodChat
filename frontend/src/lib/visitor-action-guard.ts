/** 文件作用说明：约束官方留言与评价表单的切换，防止模板重复调用清空尚未完成的草稿。 */

/** assertVisitorActionAvailable 校验当前表单、会话状态及提交状态，已有表单重复打开不改变草稿。 */
export function assertVisitorActionAvailable(
  current: "lead" | "evaluation" | null,
  requested: "lead" | "evaluation",
  status: string,
  busy: boolean,
): void {
  if (current === requested) return;
  if (current) throw new Error("请先完成或关闭当前表单");
  if (busy) throw new Error("请先完成当前操作");
  if (requested === "lead" && status === "closed")
    throw new Error("当前咨询已结束，请发起新的咨询后留言");
  if (requested === "evaluation" && status !== "closed")
    throw new Error("咨询结束后才可以评价本次服务");
}
