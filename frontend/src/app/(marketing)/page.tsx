/** 文件作用说明：按已确认的银白钴蓝图稿展示科技风官网、真实产品能力与免费注册入口。 */
import type { Metadata } from "next";
import Image from "next/image";
import Link from "next/link";
import { Button } from "antd";
import { ProductPreview } from "@/components/product-preview";
import { HeroRings } from "@/components/hero-rings";
import {
  ArrowRightOutlined,
  ApartmentOutlined,
  LinkOutlined,
  TeamOutlined,
  SkinOutlined,
} from "@/components/public-icons";

export const metadata: Metadata = {
  title: "在线客服与 AI 页面装修",
  alternates: { canonical: "/" },
  openGraph: {
    title: "GoodChat · 连接每一位客户，即刻回应每一次期待",
    description: "在线客服、团队协作与品牌页面，一处连接。基础版长期免费，包含 2 个客服坐席。",
    url: "/",
  },
};
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
export default function HomePage() {
  return (
    <div data-public-page="home" className="cobalt-home">
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
            连接每一位客户。
            <br />
            <span>即刻回应</span>每一次期待。
          </h1>
          <p>在线客服、团队协作与品牌页面，一处连接。</p>
          <span className="public-kicker">
            <TeamOutlined /> 永久免费 · 2 个客服坐席
          </span>
          <div className="public-hero-actions">
            <Button type="primary" size="large" href="/register">
              免费创建工作空间 <ArrowRightOutlined />
            </Button>
            <Link className="public-text-action" href="/features">
              了解产品 <ArrowRightOutlined />
            </Link>
          </div>
        </div>
        <div className="cobalt-hero-callout cobalt-callout-entry" aria-hidden="true">
          <LinkOutlined />
          <div>
            <strong>多入口接入</strong>
            <span>网页、手机网页等</span>
          </div>
        </div>
        <div className="cobalt-hero-callout cobalt-callout-route" aria-hidden="true">
          <ApartmentOutlined />
          <div>
            <strong>按规则分配</strong>
            <span>把咨询交给合适的人</span>
          </div>
        </div>
        <div className="public-hero-product">
          <ProductPreview />
        </div>
      </section>
      <section className="public-width cobalt-connect">
        <div className="cobalt-connect-copy">
          <div className="public-overline">01 / CONNECT</div>
          <h2>
            接入很轻，
            <br />
            协作很强。
          </h2>
          <p>将客户从不同渠道汇聚到一个工作台，按规则分配，协同服务，打造一致的品牌体验。</p>
        </div>
        <div className="cobalt-connect-list">
          {connections.map((item) => (
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
          <div className="cobalt-zero" aria-label="零元">
            <small>¥</small>0
          </div>
          <div>
            <h2>从免费开始，让服务跟上增长。</h2>
            <p>基础版长期免费，包含 2 个客服坐席。团队扩展时，再按需升级。</p>
          </div>
          <Button type="primary" size="large" href="/register">
            免费开始 <ArrowRightOutlined />
          </Button>
        </div>
      </section>
      <p className="public-width public-roadmap-note">
        当前支持网页接待。macOS、Windows、Android
        客服端及第三方平台渠道将后续接入，具体可用能力以控制台为准。
      </p>
    </div>
  );
}
