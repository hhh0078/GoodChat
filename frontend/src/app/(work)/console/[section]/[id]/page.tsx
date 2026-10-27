/** 文件作用说明：连接租户私有模板与页面草稿编辑器，保持发布流程独立。 */
import { notFound } from "next/navigation";
import { DecorationEditor } from "@/features/decoration/editor";
/** EditorRoute 校验编辑资源类型后加载版本化装修编辑器。 */
export default async function EditorRoute({
  params,
}: {
  params: Promise<{ section: string; id: string }>;
}) {
  const { section, id } = await params;
  if (section !== "templates" && section !== "pages") notFound();
  return <DecorationEditor id={id} kind={section} />;
}
