/** 文件作用说明：提供邮箱验证后重置密码的页面。 */
import { AuthForm } from "@/features/auth/auth-form";
import { getTranslator } from "@/i18n/server";
import type { Metadata } from "next";
import { Suspense } from "react";
/** generateMetadata 按当前语言呈现私有入口标题。 */
export async function generateMetadata(): Promise<Metadata> {
  const tr = await getTranslator();
  return { title: tr("找回密码") };
}
/** ResetPasswordPage 展示密码找回表单。 */
export default async function ResetPasswordPage() {
  const tr = await getTranslator();
  return (
    <Suspense fallback={<div className="console-loading">{tr("正在准备验证\u2026")}</div>}>
      <AuthForm mode="reset" />
    </Suspense>
  );
}
