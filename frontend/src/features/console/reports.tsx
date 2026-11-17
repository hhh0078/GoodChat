/** 文件作用说明：在独立页面加载真实服务统计，按时间筛选并展示统一口径。 */
"use client";
import { EmptyData, ErrorNotice, PageTitle } from "@/components/page-state";
import { useSession } from "@/features/auth/session";
import { useFormats } from "@/i18n/use-formats";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import { InfoCircleOutlined } from "@ant-design/icons";
import { Button, Card, DatePicker, Space, Spin, Table } from "antd";
import { useEffect, useState, type ComponentProps } from "react";
import { ReportTrend, type TrendPoint } from "./report-trend";
type Statistics = {
  overview: Record<string, number>;
  by_agent: {
    agent_id: string;
    agent_name: string;
    conversations: number;
    active: number;
    closed: number;
  }[];
  by_channel: {
    channel_code: string;
    conversations: number;
    visitors: number;
  }[];
  daily: TrendPoint[];
  trend: TrendPoint[];
  trend_interval: "hour" | "day";
  from: string;
  to: string;
  timezone: string;
};
type PickerRange = Parameters<
  NonNullable<ComponentProps<typeof DatePicker.RangePicker>["onChange"]>
>[0];
/** ReportsPage 将统计与聊天记录分开，所有指标来自服务端。 */
export function ReportsPage() {
  const { currentPreferences, numberFormatter, dateTimeFormatter: dateFormatter } = useFormats();
  const metricNumber = numberFormatter({ maximumFractionDigits: 2 });

  const tr = useTranslator();
  const { tenantId } = useSession();
  const [data, setData] = useState<Statistics | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [range, setRange] = useState<PickerRange>(null);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setData(null);
    setError(null);
    setLoading(true);
    const query = new URLSearchParams();
    if (range?.[0]) query.set("from", range[0].format("YYYY-MM-DD"));
    if (range?.[1]) query.set("to", range[1].format("YYYY-MM-DD"));
    api<Statistics>(`/tenant/statistics?${query}`, { tenantId, signal: controller.signal })
      .then(setData)
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [tenantId, range, revision]);
  const metrics = [
    [tr("访客数"), "visitors"],
    [tr("客户数"), "contacts"],
    [tr("咨询会话"), "conversations"],
    [tr("消息数"), "messages"],
    [tr("排队中"), "queued"],
    [tr("接待中"), "active"],
    [tr("已结束"), "closed"],
    [tr("平均首次回复秒数"), "avg_first_response_seconds"],
  ];
  return (
    <section className="console-screen console-reports">
      <PageTitle
        title={tr("服务统计")}
        description={tr("访问、客户、咨询和消息分别统计；转接不额外增加咨询会话数。")}
        action={
          <Space wrap>
            <span className="muted">{tr("统计时间")}</span>
            <Button
              type={range ? "default" : "primary"}
              onClick={() => {
                setRange(null);
                setRevision((value) => value + 1);
              }}
            >
              {tr("今天 ")}
            </Button>
            <DatePicker.RangePicker
              aria-label={tr("统计日期")}
              value={range}
              placeholder={[tr("开始"), tr("结束")]}
              style={{ width: 240 }}
              onChange={setRange}
            />
          </Space>
        }
      />
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      {data && (
        <p className="console-report-period">
          {range ? tr("所选日期") : tr("今天")} ·{" "}
          {dateFormatter({
            timeZone: data.timezone,
            dateStyle: "medium",
          }).format(new Date(data.from))}{" "}
          —{" "}
          {dateFormatter({
            timeZone: data.timezone,
            dateStyle: "medium",
          }).format(new Date(new Date(data.to).getTime() - 1))}{" "}
          <span className="muted">（{data.timezone}）</span>
        </p>
      )}

      <div className="stat-grid">
        {metrics.map(([label, key]) => (
          <Card size="small" key={key}>
            <div className="muted">{label}</div>
            <div className="stat-value">
              {loading ? (
                <Spin size="small" />
              ) : typeof data?.overview?.[key] === "number" &&
                Number.isFinite(data.overview[key]) ? (
                metricNumber.format(data.overview[key])
              ) : (
                "—"
              )}
            </div>
          </Card>
        ))}
      </div>
      {loading ? (
        <div className="report-trend-loading">
          <Spin description={tr("正在读取服务趋势")} />
        </div>
      ) : (
        data && (
          <ReportTrend
            items={data.trend || data.daily || []}
            hourly={data.trend_interval === "hour"}
          />
        )
      )}
      <div className="two-columns console-report-tables">
        <section className="console-report-section">
          <h2>{tr("按客服统计")}</h2>
          <Table
            rowKey="agent_id"
            loading={loading}
            dataSource={data?.by_agent || []}
            pagination={{ pageSize: 8 }}
            scroll={{ x: 480 }}
            locale={{ emptyText: <EmptyData /> }}
            columns={[
              { title: tr("客服"), dataIndex: "agent_name" },
              { title: tr("咨询"), dataIndex: "conversations" },
              { title: tr("接待中"), dataIndex: "active" },
              { title: tr("已结束"), dataIndex: "closed" },
            ]}
          />
        </section>
        <section className="console-report-section">
          <h2>{tr("按入口来源统计")}</h2>
          <Table
            rowKey="channel_code"
            loading={loading}
            dataSource={data?.by_channel || []}
            pagination={{ pageSize: 8 }}
            locale={{ emptyText: <EmptyData /> }}
            columns={[
              {
                title: tr("来源"),
                dataIndex: "channel_code",
                render: (value) => value || tr("未知来源"),
              },
              { title: tr("访客"), dataIndex: "visitors" },
              { title: tr("咨询"), dataIndex: "conversations" },
            ]}
          />
        </section>
      </div>
      <section className="console-report-section console-report-daily">
        <h2>{data?.trend_interval === "hour" ? tr("时段明细") : tr("每日明细")}</h2>
        <Table
          rowKey={(item) => item.timestamp || item.date}
          loading={loading}
          dataSource={data?.trend || data?.daily || []}
          pagination={{ pageSize: 14 }}
          locale={{ emptyText: <EmptyData /> }}
          columns={[
            { title: data?.trend_interval === "hour" ? tr("时段") : tr("日期"), dataIndex: "date" },
            { title: tr("访客数"), dataIndex: "visitors" },
            { title: tr("咨询数"), dataIndex: "conversations" },
            { title: tr("消息数"), dataIndex: "messages" },
          ]}
        />
      </section>
      <p className="console-report-note">
        <InfoCircleOutlined />
        {tr(" 系统欢迎语与自动回复不计入人工首响；无有效样本时以服务端返回值为准。 ")}
      </p>
    </section>
  );
}
