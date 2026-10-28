/** 文件作用说明：为租户管理员展示成员在线时长、接待效率和按最终接待人归属的真实评价。 */
"use client";
import { useEffect, useState, type ComponentProps } from "react";
import { Alert, Avatar, Badge, Button, Card, DatePicker, Space, Table, Tag, Tooltip } from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import { api, ApiError } from "@/lib/api";
import {
  reportScopeKey,
  scopedReport,
  serialPoll,
  type ReportSnapshot,
} from "@/lib/reporting-state";
import { useSession } from "@/features/auth/session";
import { EmptyData, ErrorNotice, PageTitle } from "@/components/page-state";
import styles from "./operations-statistics.module.css";

type MemberMetrics = {
  id: string;
  display_name: string;
  avatar_url: string;
  role: string;
  can_serve: boolean;
  disabled: boolean;
  removed_at: string | null;
  online: boolean;
  online_seconds: number;
  last_seen_at: string | null;
  available: boolean;
  busy: boolean;
  current_load: number;
  capacity: number;
  received_conversations: number;
  assignments: number;
  closed_conversations: number;
  active_conversations: number;
  public_messages: number;
  withdrawn_messages: number;
  first_response_samples: number;
  avg_first_response_seconds: number | null;
  reply_samples: number;
  avg_reply_seconds: number | null;
  evaluation_count: number;
  average_score: number | null;
  satisfaction_percent: number | null;
};
type MemberReport = {
  items: MemberMetrics[];
  from: string;
  to: string;
  timezone: string;
  presence_started_at: string | null;
  definitions: Record<string, string>;
};
type PickerRange = Parameters<
  NonNullable<ComponentProps<typeof DatePicker.RangePicker>["onChange"]>
>[0];
const number = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 1 });

/** metricMean 使用实际样本数加权，不把没有首响或评价的成员当作零分计入。 */
function metricMean(
  items: MemberMetrics[],
  value: "avg_first_response_seconds" | "average_score" | "avg_reply_seconds",
  weight: "first_response_samples" | "evaluation_count" | "reply_samples",
) {
  const samples = items.reduce((sum, item) => sum + (item[value] === null ? 0 : item[weight]), 0);
  if (!samples) return null;
  return (
    items.reduce((sum, item) => sum + (item[value] === null ? 0 : item[value]! * item[weight]), 0) /
    samples
  );
}

