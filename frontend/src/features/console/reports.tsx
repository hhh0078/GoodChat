/** 文件作用说明：在独立页面加载真实服务统计，按时间筛选并展示统一口径。 */
"use client";
import { useEffect, useState } from "react";
import { Alert, Card, DatePicker, Space, Spin, Table } from "antd";
import { api } from "@/lib/api";
import { useSession } from "@/features/auth/session";
import { ErrorNotice, PageTitle, EmptyData } from "@/components/page-state";
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
  daily: { date: string; visitors: number; conversations: number; messages: number }[];
};
/** ReportsPage 将统计与聊天记录分开，所有指标来自服务端。 */
export function ReportsPage() {
  const { tenantId } = useSession();
  const [data, setData] = useState<Statistics | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [range, setRange] = useState<string[]>([]);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setData(null);
    setError(null);
    setLoading(true);
    const query = new URLSearchParams();
    if (range[0]) query.set("from", range[0]);
    if (range[1]) query.set("to", range[1]);
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
    <>
      <PageTitle
        title="服务统计"
        description="访问、客户、咨询和消息分别统计；转接不额外增加咨询会话数。"
      />
      <Space style={{ marginBottom: 22 }}>
        <span className="muted">统计时间</span>
        <DatePicker.RangePicker
          onChange={(values) =>
            setRange(
              values?.[0] && values?.[1]
                ? [values[0].startOf("day").toISOString(), values[1].endOf("day").toISOString()]
                : [],
            )
          }
        />
      </Space>
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      <Alert
        type="info"
        showIcon
        title="系统欢迎语与自动回复不计入人工首响；无有效样本时以服务端返回值为准。"
      />
      <div className="stat-grid">
        {metrics.map(([label, key]) => (
          <Card size="small" key={key}>
            <div className="muted">{label}</div>
            <div className="stat-value">
              {loading ? <Spin size="small" /> : (data?.overview?.[key] ?? "—")}
            </div>
          </Card>
        ))}
      </div>
      <div className="two-columns">
        <Card title="按客服统计">
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
        </Card>
        <Card title="按入口来源统计">
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
        </Card>
      </div>
      <Card title="每日趋势" style={{ marginTop: 22 }}>
        <Table
          rowKey="date"
          loading={loading}
          dataSource={data?.daily || []}
          pagination={{ pageSize: 14 }}
          locale={{ emptyText: <EmptyData /> }}
          columns={[
            { title: "日期", dataIndex: "date" },
            { title: "访客数", dataIndex: "visitors" },
            { title: "咨询数", dataIndex: "conversations" },
            { title: "消息数", dataIndex: "messages" },
          ]}
        />
      </Card>
    </>
  );
}
