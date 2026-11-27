/** 文件作用说明：校验站内返回地址并生成不含登录凭证的访客接入链接。 */
/** safeReturnPath 仅允许同站相对路径，防止登录后跳转到第三方网站。 */
export function safeReturnPath(value: string | null | undefined, fallback = "/console"): string {
  if (
    !value ||
    !value.startsWith("/") ||
    value.startsWith("//") ||
    value.includes("\\") ||
    /[\u0000-\u001f]/.test(value)
  )
    return fallback;
  return value;
}
/** loginReturnURL 保留当前站内页面与查询参数，登录后仍能定位邮件指定的会话。 */
export function loginReturnURL(pathname: string, search: string): string {
  return `/login?next=${encodeURIComponent(safeReturnPath(pathname + search))}`;
}
/** conversationFromQuery 只接收单个完整会话UUID，拒绝重复参数、路径和任意请求片段。 */
export function conversationFromQuery(value: string | string[] | null | undefined): string {
  return typeof value === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value)
    ? value.toLowerCase()
    : "";
}
/** entryURL 生成公开入口；业务参数通过 URLSearchParams 编码。 */
export function entryURL(
  origin: string,
  entryKey: string,
  profile?: Record<string, string>,
): string {
  const url = new URL(`/c/${encodeURIComponent(entryKey)}`, origin);
  for (const [key, value] of Object.entries(profile || {}))
    if (value) url.searchParams.set(key, value);
  return url.toString();
}
/** siteURL 浏览器优先采用当前站点，服务端无请求时使用运行期部署配置。 */
export function siteURL(): string {
  if (typeof window !== "undefined") return window.location.origin;
  return (
    process.env.SUPPORT_PUBLIC_URL || process.env.NEXT_PUBLIC_SITE_URL || "http://127.0.0.1:13000"
  );
}
