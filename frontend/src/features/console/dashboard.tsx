/** 文件作用说明：按接待准备图稿展示真实租户指标、初始化进度和日常工作入口。 */
"use client";
import { ErrorNotice, PageTitle } from "@/components/page-state";
import { useSession } from "@/features/auth/session";
import Link from "@/i18n/link";
import { useFormats } from "@/i18n/use-formats";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import type { Member, Resource } from "@/lib/types";
import { useResource } from "@/lib/use-resource";
import { serialPoll } from "@/lib/reporting-state";
import "./dashboard.css";
import {
  ArrowRightOutlined,
  CheckOutlined,
  CustomerServiceOutlined,
  FlagOutlined,
  IdcardOutlined,
  LinkOutlined,
  PartitionOutlined,
  RightOutlined,
  SkinOutlined,
  TeamOutlined,
} from "@ant-design/icons";
import { Alert, Button, Progress, Skeleton, Tag } from "antd";
import { useEffect, useState } from "react";
import { ConnectionAlerts } from "./connection-alerts";
type DashboardSubscriptionData = {
  plan?: {
    name: string;
  };
  subscription?: {
    expires_at?: string | null;
  };
  usage?: {
    seats_used: number;
    seats_reserved: number;
    seat_limit: number;
  };
};
/** DashboardSubscription 读取真实订阅与坐席占用，失败时不填充免费版或示例额度。 */
function DashboardSubscription({ tenantId }: { tenantId: string }) {
  const { currentPreferences, numberFormatter } = useFormats();
  const locale = currentPreferences().locale;

  const tr = useTranslator();
  const [data, setData] = useState<DashboardSubscriptionData | null>(null);
  const [error, setError] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setData(null);
    setError(false);
    api<DashboardSubscriptionData>("/tenant/subscription", { tenantId, signal: controller.signal })
      .then((result) => {
        if (!controller.signal.aborted) setData(result);
      })
      .catch(() => {
        if (!controller.signal.aborted) setError(true);
      });
    return () => controller.abort();
  }, [tenantId, revision, locale]);
  const usage = data?.usage;
  const occupied = usage ? usage.seats_used + usage.seats_reserved : 0;
  const percent = usage?.seat_limit ? Math.min(100, (occupied / usage.seat_limit) * 100) : 0;
  return (
    <section className="dashboard-subscription">
      <header>
        <h2>{tr("当前套餐")}</h2>
        <Link href="/console/billing">
          {tr("管理套餐 ")}
          <ArrowRightOutlined />
        </Link>
      </header>
      {error ? (
        <div className="dashboard-subscription-error">
          <span className="muted">{tr("套餐暂时无法读取")}</span>
          <Button type="link" size="small" onClick={() => setRevision((value) => value + 1)}>
            {tr("重试 ")}
          </Button>
        </div>
      ) : !data ? (
        <Skeleton active title={{ width: "40%" }} paragraph={{ rows: 1 }} />
      ) : (
        <>
          <div className="dashboard-plan-line">
            <strong>{data.plan?.name || tr("未开通")}</strong>
            <span>
              {data.subscription?.expires_at
                ? tr("有效至 {p0}", {
                    p0: String(
                      new Date(data.subscription.expires_at).toLocaleDateString(
                        currentPreferences().locale,
                        { timeZone: currentPreferences().timezone },
                      ),
                    ),
                  })
                : data.subscription?.expires_at === null
                  ? tr("长期有效")
                  : tr("以订阅记录为准")}
            </span>
          </div>
          <div className="dashboard-seat-usage">
            <Progress
              percent={percent}
              showInfo={false}
              strokeColor="var(--brand)"
              trailColor="var(--brand-soft)"
            />
            <span>{usage ? `${occupied} / ${usage.seat_limit}` : "—"}</span>
          </div>
          {!!usage?.seats_reserved && (
            <p className="muted">
              {tr("含 ")}
              {usage.seats_reserved}
              {tr(" 个邀请预留坐席")}
            </p>
          )}
        </>
      )}
    </section>
  );
}
/** Dashboard 以真实配置判定接待准备状态，读取失败时不将指标或进度伪装为零。 */
export function Dashboard() {
  const tr = useTranslator();
  const { current, tenantId } = useSession();
  const membership = current.memberships.find((item) => item.tenant_id === tenantId);
  const manages = ["owner", "admin"].includes(membership?.role || "");
  const members = useResource<Member>(manages ? "/tenant/members" : "");
  const links = useResource<Resource>(manages ? "/tenant/access-links" : "");
  const groups = useResource<Resource>(manages ? "/tenant/groups" : "");
  const pools = useResource<Resource>(manages ? "/tenant/routing-pools" : "");
  const pages = useResource<Resource>(manages ? "/tenant/pages" : "");
  const [overview, setOverview] = useState<{
    scope: string;
    data: Record<string, number | string>;
  } | null>(null);
  const [overviewError, setOverviewError] = useState<unknown>(null);
  const [overviewRevision, setOverviewRevision] = useState(0);
  const overviewScope = JSON.stringify([tenantId, membership?.id, manages]);
  useEffect(() => {
    const controller = new AbortController();
    setOverview(null);
    setOverviewError(null);
    if (!membership) return () => controller.abort();
    const load = serialPoll(controller.signal, async () => {
      try {
        const data = await api<Record<string, number | string>>("/tenant/dashboard", {
          tenantId,
          signal: controller.signal,
        });
        if (!controller.signal.aborted) {
          setOverview({ scope: overviewScope, data });
          setOverviewError(null);
        }
      } catch (error) {
        if (!controller.signal.aborted) setOverviewError(error);
      }
    });
    void load();
    const timer = setInterval(() => void load(), 30000);
    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [tenantId, overviewScope, overviewRevision, membership?.id]);
  const report = overview?.scope === overviewScope ? overview.data : null;
  const metrics = manages
    ? [
        {
          title: tr("排队中"),
          key: "queued",
          icon: CustomerServiceOutlined,
          text: tr("等待分配的咨询"),
        },
        {
          title: tr("接待中"),
          key: "active",
          icon: CustomerServiceOutlined,
          text: tr("团队当前接待"),
        },
        {
          title: tr("今日咨询"),
          key: "today_created",
          icon: IdcardOutlined,
          text: tr("今天新建的咨询"),
        },
        {
          title: tr("在线客服"),
          key: "online_agents",
          icon: TeamOutlined,
          text: tr("实时探活有效的客服"),
        },
      ]
    : [
        {
          title: tr("待接单"),
          key: "assigned",
          icon: CustomerServiceOutlined,
          text: tr("已分配给我的咨询"),
        },
        {
          title: tr("接待中"),
          key: "active",
          icon: CustomerServiceOutlined,
          text: tr("我正在接待的咨询"),
        },
        {
          title: tr("待处理消息"),
          key: "unread",
          icon: IdcardOutlined,
          text: tr("含未读访客消息的咨询"),
        },
        {
          title: tr("今日接待"),
          key: "today_received",
          icon: CustomerServiceOutlined,
          text: tr("按本人接待段去重统计"),
        },
      ];
  const setup = [
    {
      title: tr("建立接待团队"),
      text: tr("邀请成员，设置角色与接待资格。"),
      href: "/console/members",
      icon: TeamOutlined,
      ready: members.items.some((member) => member.can_serve && !member.disabled),
      loading: members.loading,
      error: members.error,
      reload: members.reload,
    },
    {
      title: tr("配置分组与分配"),
      text: tr("配置业务组和分配池，让咨询找到合适的人。"),
      href: "/console/routing",
      icon: PartitionOutlined,
      ready: groups.items.length > 0 && pools.items.length > 0,
      loading: groups.loading || pools.loading,
      error: groups.error || pools.error,
      reload: () => {
        groups.reload();
        pools.reload();
      },
    },
    {
      title: tr("创建咨询入口"),
      text: tr("生成接入链接，分享或嵌入你的网站。"),
      href: "/console/links",
      icon: LinkOutlined,
      ready: links.items.some((link) => link.status === "active"),
      loading: links.loading,
      error: links.error,
      reload: links.reload,
    },
    {
      title: tr("设计品牌页面"),
      text: tr("选择模板或 AI 创作，预览后发布自己的页面。"),
      href: "/console/templates",
      icon: SkinOutlined,
      ready: pages.items.some((page) => !!page.current_release_id),
      loading: pages.loading,
      error: pages.error,
      reload: pages.reload,
    },
  ];
  const dailyWork = manages
    ? [
        {
          href: "/workspace",
          icon: CustomerServiceOutlined,
          title: tr("接待客户"),
          text: tr("进入工作台，处理正在进行的咨询"),
        },
        {
          href: "/console/member-statistics",
          icon: TeamOutlined,
          title: tr("成员统计"),
          text: tr("查看团队在线与接待效率"),
        },
        {
          href: "/console/reports",
          icon: IdcardOutlined,
          title: tr("服务统计"),
          text: tr("查看今日咨询与服务表现"),
        },
        {
          href: "/console/records",
          icon: LinkOutlined,
          title: tr("查看聊天记录"),
          text: tr("查找历史沟通，了解客户详情"),
        },
      ]
    : [
        {
          href: "/workspace",
          icon: CustomerServiceOutlined,
          title: tr("接待客户"),
          text: tr("查看咨询，开始今天的接待"),
        },
        {
          href: "/console/contacts",
          icon: IdcardOutlined,
          title: tr("客户资料"),
          text: tr("整理授权范围内的客户信息"),
        },
        {
          href: "/console/records",
          icon: LinkOutlined,
          title: tr("聊天记录"),
          text: tr("查找接待记录与历史消息"),
        },
        {
          href: "/console/automation",
          icon: CustomerServiceOutlined,
          title: tr("快捷话术与提醒"),
          text: tr("管理常用回复和服务提醒"),
        },
      ];
  return (
    <section className="console-screen console-dashboard">
      <PageTitle
        title={tr("你好，{p0}", { p0: String(current.user.display_name || tr("欢迎回来")) })}
        description={tr("让每一次咨询，都有回应。")}
      />
      {!membership && (
        <Alert
          type="info"
          showIcon
          title={
            current.user.is_platform_admin
              ? tr("你可以进入平台管理，维护租户和公共模板。")
              : tr("请接受团队邀请，或联系租户管理员。")
          }
        />
      )}
      {membership && tenantId && <ConnectionAlerts key={tenantId} tenantId={tenantId} />}
      <div className="dashboard-overview-heading">
        <div>
          <h2>{manages ? tr("今日团队概况") : tr("我的今日工作")}</h2>
          <p>
            {report?.timezone || "—"} · {tr("每30秒更新")}
          </p>
        </div>
        <div className="dashboard-overview-actions">
          <Button onClick={() => setOverviewRevision((value) => value + 1)}>{tr("刷新")}</Button>
          <Button href="/workspace" type="primary">
            {tr("进入客服工作台 ")}
            <ArrowRightOutlined />
          </Button>
        </div>
      </div>
      <ErrorNotice error={overviewError} retry={() => setOverviewRevision((value) => value + 1)} />
      <div className="console-metrics">
        {metrics.map((item) => (
          <article className="console-metric" key={item.key}>
            <span className="console-metric-icon" aria-hidden="true">
              <item.icon />
            </span>
            <div>
              <div className="console-metric-label">{item.title}</div>
              <div className="stat-value">
                {overviewError ? (
                  "—"
                ) : !report ? (
                  <Skeleton.Input active size="small" />
                ) : (
                  (report[item.key] ?? "—")
                )}
              </div>
              <p className="console-metric-note">{item.text}</p>
            </div>
          </article>
        ))}
      </div>
      <div className="dashboard-shift-summary">
        <span>
          {tr("今日结束")} <strong>{!overviewError ? (report?.today_closed ?? "—") : "—"}</strong>
        </span>
        {manages && (
          <span>
            {tr("待接单")} <strong>{!overviewError ? (report?.assigned ?? "—") : "—"}</strong>
          </span>
        )}
        <span className="muted">{tr("日期按租户时区统计")}</span>
      </div>
      <div className={`dashboard-panels${manages ? "" : " dashboard-agent-panels"}`}>
        {manages && (
          <section className="dashboard-setup">
            <div className="dashboard-panel-heading">
              <h2>{tr("接待准备")}</h2>
              <p>{tr("完成以下步骤，搭建你的客户接待服务。")}</p>
            </div>
            <ol className="dashboard-setup-list">
              {setup.map((step, index) => (
                <li className="dashboard-setup-step" key={step.href}>
                  <span
                    className={`dashboard-step-number${step.ready && !step.error && !step.loading ? " is-ready" : ""}`}
                    aria-label={tr("第 {p0} 步", { p0: String(index + 1) })}
                  >
                    {step.ready && !step.error && !step.loading ? <CheckOutlined /> : index + 1}
                  </span>
                  <step.icon className="dashboard-step-icon" aria-hidden="true" />
                  <div className="dashboard-step-copy">
                    <strong>{step.title}</strong>
                    <p>{step.text}</p>
                  </div>
                  <div className="dashboard-step-action">
                    {step.loading ? (
                      <span className="muted">{tr("读取中")}</span>
                    ) : step.error ? (
                      <Button size="small" type="link" onClick={step.reload}>
                        {tr("重试 ")}
                      </Button>
                    ) : (
                      <>
                        {step.ready && <Tag bordered={false}>{tr("已就绪")}</Tag>}
                        <Link href={step.href}>
                          {step.ready ? tr("查看") : tr("去设置")}
                          <ArrowRightOutlined />
                        </Link>
                      </>
                    )}
                  </div>
                </li>
              ))}
            </ol>
            {tenantId && <DashboardSubscription key={tenantId} tenantId={tenantId} />}
          </section>
        )}
        <div className="dashboard-side">
          <aside className="dashboard-shortcuts">
            <div className="dashboard-panel-heading">
              <h2>{tr("日常工作")}</h2>
              <p>{tr("从这里快速进入你常用的功能。")}</p>
            </div>
            <div className="dashboard-quick-list">
              {dailyWork.map((item) => (
                <Link className="dashboard-quick-link" href={item.href} key={item.href}>
                  <span className="dashboard-quick-icon" aria-hidden="true">
                    <item.icon />
                  </span>
                  <div>
                    <strong>{item.title}</strong>
                    <span>{item.text}</span>
                  </div>
                  <RightOutlined className="dashboard-quick-arrow" aria-hidden="true" />
                </Link>
              ))}
            </div>
          </aside>
          <aside className="dashboard-tip">
            <span className="dashboard-tip-icon" aria-hidden="true">
              <FlagOutlined />
            </span>
            <div>
              <strong>{tr("先跑通一次完整接待")}</strong>
              <p>{tr("切换为可接待，再打开咨询入口测试回复。声音与通知需主动启用。")}</p>
            </div>
            <Button href="/help">
              {tr("操作指南 ")}
              <ArrowRightOutlined />
            </Button>
          </aside>
        </div>
      </div>
    </section>
  );
}
