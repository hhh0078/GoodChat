/** 文件作用说明：按接待准备图稿展示真实租户指标、初始化进度和日常工作入口。 */
"use client";
import Link from "next/link";
import { Alert, Button, Skeleton, Tag } from "antd";
import {
  ArrowRightOutlined,
  BarChartOutlined,
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
    { title: "团队成员", result: members, icon: TeamOutlined, value: members.items.length },
    { title: "接入链接", result: links, icon: LinkOutlined, value: links.items.length },
    { title: "客户档案", result: contacts, icon: IdcardOutlined, value: contacts.items.length },
    {
      title: "进行中咨询",
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
      title: "配置接待范围",
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
          href: "/console/members",
          icon: TeamOutlined,
          title: "管理团队成员",
          text: "添加成员、设置角色与权限",
        },
        {
          href: "/console/links",
          icon: LinkOutlined,
          title: "获取接入链接",
          text: "为不同业务配置咨询入口",
        },
        {
          href: "/console/routing",
          icon: PartitionOutlined,
          title: "分组与分配",
          text: "安排接待范围与分配规则",
        },
        {
          href: "/console/reports",
          icon: BarChartOutlined,
          title: "查看服务统计",
          text: "了解咨询情况与团队工作数据",
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
        description={membership ? "把每一次咨询，接稳、接好。" : "你的账号暂未加入工作空间"}
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
      </div>
      <aside className="dashboard-tip">
        <span className="dashboard-tip-icon" aria-hidden="true">
          <FlagOutlined />
        </span>
        <div>
          <strong>先跑通一次完整接待</strong>
          <p>
            在工作台切换为可接待，再用独立浏览器窗口打开入口，测试从咨询到回复的完整流程。声音和网页通知需主动启用。
          </p>
        </div>
        <Button href="/help">
          查看操作指南 <ArrowRightOutlined />
        </Button>
      </aside>
    </section>
  );
}
