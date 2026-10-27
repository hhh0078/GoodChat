/** 文件作用说明：提供邮箱注册与新租户创建页面。 */
import { Suspense } from "react";
import type { Metadata } from "next";
import { AuthForm } from "@/features/auth/auth-form";
export const metadata: Metadata = { title: "创建工作空间" };
/** RegisterPage 展示通过邮箱验证创建租户的表单。 */
export default function RegisterPage() {
  return (
    <Suspense fallback={<div className="console-loading">正在准备注册…</div>}>
      <AuthForm mode="register" />
    </Suspense>
  );
}
