/** 文件作用说明：按明亮品牌设计图展示 GoodChat 免费客服定位、真实产品能力与可索引的注册入口。 */
import type { Metadata } from "next";
import { Button } from "antd";
import { ProductPreview } from "@/components/product-preview";
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
    title: "让客户更容易找到你",
    text: "售前、售后、广告和门店各用自己的链接，电脑与手机打开即可咨询。",
  },
  {
    icon: ApartmentOutlined,
    title: "让每次咨询找到对的人",
    text: "按客服组、分配池、技能与接待状态安排咨询，支持自动分配和人工领取。",
  },
  {
    icon: MessageOutlined,
    title: "把沟通留在同一个地方",
    text: "文字、图片、文件和语音集中接待，保留来源、客户资料与沟通记录。",
  },
  {
    icon: SkinOutlined,
    title: "服务页面，也有你的风格",
    text: "从基础配色、组件布局到代码装修与 AI 创作，预览后发布自己的页面。",
  },
  {
    icon: TeamOutlined,
    title: "一个团队，清晰协作",
    text: "分别设置管理员与客服角色、接待资格和服务范围，让成员清楚分工。",
  },
  {
    icon: SafetyOutlined,
    title: "各自的工作空间，各自管理",
    text: "按租户、角色和接待范围授权，客户记录与私有模板在各自工作空间内管理。",
  },
];
/** HomePage 通过居中品牌主张、大幅原生产品示意与能力分区呈现免费客服服务。 */
export default function HomePage() {
  return (
    <div data-public-page="home">
      <section className="public-width public-home-hero">
        <div className="public-hero-copy">
          <h1>
            每一次对话，
            <br />
            都值得<span>好好回应。</span>
          </h1>
          <span className="public-kicker">
            <i /> 基础客服长期免费 · 含 2 个坐席
          </span>
          <p>从一个链接开始，把客户、团队和品牌连在一起。</p>
          <div className="public-hero-actions">
            <Button type="primary" size="large" href="/register">
              免费开始 <ArrowRightOutlined />
            </Button>
            <Button size="large" href="/features">
              探索产品
            </Button>
          </div>
          <div className="public-hero-assurance">
            <span>免费 2 个客服坐席</span>
            <i />
            <span>基础服务长期免费</span>
            <i />
            <span>邮箱注册即可使用</span>
          </div>
        </div>
        <div className="public-hero-product">
          <ProductPreview />
        </div>
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
          <div>
            <div className="public-overline">从找到你，到服务好</div>
            <h2>服务的每一步，都有着落。</h2>
          </div>
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
          <span className="public-kicker">免费开始 · 按需成长</span>
          <h2>下一次好对话，从这里开始。</h2>
          <p>2 个客服坐席，足够开始。团队成长后，再选择适合你的套餐。</p>
          <Button type="primary" size="large" href="/register">
            创建你的工作空间 <ArrowRightOutlined />
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
