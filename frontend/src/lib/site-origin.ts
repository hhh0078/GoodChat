/** 文件作用说明：从受控本机网关读取当前站点来源，为服务端SEO、文章与地图生成同域网址。 */
import { headers } from "next/headers";
import { cache } from "react";
import { siteURL } from "./urls";

/** requestSiteURL 仅采用网关覆盖后的规范来源，直接启动Next时回退到本站部署配置。 */
export const requestSiteURL = cache(async (): Promise<string> => {
  const value = (await headers()).get("x-goodchat-public-origin");
  if (value) {
    try {
      const url = new URL(value);
      if (
        ["http:", "https:"].includes(url.protocol) &&
        !url.username &&
        !url.password &&
        url.href === `${url.origin}/`
      )
        return url.origin;
    } catch {
      /* 无效网关来源不进入SEO或分享链接。 */
    }
  }
  return siteURL();
});
