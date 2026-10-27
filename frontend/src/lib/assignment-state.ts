/** 文件作用说明：校验前端转接入口的目标成员与当前在线容量，最终授权仍由服务端事务执行。 */
/** isTransferRecipient 只向未过期待确认转接的目标租户成员显示接收操作。 */
export function isTransferRecipient(
  transfer:
    | { status: string; target_agent_id?: string; target_member_id?: string; expires_at?: string }
    | null
    | undefined,
  memberId: string,
  now = Date.now(),
): boolean {
  return (
    !!transfer &&
    transfer.status === "pending" &&
    (transfer.target_agent_id || transfer.target_member_id) === memberId &&
    (!transfer.expires_at || new Date(transfer.expires_at).getTime() > now)
  );
}
/** canTransferTo 排除离线、未上班、示忙和容量已满的目标，服务端仍需重新校验。 */
export function canTransferTo(member: Record<string, unknown>): boolean {
  return (
    member.online === true &&
    member.available === true &&
    member.busy === false &&
    typeof member.current_load === "number" &&
    typeof member.capacity === "number" &&
    member.current_load >= 0 &&
    member.current_load < member.capacity
  );
}
