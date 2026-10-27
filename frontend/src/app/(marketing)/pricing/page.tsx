/** 文件作用说明：由服务端读取平台已发布套餐，输出可索引的真实价格和客服坐席额度。 */
import { Alert, Button, Tag } from "antd";
import type { Metadata } from "next";
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
    plans = payload?.data?.items || [];
  } catch {
    unavailable = true;
  }
  return (
    <div className="public-width content-page">
      <div className="eyebrow">START WITH A CONVERSATION</div>
      <h1>
        免费 2 个客服坐席，
        <br />
        按需扩展团队。
      </h1>
      <p className="page-lead">
        根据团队人数选择客服坐席与服务能力，具体价格和有效期以已发布套餐为准。
      </p>
      {unavailable || !plans.length ? (
        <Alert
          type="info"
          showIcon
          title="套餐信息正在配置中"
          description="当前暂时无法读取已发布套餐。你可以先创建工作空间，在控制台查看实际可用额度。"
        />
      ) : (
        <div className="feature-grid" style={{ marginTop: 32 }}>
          {plans.map((plan) => (
            <article className="feature-card" key={plan.id}>
              <Tag color={plan.price_amount === 0 ? "green" : "default"}>
                {plan.price_amount === 0 ? "免费开始" : "扩展团队"}
              </Tag>
              <h2 style={{ marginTop: 16 }}>{plan.name}</h2>
              <div style={{ fontSize: 38, fontWeight: 700, color: "#23684e" }}>
                ¥{(plan.price_amount / 100).toLocaleString("zh-CN", { maximumFractionDigits: 2 })}
                <span style={{ fontSize: 13, fontWeight: 400, color: "#68776e" }}>
                  {" "}
                  / {plan.validity_days === 0 ? "长期有效" : `${plan.validity_days} 天`}
                </span>
              </div>
              <p style={{ margin: "16px 0" }}>{plan.description}</p>
              <strong>{plan.seat_limit} 个客服坐席</strong>
              <ul className="clean-list">
                {Object.entries(plan.features || {})
                  .filter(([, value]) => !!value)
                  .map(([key, value]) => (
                    <li key={key}>
                      ✓{" "}
                      {(
                        {
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
                        } as Record<string, string>
                      )[key] || key}
                      {typeof value === "number" ? `：${value}` : ""}
                    </li>
                  ))}
              </ul>
              <Button
                type="primary"
                block
                href={plan.price_amount === 0 ? "/register" : "/console/billing"}
              >
                {plan.price_amount === 0 ? "免费创建工作空间" : "查看开通方式"}
              </Button>
            </article>
          ))}
        </div>
      )}
      <div className="notice-box">
        基础免费版默认 0 元、长期有效，包含 2
        个客服坐席。超出套餐额度需要申请升级；管理员可调整已发布版本的价格、功能与有效期，实际开通以控制台记录为准。
      </div>
    </div>
  );
}
