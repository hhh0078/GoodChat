/** 文件作用说明：展示租户真实订阅、坐席占用与升级套餐申请，不伪造支付或开通状态。 */
"use client";
import { showRequestError } from "@/lib/request-error";
import { ErrorNotice, PageTitle } from "@/components/page-state";
import { useSession } from "@/features/auth/session";
import { useFormats } from "@/i18n/use-formats";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import type { Resource } from "@/lib/types";
import { Alert, App, Button, Card, Popconfirm, Spin, Table, Tag } from "antd";
import { useEffect, useState } from "react";
type Plan = Resource & {
  price_amount: number;
  validity_days: number;
  seat_limit: number;
  description: string;
};
type Subscription = {
  plan: Plan;
  subscription: Resource;
  usage: {
    seats_used: number;
    seats_reserved: number;
    seat_limit: number;
  };
  available_plans: Plan[];
};
/** BillingPage 读取当前订阅，以明确的申请方式支持人工开通流程。 */
export function BillingPage() {
  const { currentPreferences, numberFormatter } = useFormats();
  const locale = currentPreferences().locale;

  const tr = useTranslator();
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
      .then((result) => {
        if (!controller.signal.aborted) setData(result);
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      });
    return () => controller.abort();
  }, [tenantId, revision, locale]);
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
      message.success(tr("升级申请已提交，等待平台确认付款与开通"));
      setRevision((value) => value + 1);
    } catch (problem) {
      showRequestError(problem);
    } finally {
      setBusy("");
    }
  }
  return (
    <section className="console-screen console-billing">
      <PageTitle
        title={tr("套餐与客服坐席")}
        description={tr(
          "免费开始，根据团队规模申请扩展。管理员成员只有开启接待权限才占用客服坐席。",
        )}
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
                title={tr("原订阅已到期，当前按免费版权益使用。历史记录保留。")}
                style={{ marginBottom: 20 }}
              />
            )}
            <div className="stat-grid">
              <Card>
                <div className="muted">{tr("当前版本")}</div>
                <strong style={{ fontSize: 24 }}>{data.plan?.name || tr("未开通")}</strong>
              </Card>
              <Card>
                <div className="muted">{tr("已使用客服坐席")}</div>
                <div className="stat-value">
                  {data.usage.seats_used} / {data.usage.seat_limit}
                </div>
              </Card>
              <Card>
                <div className="muted">{tr("待接受邀请预留")}</div>
                <div className="stat-value">{data.usage.seats_reserved}</div>
              </Card>
              <Card>
                <div className="muted">{tr("服务到期时间")}</div>
                <strong>
                  {data.subscription?.expires_at
                    ? new Date(String(data.subscription.expires_at)).toLocaleDateString(
                        currentPreferences().locale,
                        { timeZone: currentPreferences().timezone },
                      )
                    : data.subscription?.expires_at === null
                      ? tr("长期有效")
                      : tr("以订阅记录为准")}
                </strong>
              </Card>
            </div>
            <Alert
              type="info"
              showIcon
              title={tr("升级采取申请确认方式")}
              description={tr(
                "提交申请不会立即收费或改变套餐。平台确认收款与开通后，额度才会更新。",
              )}
              style={{ marginBottom: 24 }}
            />
            <div className="table-card">
              <h2 className="console-section-heading">{tr("可选版本")}</h2>
              <Table
                rowKey="id"
                dataSource={data.available_plans || []}
                pagination={false}
                columns={[
                  { title: tr("版本"), dataIndex: "name" },
                  {
                    title: tr("价格"),
                    dataIndex: "price_amount",
                    render: (value) =>
                      `¥${(Number(value) / 100).toLocaleString(currentPreferences().locale)}`,
                  },
                  { title: tr("客服坐席"), dataIndex: "seat_limit" },
                  {
                    title: tr("有效期"),
                    dataIndex: "validity_days",
                    render: (value) =>
                      value === 0 ? tr("长期有效") : tr("{p0} 天", { p0: String(value) }),
                  },
                  {
                    title: tr("操作"),
                    render: (_, plan) =>
                      plan.id === data.plan?.id ? (
                        <Tag color="blue">{tr("当前版本")}</Tag>
                      ) : (
                        <Popconfirm
                          title={tr("申请开通「{p0}」？", { p0: String(plan.name) })}
                          description={tr("本操作提交开通申请，平台后续确认收费与生效。")}
                          onConfirm={() => requestUpgrade(plan)}
                        >
                          <Button type="primary" loading={busy === plan.id}>
                            {tr("申请升级 ")}
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
