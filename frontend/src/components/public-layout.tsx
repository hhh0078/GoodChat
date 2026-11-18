/** 文件作用说明：提供服务端官网导航、页脚与公开信息页面布局。 */
import Link from "@/i18n/link";
import { useTranslator } from "@/i18n/use-translator";
import { Button } from "antd";
import { Brand } from "./brand";
import { OfficialSupportWidget } from "./official-support-widget";
import { officialSupportEntry } from "@/lib/official-support-entry";
import { PublicMotion } from "./public-motion";
import "@/styles/public-motion.css";
import "@/styles/public-ai-demo.css";
/** PublicLayout 渲染无需客户端 JavaScript 即可读取的官网导航和内容。 */
export function PublicLayout({ children }: { children: React.ReactNode }) {
  const tr = useTranslator();
  return (
    <div className="public-site">
      <PublicMotion />
      <div className="public-width">
        <header className="public-header">
          <Brand />
          <nav className="public-nav" aria-label={tr("官网导航")}>
            <Link href="/features">{tr("产品功能")}</Link>
            <Link href="/solutions">{tr("使用场景")}</Link>
            <Link href="/pricing">{tr("价格方案")}</Link>
            <Link href="/downloads">{tr("客户端下载")}</Link>
            <Link href="/help">{tr("接入帮助")}</Link>
          </nav>
          <div className="public-actions">
            <Link href="/login">{tr("登录")}</Link>
            <Button type="primary" href="/register">
              {tr("免费开始 ")}
            </Button>
          </div>
        </header>
      </div>
      <main>{children}</main>
      <footer className="public-footer">
        <div className="public-width footer-row">
          <div className="public-footer-brand">
            <Brand />
            <p className="public-footer-description">{tr("每一次对话，都值得好好回应。")}</p>
          </div>
          <nav className="footer-links" aria-label={tr("页脚导航")}>
            <Link href="/features">{tr("产品功能")}</Link>
            <Link href="/solutions">{tr("使用场景")}</Link>
            <Link href="/pricing">{tr("价格方案")}</Link>
            <Link href="/help">{tr("接入帮助")}</Link>
            <Link href="/privacy">{tr("隐私政策")}</Link>
            <Link href="/downloads">{tr("客户端下载")}</Link>
            <Link href="/terms">{tr("服务条款")}</Link>
            <Link href="/console">{tr("工作台")}</Link>
          </nav>
        </div>
      </footer>
      <OfficialSupportWidget entryKey={officialSupportEntry} />
    </div>
  );
}
