/** 文件作用说明：以真实平台规模和可操作的管理入口展示平台概览，不补充模拟趋势。 */
"use client";
import { ErrorNotice } from "@/components/page-state";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import {
  ArrowRightOutlined,
  BankOutlined,
  CreditCardOutlined,
  FileDoneOutlined,
  SkinOutlined,
  TeamOutlined,
} from "@ant-design/icons";
import { Button, Skeleton } from "antd";
import { useEffect, useState } from "react";
/** PlatformOverview 展示授权接口返回的真实计数，并通过父级标签切换进入管理任务。 */
export function PlatformOverview({ onNavigate }: { onNavigate?: (key: string) => void }) {
  const tr = useTranslator();
  const [data, setData] = useState<{
    tenants: number;
    users: number;
  } | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    api<{
      tenants: number;
      users: number;
    }>("/platform/overview", { signal: controller.signal })
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
        <h2>{tr("平台概览")}</h2>
        <p>{tr("查看平台规模，处理日常运营事务。")}</p>
      </div>
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      <div className="studio-platform-metrics">
        {[
          {
            label: tr("注册租户"),
            value: data?.tenants,
            icon: BankOutlined,
            text: tr("平台中已建立的租户"),
          },
          {
            label: tr("账号总数"),
            value: data?.users,
            icon: TeamOutlined,
            text: tr("平台中已注册的账号"),
          },
        ].map((metric) => (
          <article className="studio-platform-metric" key={metric.label}>
            <span className="studio-platform-metric-icon" aria-hidden="true">
              <metric.icon />
            </span>
            <div>
              <div className="muted">{metric.label}</div>
              <div className="stat-value">
                {!data && !error ? (
                  <Skeleton.Input active size="small" />
                ) : error ? (
                  "—"
                ) : (
                  metric.value
                )}
              </div>
              <p>{metric.text}</p>
            </div>
          </article>
        ))}
      </div>
      <h3 className="studio-overview-title">{tr("运营工作")}</h3>
      <div className="studio-overview-actions">
        {[
          {
            key: "tenants",
            icon: BankOutlined,
            title: tr("租户运营"),
            text: tr("管理租户状态、套餐与开通邀请"),
          },
          {
            key: "templates",
            icon: SkinOutlined,
            title: tr("公共装修模板"),
            text: tr("维护可供租户选用的品牌页面"),
          },
          {
            key: "plans",
            icon: CreditCardOutlined,
            title: tr("套餐与定价"),
            text: tr("配置版本、坐席、功能和有效期"),
          },
          {
            key: "upgrades",
            icon: FileDoneOutlined,
            title: tr("升级申请"),
            text: tr("核对申请并处理人工开通"),
          },
        ].map((item) => (
          <Button
            className="studio-overview-action"
            key={item.key}
            onClick={() => onNavigate?.(item.key)}
            disabled={!onNavigate}
          >
            <span className="studio-overview-action-icon" aria-hidden="true">
              <item.icon />
            </span>
            <span>
              <strong>{item.title}</strong>
              <small>{item.text}</small>
            </span>
            <ArrowRightOutlined aria-hidden="true" />
          </Button>
        ))}
      </div>
      <p className="studio-overview-note">
        {tr("以上数据来自平台实际记录。客户档案与聊天内容在各自租户的授权范围内访问。 ")}
      </p>
    </section>
  );
}
