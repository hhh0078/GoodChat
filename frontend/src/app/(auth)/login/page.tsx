/** 文件作用说明：提供邮箱密码与验证码登录页面。 */
import { Suspense } from "react";
import type { Metadata } from "next";
import { AuthForm } from "@/features/auth/auth-form";
export const metadata: Metadata = { title: "登录" };
/** LoginPage 为包含查询参数的身份表单提供服务端边界。 */
export default function LoginPage() {
  return (
    <Suspense fallback={<div className="console-loading">正在准备登录…</div>}>
      <AuthForm mode="login" />
    </Suspense>
  );
}
