/** 文件作用说明：向总管理员显示应用资源、去重实时人数与接待余量，未知承载人数明确等待压测校准。 */
"use client";
import { useEffect, useState } from "react";
import { Alert, Badge, Button, Card, Progress, Space, Tag } from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import { api, ApiError } from "@/lib/api";
import { serialPoll } from "@/lib/reporting-state";
import { useSession } from "@/features/auth/session";
import { ErrorNotice, PageTitle } from "@/components/page-state";
import { smoothTrendPath } from "@/lib/trend-curve";
import styles from "@/features/console/operations-statistics.module.css";
import { CapacityBaseline, type CapacityState } from "./capacity-baseline";

type ResourceSample = {
  at: string;
  scope: string;
  scope_detail?: string;
  cgroup_path?: string;
  cpu_percent: number | null;
  cpu_cores: number;
  memory_used_bytes: number | null;
  memory_limit_bytes: number | null;
  memory_percent: number | null;
  visible_memory_bytes: number | null;
  go_heap_bytes: number;
  goroutines: number;
  cpu_throttled_periods: number | null;
};
type Monitor = {
  sampled_at: string;
  resources: ResourceSample;
  history: ResourceSample[];
  uptime_seconds: number;
  realtime: Record<string, number>;
  database: {
    healthy: boolean;
    latency_ms: number;
    acquired: number;
    idle: number;
    total: number;
    maximum: number;
  };
  reception: {
    available: boolean;
    available_agents?: number;
    total_capacity?: number;
    current_load?: number;
    remaining?: number;
  };
  capacity: CapacityState;
  scope_note: string;
};
const metricNumber = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 1 });

/** bytesText 按真实字节换算可读大小，未知资源与零用量明确区分。 */
function bytesText(value: number | null | undefined) {
  if (value === null || value === undefined || !Number.isFinite(value)) return "—";
  if (value >= 1024 ** 3) return `${metricNumber.format(value / 1024 ** 3)} GiB`;
  if (value >= 1024 ** 2) return `${metricNumber.format(value / 1024 ** 2)} MiB`;
  return `${metricNumber.format(value / 1024)} KiB`;
}

/** ResourceCurve 用现有平滑路径连接真实采样，首次未知CPU样本不伪装成零负载。 */
function ResourceCurve({
  items,
  metric,
  label,
}: {
  items: ResourceSample[];
  metric: "cpu_percent" | "memory_percent";
  label: string;
}) {
  const values = items
    .map((item) => item[metric])
    .filter((value): value is number => typeof value === "number" && Number.isFinite(value));
  if (values.length < 2)
    return (
      <p className="muted" style={{ minHeight: 130, paddingTop: 40 }}>
        正在积累{label}采样…
      </p>
    );
  return (
    <svg
      className={styles.resourceChart}
      viewBox="0 0 600 130"
      role="img"
      aria-label={`最近两分钟${label}真实采样平滑曲线`}
      preserveAspectRatio="none"
    >
      <line x1={0} x2={600} y1={129} y2={129} />
      <line x1={0} x2={600} y1={65} y2={65} />
      <path d={smoothTrendPath(values, 600, 130, 100)} />
    </svg>
  );
}

