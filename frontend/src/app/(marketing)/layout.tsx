/** 文件作用说明：为公开官网页面复用服务端导航与页脚。 */
import { PublicLayout } from "@/components/public-layout";
/** MarketingLayout 展示官网页面布局。 */
export default function MarketingLayout({ children }: { children: React.ReactNode }) {
  return <PublicLayout>{children}</PublicLayout>;
}
