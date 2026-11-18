/** 文件作用说明：为九语言全部公开官网页面输出与页面head一致的XML目录，不收录账号或客户资料。 */
import { siteURL } from "@/lib/urls";
import { publicSitemap } from "../lib/public-seo.ts";
import type { MetadataRoute } from "next";
/** sitemap 复用官网页面与语言规则，避免新增语言后目录遗漏或替代链接冲突。 */
export default function sitemap(): MetadataRoute.Sitemap {
  return publicSitemap(siteURL());
}
