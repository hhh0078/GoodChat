/** 文件作用说明：解析同源票据接口指定的WSS地址，兼容旧版相对路径，拒绝凭据、混合内容和非消息路径。 */
/** realtimeSocketURL 只接受消息握手地址，HTTPS页面绝不降级到明文连接。 */
export function realtimeSocketURL(value: string, pageOrigin: string): string | null {
  try {
    const page = new URL(pageOrigin);
    const url = new URL(value, page);
    if (["http:", "https:"].includes(url.protocol)) {
      if (url.origin !== page.origin) return null;
      url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    }
    if (
      !["ws:", "wss:"].includes(url.protocol) ||
      (page.protocol === "https:" && url.protocol !== "wss:") ||
      url.username ||
      url.password ||
      url.hash ||
      url.pathname !== "/ws/v1" ||
      url.searchParams.getAll("ticket").length !== 1 ||
      !url.searchParams.get("ticket") ||
      [...url.searchParams.keys()].some((key) => key !== "ticket")
    )
      return null;
    return url.href;
  } catch {
    return null;
  }
}
