/** 文件作用说明：提供受邀成员邮箱验证和加入已有租户的页面。 */
import { Suspense } from "react";
import type { Metadata } from "next";
import { AuthForm } from "@/features/auth/auth-form";
export const metadata: Metadata = { title: "加入团队" };
/** AcceptInvitationPage 展示邀请验证表单。 */
export default function AcceptInvitationPage() {
  return (
    <Suspense fallback={<div className="console-loading">正在读取邀请…</div>}>
      <AuthForm mode="invitation" />
    </Suspense>
  );
}
