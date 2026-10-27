/** 文件作用说明：按明亮品牌设计图展示 GoodChat 免费客服定位、真实产品能力与可索引的注册入口。 */
import type { Metadata } from "next";
import Image from "next/image";
import { Button } from "antd";
import {
  ArrowRightOutlined,
  ApartmentOutlined,
  LinkOutlined,
  MessageOutlined,
  TeamOutlined,
  SkinOutlined,
  SafetyOutlined,
} from "@/components/public-icons";
export const metadata: Metadata = {
  title: "在线客服与 AI 页面装修",
  alternates: { canonical: "/" },
  openGraph: {
    title: "GoodChat · 永久免费基础客服",
    description: "多链接接入、灵活分配和统一客服工作台，让客户在你的品牌页面里获得服务。",
    url: "/",
  },
};
const highlights = [
  { icon: LinkOutlined, title: "多链接接入", text: "独立链接与网站嵌入，连接不同业务入口。" },
  {
    icon: ApartmentOutlined,
    title: "灵活分配",
    text: "按接待范围和团队规则，把咨询交给合适的人。",
  },
  { icon: SkinOutlined, title: "自主装修", text: "让聊天页面与自己的品牌保持一致。" },
];
const features = [
  {
    icon: LinkOutlined,
    title: "入口",
    text: "售前、售后、广告和门店各用自己的链接，电脑与手机打开即可咨询。",
  },
  {
    icon: ApartmentOutlined,
    title: "分配",
    text: "按客服组、分配池、技能与接待状态安排咨询，支持自动分配和人工领取。",
  },
  {
    icon: MessageOutlined,
    title: "沟通",
    text: "文字、图片、文件和语音集中接待，保留来源、客户资料与沟通记录。",
  },
  {
    icon: SkinOutlined,
    title: "装修",
    text: "从基础配色、组件布局到代码装修与 AI 创作，预览后发布自己的页面。",
  },
  {
    icon: TeamOutlined,
    title: "团队",
    text: "分别设置管理员与客服角色、接待资格和服务范围，让成员清楚分工。",
  },
  {
    icon: SafetyOutlined,
    title: "权限",
    text: "按租户、角色和接待范围授权，客户记录与私有模板在各自工作空间内管理。",
  },
];
/** HomePage 以文字排版和明确标注的产品示意图呈现官网首页。 */
export default function HomePage() {
  return (
    <div data-public-page="home">
      <section className="public-width public-home-hero">
        <div className="public-hero-copy">
          <span className="public-kicker">免费包含 2 个客服坐席</span>
          <h1>基础客服，永久免费。</h1>
          <p>从一个聊天链接开始，把入口、分配、消息和品牌页面连在一起。团队扩展时，再按需升级。</p>
          <div className="public-hero-actions">
            <Button type="primary" size="large" href="/register">
              免费开始
            </Button>
            <Button size="large" href="/features">
              了解产品
            </Button>
          </div>
        </div>
        <figure className="public-hero-preview">
          <figcaption>界面示意 · 示例聊天与客户资料</figcaption>
          <Image
            src="/images/workspace-preview.png"
            alt="GoodChat 明亮客服工作台设计示意，包含咨询列表、聊天和客户资料，非真实业务数据"
            width={1488}
            height={1056}
            priority
            sizes="(max-width: 768px) calc(100vw - 40px), 56vw"
          />
        </figure>
      </section>
      <section className="public-highlights-band" aria-label="产品重点">
        <div className="public-width public-highlights">
          {highlights.map((item) => (
            <article key={item.title}>
              <item.icon />
              <div>
                <h2>{item.title}</h2>
                <p>{item.text}</p>
              </div>
            </article>
          ))}
        </div>
      </section>
      <section className="public-width public-capabilities">
        <div className="public-section-title">
          <h2>覆盖客服全流程的核心能力</h2>
          <a href="/features">
            了解更多功能 <ArrowRightOutlined />
          </a>
        </div>
        <div className="public-capability-grid">
          {features.map((item) => (
            <article key={item.title}>
              <item.icon />
              <div>
                <h3>{item.title}</h3>
                <p>{item.text}</p>
              </div>
            </article>
          ))}
        </div>
      </section>
      <section className="public-start-band">
        <div className="public-width">
          <h2>从一次对话开始。</h2>
          <p>免费包含 2 个客服坐席，从网页开始，把接待做扎实。</p>
          <Button type="primary" href="/register">
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
