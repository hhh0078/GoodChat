/** 文件作用说明：为三端工作区提供统一认证布局并禁止搜索引擎收录。 */
import type { Metadata } from "next";
import { WorkspaceShell } from "@/components/workspace-shell";
export const metadata: Metadata = { robots: { index: false, follow: false } };
/** WorkLayout 渲染身份保护下的工作端外壳。 */
export default function WorkLayout({ children }: { children: React.ReactNode }) {
  return <WorkspaceShell>{children}</WorkspaceShell>;
}
