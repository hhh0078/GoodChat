/** 文件作用说明：在官网根路径提供启用的IndexNow所有权验证键，未配置时返回真实404。 */
import { articleData } from "@/lib/site-articles";
export const dynamic = "force-dynamic";
/** GET 将后台公开验证键作为纯文本提供，禁止缓存旧凭据。 */
export async function GET() {
  const value = await articleData<{ key: string }>("article-indexnow-key");
  return new Response(value?.key || "Not found", {
    status: value ? 200 : 404,
    headers: { "Content-Type": "text/plain; charset=utf-8", "Cache-Control": "no-store" },
  });
}
