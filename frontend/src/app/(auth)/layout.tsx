/** 文件作用说明：将认证页面标记为不索引，避免邮箱与邀请入口被收录。 */
import type { Metadata } from "next";
export const metadata: Metadata = { robots: { index: false, follow: false } };
/** AuthLayout 保留独立认证流程布局。 */
export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return children;
}
