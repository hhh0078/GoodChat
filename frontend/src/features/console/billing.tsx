/** 文件作用说明：展示租户真实订阅、坐席占用与升级套餐申请，不伪造支付或开通状态。 */
"use client";
import { useEffect, useState } from "react";
import { Alert, App, Button, Card, Popconfirm, Space, Spin, Table, Tag } from "antd";
import { api, errorMessage } from "@/lib/api";
import { useSession } from "@/features/auth/session";
import { ErrorNotice, PageTitle } from "@/components/page-state";
import type { Resource } from "@/lib/types";
type Plan = Resource & {
  price_amount: number;
  validity_days: number;
  seat_limit: number;
  description: string;
};
type Subscription = {
  plan: Plan;
  subscription: Resource;
  usage: { seats_used: number; seats_reserved: number; seat_limit: number };
  available_plans: Plan[];
};
/** BillingPage 读取当前订阅，以明确的申请方式支持人工开通流程。 */
export function BillingPage() {
  const { tenantId } = useSession();
  const { message } = App.useApp();
  const [data, setData] = useState<Subscription | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState("");
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setData(null);
    setError(null);
    api<Subscription>("/tenant/subscription", { tenantId, signal: controller.signal })
      .then(setData)
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      });
    return () => controller.abort();
  }, [tenantId, revision]);
  /** requestUpgrade 创建真实升级申请，后续收费及开通由平台确认。 */
  async function requestUpgrade(plan: Plan) {
    setBusy(plan.id);
    try {
      await api("/tenant/upgrade-requests", {
        method: "POST",
        tenantId,
        bodyJSON: { plan_id: plan.id },
        idempotent: true,
      });
      message.success("升级申请已提交，等待平台确认付款与开通");
      setRevision((value) => value + 1);
    } catch (problem) {
      message.error(errorMessage(problem));
    } finally {
      setBusy("");
    }
  }
  return (
    <section className="console-screen console-billing">
      <PageTitle
        title="套餐与客服坐席"
        description="免费开始，根据团队规模申请扩展。管理员成员只有开启接待权限才占用客服坐席。"
      />
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      {!data && !error ? (
        <Spin />
      ) : (
        data && (
          <>
            {data.subscription?.status === "expired" && (
              <Alert
                type="warning"
                showIcon
                title="原订阅已到期，当前按免费版权益使用。历史记录保留。"
                style={{ marginBottom: 20 }}
              />
            )}
            <div className="stat-grid">
              <Card>
                <div className="muted">当前版本</div>
                <strong style={{ fontSize: 24 }}>{data.plan?.name || "未开通"}</strong>
              </Card>
              <Card>
                <div className="muted">已使用客服坐席</div>
                <div className="stat-value">
                  {data.usage.seats_used} / {data.usage.seat_limit}
                </div>
              </Card>
              <Card>
                <div className="muted">待接受邀请预留</div>
                <div className="stat-value">{data.usage.seats_reserved}</div>
              </Card>
              <Card>
                <div className="muted">服务到期时间</div>
                <strong>
                  {data.subscription?.expires_at
                    ? new Date(String(data.subscription.expires_at)).toLocaleDateString("zh-CN")
                    : data.subscription?.expires_at === null
                      ? "长期有效"
                      : "以订阅记录为准"}
                </strong>
              </Card>
            </div>
            <Alert
              type="info"
              showIcon
              title="升级采取申请确认方式"
              description="提交申请不会立即收费或改变套餐。平台确认收款与开通后，额度才会更新。"
              style={{ marginBottom: 24 }}
            />
            <div className="table-card">
              <h2 className="console-section-heading">可选版本</h2>
              <Table
                rowKey="id"
                dataSource={data.available_plans || []}
                pagination={false}
                columns={[
                  { title: "版本", dataIndex: "name" },
                  {
                    title: "价格",
                    dataIndex: "price_amount",
                    render: (value) => `¥${(Number(value) / 100).toLocaleString("zh-CN")}`,
                  },
                  { title: "客服坐席", dataIndex: "seat_limit" },
                  {
                    title: "有效期",
                    dataIndex: "validity_days",
                    render: (value) => (value === 0 ? "长期有效" : `${value} 天`),
                  },
                  {
                    title: "操作",
                    render: (_, plan) =>
                      plan.id === data.plan?.id ? (
                        <Tag color="blue">当前版本</Tag>
                      ) : (
                        <Popconfirm
                          title={`申请开通「${plan.name}」？`}
                          description="本操作提交开通申请，平台后续确认收费与生效。"
                          onConfirm={() => requestUpgrade(plan)}
                        >
                          <Button type="primary" loading={busy === plan.id}>
                            申请升级
                          </Button>
                        </Popconfirm>
                      ),
                  },
                ]}
              />
            </div>
          </>
        )
      )}
    </section>
  );
}
