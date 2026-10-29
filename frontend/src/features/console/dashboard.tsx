/** 文件作用说明：按接待准备图稿展示真实租户指标、初始化进度和日常工作入口。 */
"use client";
import Link from "next/link";
import { useEffect, useState } from "react";
import { Alert, Button, Progress, Skeleton, Tag } from "antd";
import {
  ArrowRightOutlined,
  CheckOutlined,
  CustomerServiceOutlined,
  FlagOutlined,
  IdcardOutlined,
  LinkOutlined,
  RightOutlined,
  SkinOutlined,
  TeamOutlined,
  PartitionOutlined,
} from "@ant-design/icons";
import { useSession } from "@/features/auth/session";
import { useResource } from "@/lib/use-resource";
import type { Member, Resource } from "@/lib/types";
import { PageTitle } from "@/components/page-state";
import { api } from "@/lib/api";
import { ConnectionAlerts } from "./connection-alerts";

type DashboardSubscriptionData = {
  plan?: { name: string };
  subscription?: { expires_at?: string | null };
  usage?: { seats_used: number; seats_reserved: number; seat_limit: number };
};

/** DashboardSubscription 读取真实订阅与坐席占用，失败时不填充免费版或示例额度。 */
function DashboardSubscription({ tenantId }: { tenantId: string }) {
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
  }, [tenantId, revision]);
  const usage = data?.usage;
  const occupied = usage ? usage.seats_used + usage.seats_reserved : 0;
  const percent = usage?.seat_limit ? Math.min(100, (occupied / usage.seat_limit) * 100) : 0;
  return (
    <section className="dashboard-subscription">
      <header>
        <h2>当前套餐</h2>
        <Link href="/console/billing">
          管理套餐 <ArrowRightOutlined />
        </Link>
      </header>
      {error ? (
        <div className="dashboard-subscription-error">
          <span className="muted">套餐暂时无法读取</span>
          <Button type="link" size="small" onClick={() => setRevision((value) => value + 1)}>
            重试
          </Button>
        </div>
      ) : !data ? (
        <Skeleton active title={{ width: "40%" }} paragraph={{ rows: 1 }} />
      ) : (
        <>
          <div className="dashboard-plan-line">
            <strong>{data.plan?.name || "未开通"}</strong>
            <span>
              {data.subscription?.expires_at
                ? `有效至 ${new Date(data.subscription.expires_at).toLocaleDateString("zh-CN")}`
                : data.subscription?.expires_at === null
                  ? "长期有效"
                  : "以订阅记录为准"}
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
            <p className="muted">含 {usage.seats_reserved} 个邀请预留坐席</p>
          )}
        </>
      )}
    </section>
  );
}

