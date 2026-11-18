/** 文件作用说明：按已确认的银白钴蓝图稿展示科技风官网、真实产品能力与免费注册入口。 */
import { PublicAIOverview, PublicAIPreview } from "@/components/public-ai-overview";
import { HeroRings } from "@/components/hero-rings";
import { ProductPreview } from "@/components/product-preview";
import {
  ApartmentOutlined,
  ArrowRightOutlined,
  LinkOutlined,
  SkinOutlined,
  TeamOutlined,
  RobotOutlined,
} from "@/components/public-icons";
import Link from "@/i18n/link";
import { publicMetadata } from "@/i18n/metadata";
import { localizeStatic } from "@/i18n/runtime";
import { getTranslator } from "@/i18n/server";
import { Button } from "antd";
import type { Metadata } from "next";
import Image from "next/image";
import { getLocale } from "next-intl/server";
import type { Locale } from "@/i18n/preferences";
import { publicSoftwareData } from "@/lib/public-seo";
import { siteURL } from "@/lib/urls";
/** generateMetadata 输出当前语言的官网标题和替代地址。 */
export async function generateMetadata(): Promise<Metadata> {
  return publicMetadata(
    "/",
    "AI 客服系统与知识库自动回复，小团队免费",
    "GoodChat AI 客服支持知识库自动回复、图片识别、媒体话术与人工接管。小团队基础系统免费，含 2 个客服坐席；AI 调用按实际 Token 计费。",
  );
}
const connections = [
  {
    icon: LinkOutlined,
    title: "一个链接，连接网页与手机",
    text: "通过网页、手机网页或专属链接接入，让客户随时找到你。",
  },
  {
    icon: TeamOutlined,
    title: "按团队规则，分配给合适的人",
    text: "按业务组、技能与接待状态安排咨询，让团队协作更有序。",
  },
  {
    icon: SkinOutlined,
    title: "用 AI 设计属于你的客服页面",
    text: "自定义品牌与页面布局，AI 生成草稿，预览后发布自己的设计。",
  },
];
/** HomePage 保持官网正文可索引，并将独立视觉素材与原生产品示意组合为真实网页。 */
export default async function HomePage() {
  const tr = await getTranslator();
  const locale = (await getLocale()) as Locale;
  const schema = publicSoftwareData(
    siteURL(),
    locale,
    tr("AI 客服系统"),
    tr(
      "GoodChat AI 客服支持知识库自动回复、图片识别、媒体话术与人工接管。小团队基础系统免费，含 2 个客服坐席；AI 调用按实际 Token 计费。",
    ),
    tr("系统与 2 个基础坐席免费，AI 调用按实际 Token 计费。"),
  );
  return (
    <div data-public-page="home" className="cobalt-home">
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(schema).replace(/</g, "\\u003c") }}
      />
      <section className="public-width public-home-hero">
        <Image
          className="cobalt-hero-environment"
          src="/images/cobalt-hero-environment.png"
          alt=""
          width={1600}
          height={1200}
          priority
          sizes="100vw"
        />
        <HeroRings />
        <div className="public-hero-copy">
          <h1>
            {tr("AI 客服自动回复，")}
            <br />
            <span>{tr("人工随时接管。")}</span>
          </h1>
          <p>
            {tr("让 AI 根据你的知识库回答问题，发送图片、视频和语音话术，与人工客服协同接待。")}
          </p>
          <span className="public-kicker">
            <TeamOutlined />
            {tr("小团队免费起步 · 2 个基础坐席")}
          </span>
          <p className="public-ai-fee-note">
            {tr("系统与 2 个基础坐席免费，AI 调用按实际 Token 计费。")}
          </p>
          <div className="public-hero-actions">
            <Button type="primary" size="large" href="/register">
              {tr("开始配置 AI 客服")}
              <ArrowRightOutlined />
            </Button>
            <Link className="public-text-action" href="/features">
              {tr("了解 AI 客服功能")}
              <ArrowRightOutlined />
            </Link>
          </div>
        </div>
        <div className="cobalt-hero-callout cobalt-callout-entry" aria-hidden="true">
          <RobotOutlined />
          <div>
            <strong>{tr("知识库自动回复与人工协作")}</strong>
            <span>{tr("AI 按需读取相关知识")}</span>
          </div>
        </div>
        <div className="cobalt-hero-callout cobalt-callout-route" aria-hidden="true">
          <ApartmentOutlined />
          <div>
            <strong>{tr("人工随时接管。")}</strong>
            <span>{tr("自动或辅助，人工接手")}</span>
          </div>
        </div>
        <div className="public-hero-product">
          <PublicAIPreview />
        </div>
      </section>
      <PublicAIOverview />
      <section className="public-width cobalt-connect">
        <div className="cobalt-connect-copy">
          <div className="public-overline">02 / CONNECT</div>
          <h2>
            {tr("接入很轻， ")}
            <br />
            {tr("协作很强。 ")}
          </h2>
          <p>
            {tr("将客户从不同渠道汇聚到一个工作台，按规则分配，协同服务，打造一致的品牌体验。")}
          </p>
        </div>
        <div className="cobalt-connect-list">
          {localizeStatic(connections, tr).map((item) => (
            <article key={item.title}>
              <item.icon />
              <div>
                <h3>{item.title}</h3>
                <p>{item.text}</p>
              </div>
            </article>
          ))}
        </div>
        <div className="cobalt-design-showcase">
          <ProductPreview kind="design" />
        </div>
      </section>
      <section className="cobalt-free-band">
        <div className="public-width cobalt-free-content">
          <div className="cobalt-zero" aria-label={tr("零元")}>
            <small>¥</small>0
          </div>
          <div>
            <h2>{tr("小团队，免费开始。")}</h2>
            <p>{tr("系统与 2 个基础坐席免费，AI 调用按实际 Token 计费。")}</p>
          </div>
          <Button type="primary" size="large" href="/register">
            {tr("免费开始 ")}
            <ArrowRightOutlined />
          </Button>
        </div>
      </section>
      <section className="public-width public-ai-faq">
        <h2>{tr("免费 AI 客服常见问题")}</h2>
        <details>
          <summary>{tr("AI 客服系统免费吗？")}</summary>
          <p>
            {tr(
              "基础版长期免费，包含 2 个客服坐席。AI 模型调用按实际输入和输出 Token 计费，单价与可用余额以控制台为准。",
            )}
          </p>
          <Link href="/pricing">{tr("系统免费，AI 按量计费")}</Link>
        </details>
        <details>
          <summary>{tr("AI 客服如何自动回复？")}</summary>
          <p>
            {tr(
              "上传知识库，绑定机器人可用的话术，再选择自动或辅助回复。AI 按需读取知识，人工可随时接管。",
            )}
          </p>
          <Link href="/help">{tr("AI 客服使用指南")}</Link>
        </details>
      </section>
    </div>
  );
}
