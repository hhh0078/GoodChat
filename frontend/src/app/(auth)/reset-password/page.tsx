/** 文件作用说明：提供邮箱验证后重置密码的页面。 */
import { Suspense } from "react";
import type { Metadata } from "next";
import { AuthForm } from "@/features/auth/auth-form";
export const metadata: Metadata = { title: "找回密码" };
/** ResetPasswordPage 展示密码找回表单。 */
export default function ResetPasswordPage() {
  return (
    <Suspense fallback={<div className="console-loading">正在准备验证…</div>}>
      <AuthForm mode="reset" />
    </Suspense>
  );
}
