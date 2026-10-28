/** 文件作用说明：提供官网、认证与工作端共用的品牌标识。 */
import Link from "next/link";
import { MessageFilled } from "./public-icons";
/** Brand 显示统一品牌并链接至官网。 */
export function Brand() {
  return (
    <Link href="/" className="brand" aria-label="GoodChat 首页">
      <span className="brand-mark" aria-hidden="true">
        <MessageFilled />
      </span>
      <span>GoodChat</span>
    </Link>
  );
}
