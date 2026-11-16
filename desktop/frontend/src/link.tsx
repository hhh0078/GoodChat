/** 文件作用说明：把客服组件的辅助网页链接交给系统浏览器，避免远端页面取得原生桥接。 */
import type { AnchorHTMLAttributes } from "react";
import { invoke } from "./native";

/** DesktopLink 保留普通链接语义，点击后由原生层检查目标协议。 */
export default function DesktopLink({
  href,
  onClick,
  ...props
}: AnchorHTMLAttributes<HTMLAnchorElement>) {
  return (
    <a
      {...props}
      href={href}
      onClick={(event) => {
        onClick?.(event);
        event.preventDefault();
        if (!href) return;
        const server = document.documentElement.dataset.server || "";
        void invoke("OpenExternal", new URL(href, `${server}/`).href);
      }}
    />
  );
}