/** MemberStatisticsPage 按租户时区默认今天，服务端持续刷新当前在线与本期历史指标。 */
export function MemberStatisticsPage() {
  const { current, tenantId } = useSession();
  const manages = current.memberships.some(
    (item) => item.tenant_id === tenantId && ["owner", "admin"].includes(item.role),
  );
  const [result, setResult] = useState<ReportSnapshot<MemberReport> | null>(null);
  const [range, setRange] = useState<PickerRange>(null);
  const [revision, setRevision] = useState(0);
  const [failure, setFailure] = useState<ReportSnapshot<unknown> | null>(null);
  const [loading, setLoading] = useState(true);
  const query = new URLSearchParams();
  if (range?.[0]) query.set("from", range[0].format("YYYY-MM-DD"));
  if (range?.[1]) query.set("to", range[1].format("YYYY-MM-DD"));
  const rangeKey = query.toString();
  const defaultDay = new Date().toLocaleDateString("en-CA", { timeZone: result?.data.timezone });
  const scope = reportScopeKey(tenantId, rangeKey || `today:${defaultDay}`);
  useEffect(() => {
    if (!manages || !tenantId) {
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    /** loadStatistics 读取当前筛选统计，切换租户或离开页面后不再更新旧结果。 */
    const loadStatistics = serialPoll(controller.signal, async () => {
      setLoading(true);
      try {
        const data = await api<MemberReport>(`/tenant/member-statistics?${rangeKey}`, {
          tenantId,
          signal: controller.signal,
        });
        if (!controller.signal.aborted) {
          setResult({ scope, data });
          setFailure(null);
        }
      } catch (problem) {
        if (!controller.signal.aborted) {
          setFailure({ scope, data: problem });
          if (problem instanceof ApiError && [401, 403].includes(problem.status))
            setResult((previous) => (previous?.scope === scope ? null : previous));
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    });
    void loadStatistics();
    const timer = setInterval(() => void loadStatistics(), 15000);
    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [tenantId, manages, rangeKey, scope, revision]);
  const data = scopedReport(result, scope);
  const error = scopedReport(failure, scope);
  const dataStart = data
    ? new Date(data.from).toLocaleDateString("zh-CN", { timeZone: data.timezone })
    : "";
  const dataEnd = data
    ? new Date(new Date(data.to).getTime() - 1).toLocaleDateString("zh-CN", {
        timeZone: data.timezone,
      })
    : "";
  const items = data?.items || [];
  const firstResponse = metricMean(items, "avg_first_response_seconds", "first_response_samples");
  const score = metricMean(items, "average_score", "evaluation_count");
  const replySpeed = metricMean(items, "avg_reply_seconds", "reply_samples");
  if (!manages) return <Alert type="error" title="仅租户管理员可以查看成员统计" showIcon />;
  return (
    <section className="console-screen">
      <PageTitle
        title="成员统计"
        description="从接待效率、在线时长和客户评价了解团队表现。"
        action={<Button href="/console/members">团队成员</Button>}
      />
      <div className={styles.toolbar}>
        <Space wrap>
          <DatePicker.RangePicker value={range} onChange={setRange} aria-label="成员统计日期" />
          <Button icon={<ReloadOutlined />} onClick={() => setRevision((value) => value + 1)}>
            刷新
          </Button>
        </Space>
        <span className="muted">
          {data
            ? `数据日期 ${dataStart}${dataEnd === dataStart ? "" : ` 至 ${dataEnd}`}`
            : range?.[0] && range?.[1]
              ? `查询 ${range[0].format("YYYY-MM-DD")} 至 ${range[1].format("YYYY-MM-DD")} · 尚未取得当前范围数据`
              : "查询租户今天 · 尚未取得当前范围数据"}
          {data ? ` · ${data.timezone}` : ""} · 每15秒更新
        </span>
      </div>
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      <div
        className={styles.metrics}
        style={{ gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))" }}
      >
        {[
          { label: "当前在线成员", value: items.filter((item) => item.online).length, unit: "人" },
          {
            label: "本期接待人次",
            value: items.reduce((sum, item) => sum + item.received_conversations, 0),
            unit: "人次",
          },
          { label: "平均首响", value: firstResponse, unit: "秒" },
          { label: "平均回复", value: replySpeed, unit: "秒" },
          { label: "平均评价", value: score, unit: "星" },
        ].map((item) => (
          <Card size="small" key={item.label}>
            <span className="muted">{item.label}</span>
            <div className={styles.metricValue}>
              {data && item.value !== null ? number.format(item.value) : "—"}
              <small>{item.unit}</small>
            </div>
          </Card>
        ))}
      </div>
      <div className="table-card">
        <Table<MemberMetrics>
          rowKey="id"
          dataSource={items}
          loading={loading}
          pagination={{ pageSize: 15 }}
          scroll={{ x: 1330 }}
          locale={{ emptyText: <EmptyData text="当前没有成员统计" /> }}
          columns={[
            {
              title: "成员",
              key: "member",
              width: 210,
              render: (_, item) => (
                <Space>
                  <Avatar src={item.avatar_url}>{item.display_name.slice(0, 1)}</Avatar>
                  <div>
                    <strong>{item.display_name}</strong>
                    <div className="muted" style={{ fontSize: 12 }}>
                      {item.role === "owner" ? "所有者" : item.role === "admin" ? "管理员" : "客服"}
                      {item.removed_at ? " · 已移出" : item.disabled ? " · 已停用" : ""}
                    </div>
                  </div>
                </Space>
              ),
            },
            {
              title: "在线与时长",
              key: "presence",
              width: 145,
              render: (_, item) => (
                <div>
                  <Badge
                    status={item.online ? "success" : "error"}
                    text={item.online ? "在线" : "离线"}
                  />
                  <div className="muted">{number.format(item.online_seconds / 60)} 分钟</div>
                </div>
              ),
            },
            {
              title: "当前接待",
              key: "current",
              width: 145,
              render: (_, item) => (
                <div>
                  {item.can_serve ? (
                    <>
                      <strong>
                        {item.current_load} / {item.capacity}
                      </strong>
                      <div className="muted">
                        {!item.online
                          ? "离线"
                          : !item.available
                            ? "下班"
                            : item.busy
                              ? "忙碌"
                              : "上班可接待"}
                      </div>
                    </>
                  ) : (
                    <span className="muted">仅管理</span>
                  )}
                </div>
              ),
            },
            {
              title: "接待 / 结束",
              key: "handled",
              width: 130,
              render: (_, item) => (
                <>
                  <strong>{item.received_conversations}</strong> / {item.closed_conversations}
                  <div className="muted">当前接待 {item.active_conversations}</div>
                </>
              ),
            },
            {
              title: <Tooltip title={data?.definitions.first_response}>平均首响</Tooltip>,
              key: "first",
              width: 150,
              render: (_, item) => (
                <>
                  {item.avg_first_response_seconds === null
                    ? "—"
                    : `${number.format(item.avg_first_response_seconds)} 秒`}
                  <div className="muted">{item.first_response_samples} 个回复样本</div>
                </>
              ),
            },
            {
              title: <Tooltip title={data?.definitions.reply_speed}>平均回复</Tooltip>,
              key: "reply",
              width: 150,
              render: (_, item) => (
                <>
                  {item.avg_reply_seconds === null
                    ? "—"
                    : `${number.format(item.avg_reply_seconds)} 秒`}
                  <div className="muted">{item.reply_samples} 个等待样本</div>
                </>
              ),
            },
            {
              title: "公开消息",
              dataIndex: "public_messages",
              width: 110,
              render: (value, item) => (
                <>
                  {value}
                  <div className="muted">已撤回 {item.withdrawn_messages}</div>
                </>
              ),
            },
            {
              title: "客户评价",
              key: "rating",
              width: 145,
              render: (_, item) => (
                <>
                  {item.average_score === null ? "—" : `${number.format(item.average_score)} 星`}
                  <div className="muted">{item.evaluation_count} 条评价</div>
                </>
              ),
            },
            {
              title: "满意率",
              key: "satisfaction",
              width: 110,
              render: (_, item) =>
                item.satisfaction_percent === null ? (
                  "—"
                ) : (
                  <Tag color="green">{number.format(item.satisfaction_percent)}%</Tag>
                ),
            },
          ]}
        />
      </div>
      <div className={styles.notes}>
        <p>
          在线时长从启用采集后开始记录，同一成员的多个标签页合并计算；断线后最多保留45秒在线租约。
        </p>
        <p>
          接待与首响按本期接单记录计算，首响排除排队、系统和内部消息；未回复的接待不进入平均值。结束数按结束时间及最终接待人计算。评价按本期收到的评价统计，4～5星视为满意。
        </p>
        <p>
          平均回复按本期发出的有效公开人工回复计算，每个访客等待段只取最早问题并匹配一次回复；连续补充问题不会重复计样本，接单前的等待裁剪到本人接单时间，无问题等待的主动消息不计入。
        </p>
        {data?.presence_started_at && (
          <p>
            本租户首个在线记录：
            {new Date(data.presence_started_at).toLocaleString("zh-CN", {
              timeZone: data.timezone,
            })}
          </p>
        )}
      </div>
    </section>
  );
}
