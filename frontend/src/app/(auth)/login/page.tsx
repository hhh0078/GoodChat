/** 文件作用说明：提供邮箱密码与验证码登录页面。 */
import { AuthForm } from "@/features/auth/auth-form";
import { getTranslator } from "@/i18n/server";
import type { Metadata } from "next";
import { Suspense } from "react";
/** generateMetadata 按当前语言呈现私有入口标题。 */
export async function generateMetadata(): Promise<Metadata> {
  const tr = await getTranslator();
  return { title: tr("登录") };
}
/** LoginPage 为包含查询参数的身份表单提供服务端边界。 */
export default async function LoginPage() {
  const tr = await getTranslator();
  return (
    <Suspense fallback={<div className="console-loading">{tr("正在准备登录\u2026")}</div>}>
      <AuthForm mode="login" />
    </Suspense>
  );
}
