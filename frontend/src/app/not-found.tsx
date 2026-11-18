/** 文件作用说明：按明亮错误页设计图提供明确的未知地址提示和返回官网、工作台的入口。 */
import { LocaleSettingsButton } from "@/i18n/locale-provider";
import { Brand } from "@/components/brand";
import { getTranslator } from "@/i18n/server";
import { Button } from "antd";
/** NotFound 以清晰的文字层级说明未知地址并提供有效导航。 */
export default async function NotFound() {
  const tr = await getTranslator();
  return (
    <section className="public-error-page" aria-labelledby="public-error-title">
      <header>
        <Brand />
        <LocaleSettingsButton />
      </header>
      <div className="public-error-content">
        <span className="public-kicker">{tr("页面未找到")}</span>
        <div className="public-error-code" aria-hidden="true">
          404
        </div>
        <h1 id="public-error-title">{tr("这个页面暂时找不到了。")}</h1>
        <p>{tr("链接可能已失效，或地址输入有误。你可以返回首页，继续了解 GoodChat。")}</p>
        <div className="public-hero-actions">
          <Button type="primary" size="large" href="/">
            {tr("返回官网 ")}
          </Button>
          <Button size="large" href="/console">
            {tr("前往工作台 ")}
          </Button>
        </div>
      </div>
      <footer>{tr("GoodChat \u00B7 把咨询接起来，把服务做好。")}</footer>
    </section>
  );
}