/** Dashboard 以真实配置判定接待准备状态，读取失败时不将指标或进度伪装为零。 */
export function Dashboard() {
  const { current, tenantId } = useSession();
  const membership = current.memberships.find((item) => item.tenant_id === tenantId);
  const manages = ["owner", "admin"].includes(membership?.role || "");
  const members = useResource<Member>("/tenant/members");
  const links = useResource<Resource>("/tenant/access-links");
  const contacts = useResource<Resource>("/tenant/contacts");
  const conversations = useResource<Resource>("/conversations");
  const groups = useResource<Resource>(manages ? "/tenant/groups" : "");
  const pools = useResource<Resource>(manages ? "/tenant/routing-pools" : "");
  const pages = useResource<Resource>(manages ? "/tenant/pages" : "");
  const metrics = [
    {
      title: "团队成员",
      text: "客服与协作成员",
      result: members,
      icon: TeamOutlined,
      value: members.items.length,
    },
    {
      title: "接入管理",
      text: "网页、手机网页等",
      result: links,
      icon: LinkOutlined,
      value: links.items.length,
    },
    {
      title: "客户档案",
      text: "已建立的客户资料",
      result: contacts,
      icon: IdcardOutlined,
      value: contacts.items.length,
    },
    {
      title: "进行中咨询",
      text: "当前未结束的咨询",
      result: conversations,
      icon: CustomerServiceOutlined,
      value: conversations.items.filter((record) => record.status !== "closed").length,
    },
  ];
  const setup = [
    {
      title: "建立接待团队",
      text: "邀请成员，设置角色与接待资格。",
      href: "/console/members",
      icon: TeamOutlined,
      ready: members.items.some((member) => member.can_serve && !member.disabled),
      loading: members.loading,
      error: members.error,
      reload: members.reload,
    },
    {
      title: "配置分组与分配",
      text: "配置业务组和分配池，让咨询找到合适的人。",
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
      title: "创建咨询入口",
      text: "生成接入链接，分享或嵌入你的网站。",
      href: "/console/links",
      icon: LinkOutlined,
      ready: links.items.some((link) => link.status === "active"),
      loading: links.loading,
      error: links.error,
      reload: links.reload,
    },
    {
      title: "设计品牌页面",
      text: "选择模板或 AI 创作，预览后发布自己的页面。",
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
          title: "接待客户",
          text: "进入工作台，处理正在进行的咨询",
        },
        {
          href: "/console/members",
          icon: TeamOutlined,
          title: "邀请成员",
          text: "邀请管理员与客服加入团队",
        },
        {
          href: "/console/records",
          icon: IdcardOutlined,
          title: "查看聊天记录",
          text: "查找历史沟通，了解客户详情",
        },
        {
          href: "/console/templates",
          icon: SkinOutlined,
          title: "装修品牌页面",
          text: "让客户在你的品牌页面里获得服务",
        },
      ]
    : [
        {
          href: "/workspace",
          icon: CustomerServiceOutlined,
          title: "接待客户",
          text: "查看咨询，开始今天的接待",
        },
        {
          href: "/console/contacts",
          icon: IdcardOutlined,
          title: "客户资料",
          text: "整理授权范围内的客户信息",
        },
        {
          href: "/console/records",
          icon: LinkOutlined,
          title: "聊天记录",
          text: "查找接待记录与历史消息",
        },
        {
          href: "/console/automation",
          icon: CustomerServiceOutlined,
          title: "快捷话术与提醒",
          text: "管理常用回复和服务提醒",
        },
      ];
  return (
    <section className="console-screen console-dashboard">
      <PageTitle
        title={`你好，${current.user.display_name || "欢迎回来"}`}
        description={membership ? "让每一次咨询，都有回应。" : "你的账号暂未加入工作空间"}
        action={
          membership && (
            <Button href="/workspace" type="primary">
              进入客服工作台 <ArrowRightOutlined />
            </Button>
          )
        }
      />
      {!membership && (
        <Alert
          type="info"
          showIcon
          title={
            current.user.is_platform_admin
              ? "你可以进入平台管理，维护租户和公共模板。"
              : "请接受团队邀请，或联系租户管理员。"
          }
        />
      )}
      {membership && tenantId && <ConnectionAlerts key={tenantId} tenantId={tenantId} />}
      <div className="console-metrics">
        {metrics.map((item) => (
          <article className="console-metric" key={item.title}>
            <span className="console-metric-icon" aria-hidden="true">
              <item.icon />
            </span>
            <div>
              <div className="console-metric-label">{item.title}</div>
              <div className="stat-value">
                {item.result.loading ? (
                  <Skeleton.Input active size="small" />
                ) : item.result.error ? (
                  "—"
                ) : (
                  item.value
                )}
              </div>
              {!!item.result.error && (
                <Button type="link" size="small" onClick={item.result.reload}>
                  读取失败，重试
                </Button>
              )}
              {!item.result.error && <p className="console-metric-note">{item.text}</p>}
            </div>
          </article>
        ))}
      </div>
      <div className={`dashboard-panels${manages ? "" : " dashboard-agent-panels"}`}>
        {manages && (
          <section className="dashboard-setup">
            <div className="dashboard-panel-heading">
              <h2>接待准备</h2>
              <p>完成以下步骤，搭建你的客户接待服务。</p>
            </div>
            <ol className="dashboard-setup-list">
              {setup.map((step, index) => (
                <li className="dashboard-setup-step" key={step.href}>
                  <span
                    className={`dashboard-step-number${step.ready && !step.error && !step.loading ? " is-ready" : ""}`}
                    aria-label={`第 ${index + 1} 步`}
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
                      <span className="muted">读取中</span>
                    ) : step.error ? (
                      <Button size="small" type="link" onClick={step.reload}>
                        重试
                      </Button>
                    ) : (
                      <>
                        {step.ready && <Tag bordered={false}>已就绪</Tag>}
                        <Link href={step.href}>
                          {step.ready ? "查看" : "去设置"}
                          <ArrowRightOutlined />
                        </Link>
                      </>
                    )}
                  </div>
                </li>
              ))}
            </ol>
          </section>
        )}
        <div className="dashboard-side">
          <aside className="dashboard-shortcuts">
            <div className="dashboard-panel-heading">
              <h2>日常工作</h2>
              <p>从这里快速进入你常用的功能。</p>
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
          {manages && tenantId && <DashboardSubscription key={tenantId} tenantId={tenantId} />}
          <aside className="dashboard-tip">
            <span className="dashboard-tip-icon" aria-hidden="true">
              <FlagOutlined />
            </span>
            <div>
              <strong>先跑通一次完整接待</strong>
              <p>切换为可接待，再打开咨询入口测试回复。声音与通知需主动启用。</p>
            </div>
            <Button href="/help">
              操作指南 <ArrowRightOutlined />
            </Button>
          </aside>
        </div>
      </div>
    </section>
  );
}
