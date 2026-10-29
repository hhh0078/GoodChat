/** 文件作用说明：在工作空间汇总上班却失去实时探活的客服，定期刷新并可展开多个异常详情。 */
"use client";
import { useEffect, useState } from "react";
import { Alert, Button } from "antd";
import { api, errorMessage } from "@/lib/api";
import "./connection-alerts.css";

type ConnectionAlert = {
  id: string;
  display_name: string;
  busy: boolean;
  last_seen_at: string | null;
  offline_since: string | null;
};
type ConnectionAlertData = {
  items: ConnectionAlert[];
  scope: "team" | "self";
  probe_timeout_seconds: number;
  observed_at: string;
};

/** connectionTime 显示服务端真实探活时间，缺失或异常时间不会伪装为当前时刻。 */
function connectionTime(value: string | null): string {
  if (!value) return "暂无探活记录";
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "时间暂不可用";
  return date.toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

/** ConnectionAlerts 独立读取异常，不依赖团队指标成功；普通客服的接口结果仅含自身。 */
export function ConnectionAlerts({ tenantId }: { tenantId: string }) {
  const [data, setData] = useState<ConnectionAlertData | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    let running = false;
    /** refresh 合并轮询与回前台检查，防止慢请求重叠覆盖较新的状态。 */
    async function refresh() {
      if (running || document.visibilityState === "hidden") return;
      running = true;
      try {
        const result = await api<ConnectionAlertData>("/tenant/connection-alerts", {
          tenantId,
          signal: controller.signal,
        });
        if (!controller.signal.aborted) {
          setData(result);
          setError(null);
        }
      } catch (problem) {
        if (!controller.signal.aborted) setError(problem);
      } finally {
        running = false;
      }
    }
    void refresh();
    const timer = setInterval(() => void refresh(), 10000);
    /** visible 回到前台时即时检查，不等待后台节流后的下一轮定时器。 */
    const visible = () => void refresh();
    document.addEventListener("visibilitychange", visible);
    return () => {
      controller.abort();
      clearInterval(timer);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [tenantId, revision]);

  if (error)
    return (
      <Alert
        className="dashboard-connection-alert"
        type="error"
        showIcon
        title="客服实时连接检查失败"
        description={errorMessage(error)}
        action={
          <Button size="small" onClick={() => setRevision((value) => value + 1)}>
            重新检查
          </Button>
        }
      />
    );
  if (!data?.items.length) return null;
  return (
    <Alert
      className="dashboard-connection-alert"
      type="error"
      showIcon
      title={
        data.scope === "team"
          ? `${data.items.length} 位客服处于上班状态，但实时连接离线`
          : "你处于上班状态，但实时连接离线"
      }
      description={
        <div>
          <p className="connection-alert-explanation">
            尚未建立实时连接，或最近 {data.probe_timeout_seconds}{" "}
            秒内未完成探活。请检查客服页面及网络。
          </p>
          <details className="connection-alert-details">
            <summary>查看异常详情（{data.items.length}）</summary>
            <ul>
              {data.items.map((item) => (
                <li key={item.id}>
                  <div className="connection-alert-member">
                    <strong>{item.display_name || "未命名客服"}</strong>
                    <span>{item.busy ? "上班 · 忙碌" : "上班 · 可接待"}</span>
                  </div>
                  <span className="connection-alert-time">
                    最后探活：{connectionTime(item.last_seen_at)}
                    {item.offline_since && ` · 离线起始：${connectionTime(item.offline_since)}`}
                  </span>
                </li>
              ))}
            </ul>
          </details>
        </div>
      }
      action={
        <Button size="small" href={data.scope === "team" ? "/console/members" : "/workspace"}>
          {data.scope === "team" ? "团队成员" : "检查连接"}
        </Button>
      }
    />
  );
}
