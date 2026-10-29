/** 文件作用说明：在独立页面加载真实服务统计，按时间筛选并展示统一口径。 */
"use client";
import { useEffect, useState, type ComponentProps } from "react";
import { Button, Card, DatePicker, Space, Spin, Table } from "antd";
import { InfoCircleOutlined } from "@ant-design/icons";
import { api } from "@/lib/api";
import { useSession } from "@/features/auth/session";
import { ErrorNotice, PageTitle, EmptyData } from "@/components/page-state";
import { ReportTrend, type TrendPoint } from "./report-trend";
const metricNumber = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 2 });
type Statistics = {
  overview: Record<string, number>;
  by_agent: {
    agent_id: string;
    agent_name: string;
    conversations: number;
    active: number;
    closed: number;
  }[];
  by_channel: { channel_code: string; conversations: number; visitors: number }[];
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
    ["访客数", "visitors"],
    ["客户数", "contacts"],
    ["咨询会话", "conversations"],
    ["消息数", "messages"],
    ["排队中", "queued"],
    ["接待中", "active"],
    ["已结束", "closed"],
    ["平均首次回复秒数", "avg_first_response_seconds"],
  ];
  return (
    <section className="console-screen console-reports">
      <PageTitle
        title="服务统计"
        description="访问、客户、咨询和消息分别统计；转接不额外增加咨询会话数。"
        action={
          <Space wrap>
            <span className="muted">统计时间</span>
            <Button
              type={range ? "default" : "primary"}
              onClick={() => {
                setRange(null);
                setRevision((value) => value + 1);
              }}
            >
              今天
            </Button>
            <DatePicker.RangePicker
              aria-label="统计日期"
              value={range}
              placeholder={["开始", "结束"]}
              style={{ width: 240 }}
              onChange={setRange}
            />
          </Space>
        }
      />
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      {data && (
        <p className="console-report-period">
          {range ? "所选日期" : "今天"} ·{" "}
          {new Intl.DateTimeFormat("zh-CN", {
            timeZone: data.timezone,
            dateStyle: "medium",
          }).format(new Date(data.from))}{" "}
          —{" "}
          {new Intl.DateTimeFormat("zh-CN", {
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
          <Spin description="正在读取服务趋势" />
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
          <h2>按客服统计</h2>
          <Table
            rowKey="agent_id"
            loading={loading}
            dataSource={data?.by_agent || []}
            pagination={{ pageSize: 8 }}
            scroll={{ x: 480 }}
            locale={{ emptyText: <EmptyData /> }}
            columns={[
              { title: "客服", dataIndex: "agent_name" },
              { title: "咨询", dataIndex: "conversations" },
              { title: "接待中", dataIndex: "active" },
              { title: "已结束", dataIndex: "closed" },
            ]}
          />
        </section>
        <section className="console-report-section">
          <h2>按入口来源统计</h2>
          <Table
            rowKey="channel_code"
            loading={loading}
            dataSource={data?.by_channel || []}
            pagination={{ pageSize: 8 }}
            locale={{ emptyText: <EmptyData /> }}
            columns={[
              { title: "来源", dataIndex: "channel_code", render: (value) => value || "未知来源" },
              { title: "访客", dataIndex: "visitors" },
              { title: "咨询", dataIndex: "conversations" },
            ]}
          />
        </section>
      </div>
      <section className="console-report-section console-report-daily">
        <h2>{data?.trend_interval === "hour" ? "时段明细" : "每日明细"}</h2>
        <Table
          rowKey={(item) => item.timestamp || item.date}
          loading={loading}
          dataSource={data?.trend || data?.daily || []}
          pagination={{ pageSize: 14 }}
          locale={{ emptyText: <EmptyData /> }}
          columns={[
            { title: data?.trend_interval === "hour" ? "时段" : "日期", dataIndex: "date" },
            { title: "访客数", dataIndex: "visitors" },
            { title: "咨询数", dataIndex: "conversations" },
            { title: "消息数", dataIndex: "messages" },
          ]}
        />
      </section>
      <p className="console-report-note">
        <InfoCircleOutlined /> 系统欢迎语与自动回复不计入人工首响；无有效样本时以服务端返回值为准。
      </p>
    </section>
  );
}
