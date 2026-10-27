/** 文件作用说明：提供兼容电脑与手机的公开咨询入口，不索引访客页面。 */
import type { Metadata } from "next";
import { VisitorChat } from "@/features/chat/visitor";
export const metadata: Metadata = { title: "在线咨询", robots: { index: false, follow: false } };
/** VisitorPage 将公开入口编号交给独立访客初始化流程。 */
export default async function VisitorPage({ params }: { params: Promise<{ entryKey: string }> }) {
  const { entryKey } = await params;
  return <VisitorChat entryKey={entryKey} />;
}
