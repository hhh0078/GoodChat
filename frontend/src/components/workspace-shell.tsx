/** 文件作用说明：提供平台、租户和客服工作端统一导航、租户选择与退出操作。 */
"use client";
import { useState } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { Avatar, Button, Drawer, Dropdown, Menu, Space, Tag, App, Tooltip } from "antd";
import {
  AppstoreOutlined,
  BarChartOutlined,
  CustomerServiceOutlined,
  LinkOutlined,
  MenuOutlined,
  PartitionOutlined,
  SettingOutlined,
  SkinOutlined,
  TeamOutlined,
  UserOutlined,
  RobotOutlined,
  SafetyOutlined,
  QuestionCircleOutlined,
} from "@ant-design/icons";
import { Brand } from "./brand";
import { SessionProvider, useSession } from "@/features/auth/session";
import { api, errorMessage } from "@/lib/api";
import { WorkspaceRemindersProvider } from "@/features/chat/workspace-reminders";

/** WorkspaceChrome 根据身份呈现工作端导航与租户切换。 */
function WorkspaceChrome({ children }: { children: React.ReactNode }) {
  const { current, tenantId } = useSession();
  const pathname = usePathname();
  const router = useRouter();
  const [mobileOpen, setMobileOpen] = useState(false);
  const { message } = App.useApp();
  const member = current.memberships.find((item) => item.tenant_id === tenantId);
  const manages = member?.role === "owner" || member?.role === "admin";
  const entries = [
    { key: "/console", icon: <AppstoreOutlined />, label: "工作空间" },
    { key: "/workspace", icon: <CustomerServiceOutlined />, label: "客服工作台" },
    ...(!manages
      ? [
          { key: "/console/records", icon: <CustomerServiceOutlined />, label: "我的聊天记录" },
          { key: "/console/automation", icon: <RobotOutlined />, label: "话术与留言" },
        ]
      : []),
    ...(manages
      ? [
          { key: "/console/members", icon: <TeamOutlined />, label: "团队成员" },
          { key: "/console/links", icon: <LinkOutlined />, label: "接入管理" },
          { key: "/console/routing", icon: <PartitionOutlined />, label: "分组与分配" },
          { key: "/console/contacts", icon: <UserOutlined />, label: "客户资料" },
          { key: "/console/templates", icon: <SkinOutlined />, label: "装修与模板" },
          { key: "/console/automation", icon: <RobotOutlined />, label: "回复与运营" },
          { key: "/console/records", icon: <CustomerServiceOutlined />, label: "聊天记录" },
          { key: "/console/reports", icon: <BarChartOutlined />, label: "服务统计" },
          { key: "/console/member-statistics", icon: <BarChartOutlined />, label: "成员统计" },
          { key: "/console/billing", icon: <TeamOutlined />, label: "套餐与坐席" },
          { key: "/console/settings", icon: <SettingOutlined />, label: "租户设置" },
        ]
      : []),
    ...(!manages
      ? [{ key: "/console/settings", icon: <SettingOutlined />, label: "我的账号与头像" }]
      : []),
    ...(current.user.is_platform_admin
      ? [
          { key: "/platform", icon: <SafetyOutlined />, label: "平台管理" },
          { key: "/platform/monitoring", icon: <BarChartOutlined />, label: "服务器监控" },
        ]
      : []),
  ];
  const selected = pathname.startsWith("/console/pages/")
    ? "/console/templates"
    : [...entries]
        .reverse()
        .find((item) => pathname === item.key || pathname.startsWith(`${item.key}/`))?.key ||
      "/console";
  /** navigate 切换工作端页面并收起手机导航。 */
  function navigate(key: string) {
    router.push(key);
    setMobileOpen(false);
  }
  /** logout 撤销当前服务端会话，成功后返回登录。 */
  async function logout() {
    try {
      await api("/auth/logout", { method: "POST" });
      localStorage.removeItem("goodchat.activeTenant");
      router.replace("/login");
      router.refresh();
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  const groups = [
    { label: "概览", icon: <AppstoreOutlined />, paths: ["/console"] },
    { label: "客服", icon: <CustomerServiceOutlined />, paths: ["/workspace"] },
    { label: "客户", icon: <UserOutlined />, paths: ["/console/contacts", "/console/records"] },
    { label: "运营", icon: <RobotOutlined />, paths: manages ? [] : ["/console/automation"] },
    {
      label: manages ? "管理" : "账号",
      icon: <SettingOutlined />,
      paths: manages
        ? [
            "/console/members",
            "/console/member-statistics",
            "/console/links",
            "/console/routing",
            "/console/automation",
            "/console/templates",
            "/console/reports",
            "/console/billing",
            "/console/settings",
          ]
        : ["/console/settings"],
    },
    { label: "平台", icon: <SafetyOutlined />, paths: ["/platform", "/platform/monitoring"] },
  ]
    .map((group) => ({
      ...group,
      items: entries.filter((entry) => group.paths.includes(entry.key)),
    }))
    .filter((group) => group.items.length > 0);
  const serving = pathname === "/workspace";
  const pageLabel = entries.find((entry) => entry.key === selected)?.label || "工作空间";
  const sections = [
    { label: "工作空间", paths: ["/console", "/workspace"] },
    {
      label: "客户服务",
      paths: [
        "/console/contacts",
        "/console/records",
        ...(!manages ? ["/console/automation", "/console/settings"] : []),
      ],
    },
    {
      label: "租户管理",
      paths: manages
        ? [
            "/console/members",
            "/console/member-statistics",
            "/console/links",
            "/console/routing",
            "/console/automation",
            "/console/templates",
            "/console/billing",
            "/console/settings",
            "/console/reports",
          ]
        : [],
    },
    { label: "平台管理员", paths: ["/platform", "/platform/monitoring"] },
  ]
    .map((section) => ({
      type: "group" as const,
      key: section.label,
      label: section.label,
      children: section.paths.flatMap((path) => entries.filter((entry) => entry.key === path)),
    }))
    .filter((section) => section.children.length > 0);
  const nav = (
    <>
      <div className="sidebar-brand">
        <Brand />
      </div>
      <div className="sidebar-workspace">
        <span className="shell-tenant-name">{member?.tenant_name || "工作空间"}</span>
      </div>
      <Menu
        mode="inline"
        items={sections}
        selectedKeys={[selected]}
        onClick={({ key }) => navigate(key)}
      />
      <div className="sidebar-bottom">
        <Avatar size={30} src={member?.avatar_url || undefined}>
          {(current.user.display_name || current.user.email).slice(0, 1)}
        </Avatar>
        <div>
          <strong>{current.user.display_name || current.user.email}</strong>
          <span>
            {member?.role === "owner"
              ? "工作空间所有者"
              : member?.role === "admin"
                ? "租户管理员"
                : "客服成员"}
          </span>
        </div>
      </div>
    </>
  );
  return (
    <WorkspaceRemindersProvider
      tenantId={tenantId}
      memberId={member?.id || ""}
      canServe={!!member?.can_serve}
    >
      <div className={`workspace workbench-shell ${serving ? "is-serving" : ""}`}>
        <aside
          className={`workspace-sidebar ${serving ? "workspace-rail" : "workspace-full-nav"}`}
          aria-label="工作端主导航"
        >
          {serving ? (
            <>
              <div className="rail-brand">
                <Brand />
              </div>
              <nav className="rail-links">
                {groups.map((group) => {
                  const active = group.items.some((entry) => entry.key === selected);
                  const content = (
                    <>
                      {group.icon}
                      <span>{group.label}</span>
                    </>
                  );
                  return group.items.length === 1 ? (
                    <Tooltip key={group.label} title={group.items[0].label} placement="right">
                      <Link
                        className={`rail-link ${active ? "active" : ""}`}
                        aria-label={group.items[0].label}
                        aria-current={active ? "page" : undefined}
                        href={group.items[0].key}
                      >
                        {content}
                      </Link>
                    </Tooltip>
                  ) : (
                    <Dropdown
                      key={group.label}
                      trigger={["click"]}
                      placement="rightTop"
                      menu={{
                        items: group.items,
                        selectedKeys: [selected],
                        onClick: ({ key }) => navigate(key),
                      }}
                    >
                      <button
                        className={`rail-link ${active ? "active" : ""}`}
                        aria-label={`${group.label}导航`}
                      >
                        {content}
                      </button>
                    </Dropdown>
                  );
                })}
              </nav>
              <Tooltip title="账号与工作空间" placement="right">
                <Button
                  className="rail-account"
                  type="text"
                  aria-label="账号与工作空间"
                  onClick={() => navigate("/console/settings?tab=account")}
                >
                  <Avatar size={30} src={member?.avatar_url || undefined}>
                    {(current.user.display_name || current.user.email).slice(0, 1)}
                  </Avatar>
                </Button>
              </Tooltip>
            </>
          ) : (
            nav
          )}
        </aside>
        <div className="workspace-main">
          <header className="workspace-topbar">
            <Space className="shell-identity" size={14}>
              <Button
                className="mobile-menu"
                aria-label="打开导航"
                icon={<MenuOutlined />}
                onClick={() => setMobileOpen(true)}
              />
              {serving ? (
                <span className="shell-wordmark">GoodChat</span>
              ) : (
                <span className="shell-breadcrumb">
                  工作空间 <span aria-hidden="true">/</span>
                </span>
              )}
              {!serving && (
                <span className="shell-page-label">
                  {selected === "/console" ? "概览" : pageLabel}
                </span>
              )}
              {serving && <span className="shell-tenant-name">{member?.tenant_name}</span>}
              {serving && <span className="shell-page-label">{pageLabel}</span>}
              {member && !serving && (
                <Tag className="shell-role">
                  {member.role === "owner" ? "所有者" : member.role === "admin" ? "管理员" : "客服"}
                </Tag>
              )}
            </Space>
            <div className="shell-actions">
              <div id="workspace-header-actions" />
              {!serving && (
                <Link href="/help" className="shell-help">
                  <QuestionCircleOutlined aria-hidden="true" />
                  使用帮助
                </Link>
              )}
              <Dropdown
                menu={{
                  items: [
                    { key: "settings", label: "我的账号与头像" },
                    { key: "logout", label: "退出当前设备", danger: true },
                  ],
                  onClick: ({ key }) =>
                    key === "logout" ? void logout() : router.push("/console/settings?tab=account"),
                }}
                trigger={["click"]}
              >
                <Button type="text" className="shell-account" aria-label="账号菜单">
                  <Avatar size="small" src={member?.avatar_url || undefined}>
                    {(current.user.display_name || current.user.email).slice(0, 1)}
                  </Avatar>
                  <span style={{ marginLeft: 8 }}>
                    {current.user.display_name || current.user.email}
                  </span>
                </Button>
              </Dropdown>
            </div>
          </header>
          <main key={tenantId} className="workspace-content">
            {children}
          </main>
        </div>
        <Drawer
          rootClassName="workspace-nav-drawer"
          open={mobileOpen}
          onClose={() => setMobileOpen(false)}
          placement="left"
          width={260}
          title="工作空间导航"
        >
          {nav}
        </Drawer>
      </div>
    </WorkspaceRemindersProvider>
  );
}
/** WorkspaceShell 将工作端布局包裹在服务端身份加载边界内。 */
export function WorkspaceShell({ children }: { children: React.ReactNode }) {
  return (
    <SessionProvider>
      <WorkspaceChrome>{children}</WorkspaceChrome>
    </SessionProvider>
  );
}
