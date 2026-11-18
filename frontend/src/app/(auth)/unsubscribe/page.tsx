/** 文件作用说明：提供签名邮件退订确认页，打开邮件链接本身不会修改订阅。 */
import { UnsubscribeForm } from "@/features/notifications/unsubscribe";
import { Suspense } from "react";
export const metadata = { robots: { index: false, follow: false } };
/** UnsubscribePage 使用公开受限签名而不要求登录，安全邮件不在退订范围。 */
export default function UnsubscribePage() {
  return (
    <Suspense>
      <UnsubscribeForm />
    </Suspense>
  );
}
