/** 文件作用说明：为九种语言的官网、认证与工作端复用最新品牌标识与首页链接。 */
import Link from "@/i18n/link";
import { useTranslator } from "@/i18n/use-translator";
import { BrandMark } from "./brand-mark";
/** Brand 显示统一品牌并链接至当前语言官网。 */
export function Brand() {
  const tr = useTranslator();
  return (
    <Link href="/" className="brand" aria-label={tr("GoodChat 首页")}>
      <span className="brand-mark" aria-hidden="true">
        <BrandMark />
      </span>
      <span>GoodChat</span>
    </Link>
  );
}
