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
/** siteURL 返回部署配置的官网地址。 */
export function siteURL(): string {
  return process.env.NEXT_PUBLIC_SITE_URL || "http://127.0.0.1:13000";
}
