/** 文件作用说明：展示租户AI余额、真实Token用量及独立账本，费用按微元保留精度。 */
"use client";
import { ErrorNotice } from "@/components/page-state";
import { useSession } from "@/features/auth/session";
import { useFormats } from "@/i18n/use-formats";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import { Alert, Button, Card, Space, Statistic, Table, Tag } from "antd";
import { useLocale } from "next-intl";
import { useEffect, useState } from "react";
import type { AIUsageData } from "./types";

/** AIUsagePage 普通成员只看本人用量，管理员额外查看租户钱包和充值流水。 */
export function AIUsagePage() {
  const tr = useTranslator();
  const locale = useLocale();
  const { tenantId } = useSession();
  const { numberFormatter, dateTimeFormatter } = useFormats();
  const [data, setData] = useState<AIUsageData | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setError(null);
    api<AIUsageData>("/tenant/ai/usage", { tenantId, signal: controller.signal })
      .then((value) => {
        if (!controller.signal.aborted) setData(value);
      })
      .catch((e) => {
        if (!controller.signal.aborted) setError(e);
      });
    return () => controller.abort();
  }, [tenantId, locale, revision]);
  /** money 展示人民币微元，避免小额使用全部显示为零。 */
  function money(value: number) {
    return numberFormatter({
      style: "currency",
      currency: "CNY",
      minimumFractionDigits: 2,
      maximumFractionDigits: 6,
    }).format(value / 1000000);
  }
  const statuses: Record<string, string> = {
    queued: tr("等待处理"),
    running: tr("生成中"),
    draft: tr("待人工确认"),
    succeeded: tr("已完成"),
    failed: tr("失败"),
    cancelled: tr("已取消"),
    uncertain: tr("用量待核对"),
  };
  const kinds: Record<string, string> = {
    summary: tr("知识库描述"),
    restore: tr("知识库回滚"),
    reply: tr("客服回复"),
    test: tr("调试"),
  };
  return (
    <div className="ai-resource">
      <p className="ai-help">
        {tr(
          "输入Token包含文字、知识库、上下文及图片；图片按供应商返回的实际输入用量计费，不按上传张数额外收费。",
        )}
      </p>
      <ErrorNotice error={error} retry={() => setRevision((v) => v + 1)} />
      {data?.wallet?.unlimited && (
        <p className="ai-help">
          {tr("当前为不限制余额的运营模式，费用照常记录，负余额表示累计使用费用。")}
        </p>
      )}
      <div className="ai-toolbar">
        <p className="ai-help">
          {tr("知识库描述、调试和辅助回复均产生AI用量。待核对任务保留预算，核对前不会猜测扣费。")}
        </p>
        <Button onClick={() => setRevision((v) => v + 1)}>{tr("刷新")}</Button>
      </div>
      {data?.wallet && (
        <Space wrap className="ai-wallet">
          <Card>
            <Statistic title={tr("AI账面余额")} value={money(data.wallet.balance_micro)} />
          </Card>
          <Card>
            <Statistic title={tr("预算预占")} value={money(data.wallet.reserved_micro)} />
          </Card>
          <Card>
            <Statistic
              title={tr("当前可用余额")}
              value={
                data.wallet.unlimited
                  ? tr("不限制")
                  : money(data.wallet.balance_micro - data.wallet.reserved_micro)
              }
            />
          </Card>
        </Space>
      )}
      {(data?.jobs || []).some((j) => j.status === "uncertain") && (
        <Alert type="warning" showIcon title={tr("存在用量待核对任务，请联系平台处理。")} />
      )}
      <Table
        rowKey="id"
        dataSource={data?.jobs || []}
        loading={!data && !error}
        scroll={{ x: 750 }}
        pagination={{ pageSize: 10 }}
        columns={[
          {
            title: tr("类型"),
            dataIndex: "kind",
            render: (value: string) => kinds[value] || value,
          },
          {
            title: tr("状态"),
            dataIndex: "status",
            render: (value: string) => <Tag>{statuses[value] || value}</Tag>,
          },
          { title: tr("输入Token"), dataIndex: "input_tokens" },
          { title: tr("输出Token"), dataIndex: "output_tokens" },
          { title: tr("图片输入次数"), dataIndex: "image_inputs" },
          {
            title: tr("已扣费用"),
            dataIndex: "charged_micro",
            render: (value: number) => money(value || 0),
          },
          { title: tr("成员"), dataIndex: "owner_name" },
          {
            title: tr("时间"),
            dataIndex: "created_at",
            render: (value: string) =>
              dateTimeFormatter({ dateStyle: "short", timeStyle: "short" }).format(new Date(value)),
          },
        ]}
      />
      {!!data?.entries && (
        <Card title={tr("余额流水")}>
          <Table
            rowKey="id"
            dataSource={data.entries}
            scroll={{ x: 600 }}
            pagination={{ pageSize: 10 }}
            columns={[
              {
                title: tr("类型"),
                dataIndex: "kind",
                render: (value: string) =>
                  ({ topup: tr("充值"), charge: tr("消费"), adjustment: tr("核对调整") })[value] ||
                  value,
              },
              {
                title: tr("金额"),
                dataIndex: "delta_micro",
                render: (value: number) => money(value),
              },
              {
                title: tr("说明"),
                dataIndex: "reason",
                render: (value: string) => kinds[value] || value,
              },
              {
                title: tr("时间"),
                dataIndex: "created_at",
                render: (value: string) =>
                  dateTimeFormatter({ dateStyle: "short", timeStyle: "short" }).format(
                    new Date(value),
                  ),
              },
            ]}
          />
        </Card>
      )}
    </div>
  );
}
