/** 文件作用说明：展示当前租户的真实资源概览和初始化入口，不填充演示业务指标。 */
"use client";
import Link from "next/link";
import { Alert, Button, Card, Space, Steps, Spin } from "antd";
import {
  CustomerServiceOutlined,
  LinkOutlined,
  SkinOutlined,
  TeamOutlined,
} from "@ant-design/icons";
import { useSession } from "@/features/auth/session";
import { useResource } from "@/lib/use-resource";
import type { Resource } from "@/lib/types";
import { PageTitle } from "@/components/page-state";

/** Dashboard 展示实际查询到的资源数量和可执行配置步骤。 */
export function Dashboard() {
  const { current, tenantId } = useSession();
  const membership = current.memberships.find((item) => item.tenant_id === tenantId);
  const members = useResource<Resource>("/tenant/members");
  const links = useResource<Resource>("/tenant/access-links");
  const contacts = useResource<Resource>("/tenant/contacts");
  const conversations = useResource<Resource>("/conversations");
  const manages = ["owner", "admin"].includes(membership?.role || "");
  const metrics = [
    { title: "团队成员", result: members },
    { title: "接入链接", result: links },
    { title: "客户档案", result: contacts },
    { title: "进行中咨询", result: conversations },
  ];
  return (
    <>
      <PageTitle
        title={`你好，${current.user.display_name || "欢迎回来"}`}
        description={
          membership
            ? `${membership.tenant_name} · 从这里开始今天的接待`
            : "你的账号暂未加入工作空间"
        }
        action={
          <Button href="/workspace" type="primary" icon={<CustomerServiceOutlined />}>
            进入客服工作台
          </Button>
        }
      />
      {!membership && (
        <Alert
          type="info"
          showIcon
          title={
            current.user.is_platform_admin
              ? "你可以进入平台管理，维护租户和公共模板。"
              : "请接受团队邀请，或者联系租户管理员检查成员状态。"
          }
        />
      )}
      <div className="stat-grid">
        {metrics.map((item) => (
          <Card key={item.title} size="small">
            <div className="muted">{item.title}</div>
            <div className="stat-value">
              {item.result.loading ? (
                <Spin size="small" />
              ) : item.result.error ? (
                "—"
              ) : (
                item.result.items.filter(
                  (record) => item.title !== "进行中咨询" || record.status !== "closed",
                ).length
              )}
            </div>
            {!!item.result.error && <small className="muted">该指标暂不可用</small>}
          </Card>
        ))}
      </div>
      <div className="dashboard-grid">
        <Card title="从一个聊天链接开始">
          <Steps
            orientation="vertical"
            items={[
              {
                title: "建立团队",
                description: "邀请管理员与客服，设置成员的接待资格。",
                status: members.items.length > 1 ? "finish" : "process",
              },
              { title: "设置接待范围", description: "创建客服业务组和分配池，安排接待规则。" },
              {
                title: "创建接入链接",
                description: "分享链接或二维码，在另一个浏览器发起测试咨询。",
                status: links.items.length ? "finish" : "wait",
              },
              { title: "设计品牌页面", description: "选用模板、手动编辑或通过 AI 创建私有设计。" },
            ]}
          />
        </Card>
        <Space orientation="vertical" style={{ width: "100%" }} size={20}>
          <Card title="常用入口">
            <div className="quick-grid">
              {[
                {
                  href: "/workspace",
                  icon: CustomerServiceOutlined,
                  title: "接待客户",
                  text: "打开客服工作台",
                },
                ...(manages
                  ? [
                      {
                        href: "/console/links",
                        icon: LinkOutlined,
                        title: "接入链接",
                        text: "创建与分享入口",
                      },
                      {
                        href: "/console/members",
                        icon: TeamOutlined,
                        title: "邀请成员",
                        text: "管理你的接待团队",
                      },
                      {
                        href: "/console/templates",
                        icon: SkinOutlined,
                        title: "装修模板",
                        text: "设计品牌聊天页",
                      },
                    ]
                  : []),
              ].map((item) => (
                <Link className="quick-link" href={item.href} key={item.href}>
                  <item.icon />
                  <div>
                    <strong>{item.title}</strong>
                    <br />
                    <span>{item.text}</span>
                  </div>
                </Link>
              ))}
            </div>
          </Card>
          <Card size="small">
            <strong>先验证自己的接待流程</strong>
            <p className="muted" style={{ fontSize: 13, margin: "8px 0 0" }}>
              请确认你的成员资料已开启接待资格，在工作台切换为可接待状态，再从独立浏览器窗口打开入口。网页通知和声音需要先由你主动启用。
            </p>
          </Card>
        </Space>
      </div>
    </>
  );
}
