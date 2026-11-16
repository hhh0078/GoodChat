/** 文件作用说明：提供兼容电脑与手机的公开咨询入口，不索引访客页面。 */
import { VisitorChat } from "@/features/chat/visitor";
import { getTranslator } from "@/i18n/server";
import type { Metadata } from "next";
/** generateMetadata 按当前语言呈现私有入口标题。 */
export async function generateMetadata(): Promise<Metadata> {
  const tr = await getTranslator();
  return { title: tr("在线咨询"), robots: { index: false, follow: false } };
}
/** VisitorPage 将公开入口编号交给独立访客初始化流程。 */
export default async function VisitorPage({ params }: { params: Promise<{ entryKey: string }> }) {
  const { entryKey } = await params;
  return <VisitorChat entryKey={entryKey} />;
}