/** ServerMonitorPage 每五秒刷新平台监控，采样失败保留上次读数并显示错误与时间。 */
export function ServerMonitorPage() {
  const { current } = useSession();
  const allowed = current.user.is_platform_admin;
  const [data, setData] = useState<Monitor | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    if (!allowed) return;
    const controller = new AbortController();
    /** loadMonitoring 只读取平台受保护接口，不执行系统命令或在浏览器估算服务器资源。 */
    const loadMonitoring = serialPoll(controller.signal, async () => {
      setLoading(true);
      try {
        const result = await api<Monitor>("/platform/monitoring", { signal: controller.signal });
        if (!controller.signal.aborted) {
          setData(result);
          setError(null);
        }
      } catch (problem) {
        if (!controller.signal.aborted) {
          setError(problem);
          if (problem instanceof ApiError && [401, 403].includes(problem.status)) setData(null);
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    });
    void loadMonitoring();
    const timer = setInterval(() => void loadMonitoring(), 5000);
    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [allowed, revision]);
  if (!allowed) return <Alert type="error" showIcon title="仅总管理员可以查看服务器监控" />;
  const resources = data?.resources;
  const cgroup = resources?.scope === "application_cgroup";
  return (
    <section className="console-screen" aria-label="服务器监控">
      <PageTitle
        title="服务器监控"
        description="观察当前运行环境的资源与实时服务人数，识别接待余量和资源压力。"
        action={
          <Space wrap>
            <Button href="/platform">平台管理</Button>
            <Button
              icon={<ReloadOutlined />}
              loading={loading}
              onClick={() => setRevision((value) => value + 1)}
            >
              刷新
            </Button>
          </Space>
        }
      />
      <div className={styles.toolbar}>
        <span>
          <Tag color="blue">
            {resources ? (cgroup ? "当前进程控制组" : "Go进程") : "正在读取监控范围"}
          </Tag>
          <span className="muted">每5秒刷新 · 每秒采样</span>
        </span>
        <span className="muted">
          {data
            ? `最近采样 ${new Date(data.sampled_at).toLocaleTimeString("zh-CN")}`
            : "正在连接监控…"}
        </span>
      </div>
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      <div className={styles.metrics}>
        {[
          { label: "当前实时在线", key: "online_people", unit: "人" },
          { label: "在线访客", key: "online_visitors", unit: "人" },
          { label: "在线成员账号", key: "online_agents", unit: "人" },
          { label: "实时连接", key: "connections", unit: "条" },
        ].map((item) => (
          <Card size="small" key={item.key}>
            <span className="muted">{item.label}</span>
            <div className={styles.metricValue}>
              {data ? (data.realtime[item.key] ?? "—") : "—"}
              <small>{item.unit}</small>
            </div>
          </Card>
        ))}
      </div>
      <div className={styles.resourceGrid}>
        <Card>
          <div className={styles.resourceHeader}>
            <h2>CPU使用率</h2>
            <span className="muted">
              可用CPU {resources ? metricNumber.format(resources.cpu_cores) : "—"} 核
            </span>
          </div>
          <div className={styles.resourceReading}>
            <strong>
              {resources?.cpu_percent === null || !resources
                ? "—"
                : metricNumber.format(resources.cpu_percent)}
            </strong>
            <span className="muted">%</span>
          </div>
          <ResourceCurve items={data?.history || []} metric="cpu_percent" label="CPU" />
          <div className={styles.resourceDetails}>
            <span>{cgroup ? "按控制组CPU用量与配额计算" : "仅当前Go进程，不含其他服务进程"}</span>
            {resources?.cpu_throttled_periods !== null &&
              resources?.cpu_throttled_periods !== undefined && (
                <span>累计限流 {resources.cpu_throttled_periods} 次</span>
              )}
          </div>
        </Card>
        <Card>
          <div className={styles.resourceHeader}>
            <h2>内存用量</h2>
            <span className="muted">
              {resources?.memory_limit_bytes
                ? `上限 ${bytesText(resources.memory_limit_bytes)}`
                : "未取得独立内存上限"}
            </span>
          </div>
          <div className={styles.resourceReading}>
            <strong>{bytesText(resources?.memory_used_bytes)}</strong>
            {resources?.memory_percent !== null && resources?.memory_percent !== undefined && (
              <span className="muted">{metricNumber.format(resources.memory_percent)}%</span>
            )}
          </div>
          {resources?.memory_percent !== null && resources?.memory_percent !== undefined ? (
            <ResourceCurve items={data?.history || []} metric="memory_percent" label="内存" />
          ) : (
            <div style={{ minHeight: 130, paddingTop: 30 }}>
              <p className="muted">
                {cgroup ? "没有独立内存上限读数，暂不计算配额使用率。" : "当前监控范围为Go进程。"}
              </p>
              <p className="muted">环境可见总内存 {bytesText(resources?.visible_memory_bytes)}</p>
            </div>
          )}
          <div className={styles.resourceDetails}>
            <span>Go堆 {bytesText(resources?.go_heap_bytes)}</span>
            <span>Go协程 {resources?.goroutines ?? "—"}</span>
          </div>
        </Card>
      </div>
      <div className={styles.monitorFoot}>
        <Card>
          <h2>服务接待余量</h2>
          <div className={styles.resourceReading}>
            <strong>{data?.reception.available ? data.reception.remaining : "—"}</strong>
            <span className="muted">个当前可接待名额</span>
          </div>
          <p className="muted">在线、上班且非忙碌成员的剩余容量，属于业务接待量。</p>
          {data?.reception.available && (
            <Progress
              percent={
                data.reception.total_capacity
                  ? Math.min(
                      100,
                      ((data.reception.current_load || 0) / data.reception.total_capacity) * 100,
                    )
                  : 0
              }
              showInfo={false}
              strokeColor="var(--brand)"
            />
          )}
          <div className={styles.resourceDetails}>
            <span>可接待成员 {data?.reception.available_agents ?? "—"}</span>
            <span>
              已占用 {data?.reception.current_load ?? "—"} / {data?.reception.total_capacity ?? "—"}
            </span>
          </div>
          <hr style={{ border: 0, borderTop: "1px solid var(--line)", margin: "18px 0" }} />
          <CapacityBaseline
            value={data?.capacity}
            onChanged={() => setRevision((value) => value + 1)}
          />
        </Card>
        <Card>
          <h2>数据库与运行状态</h2>
          <div className={styles.health}>
            <Badge
              status={data?.database.healthy ? "success" : "error"}
              text={
                data
                  ? data.database.healthy
                    ? "数据库连接正常"
                    : "数据库连接异常"
                  : "读取连接状态中"
              }
            />
            <span className="muted">
              探测 {data ? metricNumber.format(data.database.latency_ms) : "—"} ms
            </span>
          </div>
          <div className={styles.resourceDetails}>
            <span>使用中 {data?.database.acquired ?? "—"}</span>
            <span>空闲 {data?.database.idle ?? "—"}</span>
            <span>连接池上限 {data?.database.maximum ?? "—"}</span>
          </div>
          <p className="muted">
            应用已运行 {data ? metricNumber.format(data.uptime_seconds / 3600) : "—"} 小时
          </p>
          <p className="muted">当前有聊天连接的租户 {data?.realtime.online_tenants ?? "—"} 个</p>
        </Card>
      </div>
      <div className={styles.notes}>
        {resources?.scope_detail && <p>{resources.scope_detail}</p>}
        <p>{data?.scope_note || "监控仅展示实际能够采集的运行环境指标。"}</p>
        <p>
          同一成员多个标签页去重为一个账号；同一访客多个连接去重为一个访客。资源曲线为最近两分钟采样，服务器或应用重启后重新积累。
        </p>
        <p>承载人数受消息频率、媒体、数据库和网络影响。接待名额不能代替服务器并发压测结果。</p>
      </div>
    </section>
  );
}
