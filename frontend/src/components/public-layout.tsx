/** 文件作用说明：提供服务端官网导航、页脚与公开信息页面布局。 */
import Link from "next/link";
import { Button } from "antd";
import { Brand } from "./brand";
/** PublicLayout 渲染无需客户端 JavaScript 即可读取的官网导航和内容。 */
export function PublicLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="public-site">
      <div className="public-width">
        <header className="public-header">
          <Brand />
          <nav className="public-nav" aria-label="官网导航">
            <Link href="/features">产品功能</Link>
            <Link href="/solutions">使用场景</Link>
            <Link href="/pricing">价格方案</Link>
            <Link href="/help">接入帮助</Link>
          </nav>
          <div className="public-actions">
            <Link href="/login">登录</Link>
            <Button type="primary" href="/register">
              免费开始
            </Button>
          </div>
        </header>
      </div>
      <main>{children}</main>
      <footer className="public-footer">
        <div className="public-width footer-row">
          <div>
            <Brand />
            <p className="public-footer-description">每一次对话，都值得好好回应。</p>
            <span className="public-footer-note">网页在线客服 · 免费开始</span>
          </div>
          <nav className="footer-links" aria-label="页脚导航">
            <Link href="/features">产品功能</Link>
            <Link href="/solutions">使用场景</Link>
            <Link href="/pricing">价格方案</Link>
            <Link href="/help">接入帮助</Link>
            <Link href="/privacy">隐私政策</Link>
            <Link href="/terms">服务条款</Link>
            <Link href="/console">工作台</Link>
          </nav>
        </div>
      </footer>
    </div>
  );
}
