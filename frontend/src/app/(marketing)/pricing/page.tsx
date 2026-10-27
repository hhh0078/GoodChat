/** 文件作用说明：按明亮定价设计图服务端读取平台套餐，公开展示真实价格、有效期、坐席与功能。 */
import { Alert, Button, Tag } from "antd";
import type { Metadata } from "next";
import { ArrowRightOutlined, CheckOutlined } from "@/components/public-icons";
export const dynamic = "force-dynamic";
export const metadata: Metadata = {
  title: "免费开始与套餐价格",
  description: "查看 GoodChat 已发布套餐、客服坐席额度、服务有效期与功能范围。",
  alternates: { canonical: "/pricing" },
};
type Plan = {
  id: string;
  name: string;
  description: string;
  price_amount: number;
  currency: string;
  validity_days: number;
  seat_limit: number;
  features: Record<string, unknown>;
};
const featureLabels: Record<string, string> = {
  ai_design: "AI 页面装修",
  advanced_design: "高级装修",
  custom_code: "代码装修",
  callbacks: "进入回调",
  statistics: "服务统计",
  chat: "在线聊天",
  templates: "私有模板",
  multi_link: "多链接接入",
  labels: "客户标签",
  decoration: "页面装修",
  advanced_decoration: "高级装修",
  export: "聊天记录导出",
};
const featureDescriptions: Record<string, string> = {
  chat: "与访客实时沟通，集中接待每一次咨询。",
  multi_link: "不同业务使用独立链接，汇聚到同一个工作空间。",
  labels: "按标签整理客户，方便分类与跟进。",
  decoration: "调整品牌名称、配色与欢迎内容。",
  advanced_decoration: "通过布局、CSS 与代码设计自己的页面。",
  advanced_design: "通过布局、CSS 与代码设计自己的页面。",
  custom_code: "使用自己的 HTML、CSS 与 JavaScript 页面。",
  ai_design: "描述设计想法，生成草稿并预览发布。",
  export: "按权限导出聊天记录，便于存档。",
  statistics: "查看咨询量、接待情况与服务趋势。",
  callbacks: "访客进入后，向已配置的业务地址发送回调。",
  templates: "将自己的页面设计保留为租户私有模板。",
};
/** PricingPage 使用后端公开配置输出价格，连接失败时明确显示配置状态。 */
export default async function PricingPage() {
  let plans: Plan[] = [];
  let unavailable = false;
  try {
    const response = await fetch(
      `${process.env.BACKEND_URL || "http://127.0.0.1:18080"}/api/v1/public/plans`,
      { cache: "no-store", signal: AbortSignal.timeout(4000) },
    );
    if (!response.ok) throw new Error("套餐暂不可用");
    const payload = await response.json();
    plans = Array.isArray(payload?.data?.items) ? payload.data.items : [];
  } catch {
    unavailable = true;
  }
  return (
    <div className="public-width public-pricing" data-public-page="pricing">
      <header className="public-pricing-heading">
        <div className="public-overline">从一个免费工作空间开始</div>
        <h1>免费 2 个客服坐席，按需扩展团队。</h1>
        <p className="page-lead">
          根据团队人数选择客服坐席与服务能力，具体价格和有效期以已发布套餐为准。
        </p>
      </header>
      {unavailable || !plans.length ? (
        <Alert
          type="info"
          showIcon
          title="套餐信息正在配置中"
          description="当前暂时无法读取已发布套餐。你可以先创建工作空间，在控制台查看实际可用额度。"
        />
      ) : (
        <div className={`public-pricing-plans ${plans.length === 1 ? "is-single" : ""}`}>
          {plans.map((plan) => (
            <article className="public-plan" key={plan.id}>
              <div className="public-plan-summary">
                <Tag color={plan.price_amount === 0 ? "green" : "default"}>
                  {plan.price_amount === 0 ? "免费开始" : "扩展团队"}
                </Tag>
                <h2>{plan.name}</h2>
                <div className="public-plan-price">
                  {plan.currency === "CNY" ? "¥" : plan.currency}
                  {(plan.price_amount / 100).toLocaleString("zh-CN", { maximumFractionDigits: 2 })}
                  <span>
                    {" "}
                    / {plan.validity_days === 0 ? "长期有效" : `${plan.validity_days} 天`}
                  </span>
                </div>
                <strong className="public-plan-seats">{plan.seat_limit} 个客服坐席</strong>
                <p>{plan.description}</p>
                <Button
                  type="primary"
                  block
                  href={plan.price_amount === 0 ? "/register" : "/console/billing"}
                >
                  {plan.price_amount === 0 ? "免费创建工作空间" : "查看开通方式"}
                </Button>
              </div>
              <div className="public-plan-capabilities">
                <h3>包含的核心能力</h3>
                <ul>
                  {Object.entries(plan.features || {})
                    .filter(([, value]) => !!value)
                    .map(([key, value]) => (
                      <li key={key}>
                        <CheckOutlined />
                        <span>
                          <strong>
                            {featureLabels[key] || key}
                            {typeof value === "number" ? `：${value}` : ""}
                          </strong>
                          {featureDescriptions[key] && <small>{featureDescriptions[key]}</small>}
                        </span>
                      </li>
                    ))}
                </ul>
              </div>
            </article>
          ))}
        </div>
      )}
      <section className="public-upgrade-note">
        <h2>扩展团队时再升级</h2>
        <p>需要更多客服坐席，可以在控制台申请升级。付费版本与价格由平台发布后展示。</p>
        <a href="/console/billing">
          查看开通方式 <ArrowRightOutlined />
        </a>
      </section>
      <p className="public-price-notice">
        基础免费版默认 0 元、长期有效，包含 2 个客服坐席。具体功能、额度与有效期以实际开通记录为准。
      </p>
    </div>
  );
}
