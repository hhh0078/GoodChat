/** 文件作用说明：读取平台真实租户与账号概览，无数据时不显示虚构规模指标。 */
"use client";
import { useEffect, useState } from "react";
import { Spin } from "antd";
import { api } from "@/lib/api";
import { ErrorNotice } from "@/components/page-state";
/** PlatformOverview 展示平台授权接口返回的规模计数。 */
export function PlatformOverview() {
  const [data, setData] = useState<{ tenants: number; users: number } | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    api<{ tenants: number; users: number }>("/platform/overview", { signal: controller.signal })
      .then((value) => {
        if (!controller.signal.aborted) {
          setData(value);
          setError(null);
        }
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      });
    return () => controller.abort();
  }, [revision]);
  return (
    <section className="studio-platform-section">
      <div className="studio-panel-heading">
        <h2>平台概览</h2>
        <p>平台规模按实际记录统计。</p>
      </div>
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      {!data && !error ? (
        <Spin />
      ) : (
        data && (
          <div className="studio-platform-metrics">
            <div className="studio-platform-metric">
              <div className="muted">注册租户</div>
              <div className="stat-value">{data.tenants}</div>
            </div>
            <div className="studio-platform-metric">
              <div className="muted">账号总数</div>
              <div className="stat-value">{data.users}</div>
            </div>
          </div>
        )
      )}
      <p className="muted">
        平台规模按数据库实际记录统计。租户业务消息仍在对应租户授权范围内访问。
      </p>
    </section>
  );
}
