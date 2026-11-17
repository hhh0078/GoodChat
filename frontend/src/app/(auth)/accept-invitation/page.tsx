/** 文件作用说明：提供受邀成员邮箱验证和加入已有租户的页面。 */
import { AuthForm } from "@/features/auth/auth-form";
import { getTranslator } from "@/i18n/server";
import type { Metadata } from "next";
import { Suspense } from "react";
/** generateMetadata 按当前语言呈现私有入口标题。 */
export async function generateMetadata(): Promise<Metadata> {
  const tr = await getTranslator();
  return { title: tr("加入团队") };
}
/** AcceptInvitationPage 展示邀请验证表单。 */
export default async function AcceptInvitationPage() {
  const tr = await getTranslator();
  return (
    <Suspense fallback={<div className="console-loading">{tr("正在读取邀请\u2026")}</div>}>
      <AuthForm mode="invitation" />
    </Suspense>
  );
}
