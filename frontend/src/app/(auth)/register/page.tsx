/** 文件作用说明：提供邮箱注册与新租户创建页面。 */
import { AuthForm } from "@/features/auth/auth-form";
import { getTranslator } from "@/i18n/server";
import type { Metadata } from "next";
import { Suspense } from "react";
/** generateMetadata 按当前语言呈现私有入口标题。 */
export async function generateMetadata(): Promise<Metadata> {
  const tr = await getTranslator();
  return { title: tr("创建工作空间") };
}
/** RegisterPage 展示通过邮箱验证创建租户的表单。 */
export default async function RegisterPage() {
  const tr = await getTranslator();
  return (
    <Suspense fallback={<div className="console-loading">{tr("正在准备注册\u2026")}</div>}>
      <AuthForm mode="register" />
    </Suspense>
  );
}
