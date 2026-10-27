/** 文件作用说明：提供可索引的 GoodChat 官网首页，介绍客服接待与自主装修能力。 */
import type { Metadata } from "next";
import { Button } from "antd";
import {
  ArrowRightOutlined,
  ApartmentOutlined,
  CheckOutlined,
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
const features = [
  {
    icon: LinkOutlined,
    title: "一个团队，多种入口",
    text: "售前、售后、广告和门店各用自己的接入链接。电脑和手机打开即可咨询。",
  },
  {
    icon: ApartmentOutlined,
    title: "把咨询分给合适的人",
    text: "按客服组、分配池和接待状态安排咨询，支持轮询、负载优先和人工领取。",
  },
  {
    icon: MessageOutlined,
    title: "接待进度，放在一起",
    text: "消息、客户资料、来源和接待记录汇集到工作台，让每次沟通有据可查。",
  },
  {
    icon: SkinOutlined,
    title: "聊天页面也有品牌感",
    text: "从基础配色到布局、代码装修，再到 AI 创作，把设计保存为自己的模板。",
  },
  {
    icon: TeamOutlined,
    title: "成员分工，清楚可控",
    text: "分别设置管理员与客服角色、接待资格和服务范围，共同维护一个工作空间。",
  },
  {
    icon: SafetyOutlined,
    title: "每家商户，独立管理",
    text: "租户的成员、消息和模板分别授权，管理与接待都遵循明确的权限范围。",
  },
];
/** HomePage 输出官网主要内容与明确标注的界面示意。 */
export default function HomePage() {
  return (
    <div className="public-width">
      <section className="hero">
        <div>
          <div className="eyebrow">GOOD CONVERSATIONS START HERE</div>
          <h1>
            基础客服，
            <br />
            永久免费。
          </h1>
          <p className="hero-description">
            免费包含 2 个客服坐席，从一个聊天链接开始。GoodChat
            把入口、分配、消息和品牌页面连在一起，团队扩展时再按需升级。
          </p>
          <div className="hero-buttons">
            <Button
              type="primary"
              size="large"
              href="/register"
              icon={<ArrowRightOutlined />}
              iconPlacement="end"
            >
              免费开始
            </Button>
            <Button size="large" href="/features">
              了解产品
            </Button>
          </div>
          <p className="hero-note">免费开始，包含 2 个客服坐席 · 电脑与手机适配</p>
        </div>
        <div className="product-preview" aria-label="客服工作台界面示意，非真实聊天数据">
          <div className="preview-top">
            <span>GoodChat 工作台</span>
            <span className="muted">界面示意</span>
            <div className="preview-dots">
              <i />
              <i />
              <i />
            </div>
          </div>
          <div className="preview-body">
            <aside className="preview-side">
              <b>我的接待</b>
              <div className="preview-item active">售前咨询</div>
              <div className="preview-item">售后服务</div>
              <div className="preview-item">客户资料</div>
            </aside>
            <div className="preview-chat">
              <div className="preview-chat-title">每一段对话，都从这里开始</div>
              <div className="bubble">你好，想了解一下如何接入我的网站。</div>
              <div className="bubble agent">你好！创建一个聊天链接，就可以开始接待。</div>
              <div className="preview-composer">把客户的问题，一起解决好。</div>
            </div>
          </div>
        </div>
      </section>
      <section className="section">
        <div className="section-heading">
          <div className="eyebrow">ONE CONNECTED WORKSPACE</div>
          <h2>
            把接待的每一步，
            <br />
            放进同一个工作空间。
          </h2>
        </div>
        <div className="feature-grid">
          {features.map((item) => (
            <article className="feature-card" key={item.title}>
              <div className="feature-icon">
                <item.icon />
              </div>
              <h3>{item.title}</h3>
              <p>{item.text}</p>
            </article>
          ))}
        </div>
      </section>
      <section className="section split-section">
        <div className="design-board" aria-hidden="true">
          <div className="mini-theme">
            <h4>你的品牌 · 你的客服</h4>
            <div className="mini-line" />
            <div className="mini-line short" />
            <div className="mini-line" />
            <div className="mini-send" />
          </div>
          <div className="mini-theme dark">
            <h4>让页面，长成你想要的样子</h4>
            <div className="mini-line" />
            <div className="mini-line short" />
            <div className="mini-send" />
          </div>
        </div>
        <div>
          <div className="eyebrow">MAKE IT YOURS</div>
          <h2>
            服务是你的，
            <br />
            页面也应该是。
          </h2>
          <p className="muted">
            选择内置模板，用组件调整布局，或用自然语言描述你的想法。设计保存到租户自己的模板库，预览后发布到接入链接。
          </p>
          <ul className="clean-list">
            {[
              "电脑与手机一起预览",
              "基础、拖拽和代码装修",
              "AI 创作保留为私有模板",
              "发布有版本，修改可回退",
            ].map((text) => (
              <li key={text}>
                <CheckOutlined className="check" />
                {text}
              </li>
            ))}
          </ul>
          <Button href="/register" type="primary">
            开始创建 <ArrowRightOutlined />
          </Button>
        </div>
      </section>
      <section className="section">
        <div className="notice-box">
          <strong>从网页开始，把接待做扎实。</strong>
          <p style={{ margin: "8px 0 0" }}>
            当前以网页接待为起点。macOS、Windows、Android
            客服端和第三方平台渠道将在后续分别接入与验收，具体可用能力以控制台为准。
          </p>
        </div>
      </section>
    </div>
  );
}
