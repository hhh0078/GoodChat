/** 文件作用说明：提供平台、租户和客服工作端统一导航、租户选择与退出操作。 */
"use client";
import { useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Avatar, Button, Drawer, Dropdown, Menu, Select, Space, Tag, App } from "antd";
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
} from "@ant-design/icons";
import { Brand } from "./brand";
import { SessionProvider, useSession } from "@/features/auth/session";
import { api, errorMessage } from "@/lib/api";

/** WorkspaceChrome 根据身份呈现工作端导航与租户切换。 */
function WorkspaceChrome({ children }: { children: React.ReactNode }) {
  const { current, tenantId, setTenantId } = useSession();
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
          { key: "/console/automation", icon: <RobotOutlined />, label: "话术与提醒" },
        ]
      : []),
    ...(manages
      ? [
          { key: "/console/members", icon: <TeamOutlined />, label: "团队成员" },
          { key: "/console/links", icon: <LinkOutlined />, label: "接入链接" },
          { key: "/console/routing", icon: <PartitionOutlined />, label: "分组与分配" },
          { key: "/console/contacts", icon: <UserOutlined />, label: "客户资料" },
          { key: "/console/templates", icon: <SkinOutlined />, label: "装修与模板" },
          { key: "/console/automation", icon: <RobotOutlined />, label: "回复与运营" },
          { key: "/console/records", icon: <CustomerServiceOutlined />, label: "聊天记录" },
          { key: "/console/reports", icon: <BarChartOutlined />, label: "服务统计" },
          { key: "/console/billing", icon: <TeamOutlined />, label: "套餐与坐席" },
          { key: "/console/settings", icon: <SettingOutlined />, label: "租户设置" },
        ]
      : []),
    ...(current.user.is_platform_admin
      ? [{ key: "/platform", icon: <SafetyOutlined />, label: "平台管理" }]
      : []),
  ];
  const selected =
    [...entries]
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
  const nav = (
    <>
      <div className="sidebar-brand">
        <Brand />
      </div>
      <Menu
        mode="inline"
        items={entries}
        selectedKeys={[selected]}
        onClick={({ key }) => navigate(key)}
      />
      <div className="sidebar-bottom">
        GoodChat 网页工作端
        <br />
        让每一次沟通都有着落
      </div>
    </>
  );
  return (
    <div className="workspace">
      <aside className="workspace-sidebar">{nav}</aside>
      <div className="workspace-main">
        <header className="workspace-topbar">
          <Space>
            <Button
              className="mobile-menu"
              aria-label="打开导航"
              icon={<MenuOutlined />}
              onClick={() => setMobileOpen(true)}
            />
            <Select
              aria-label="当前工作空间"
              style={{ width: 210 }}
              value={tenantId || undefined}
              placeholder="选择工作空间"
              options={current.memberships.map((item) => ({
                label: item.tenant_name,
                value: item.tenant_id,
              }))}
              onChange={setTenantId}
              disabled={!current.memberships.length}
            />
            {member && (
              <Tag>
                {member.role === "owner" ? "所有者" : member.role === "admin" ? "管理员" : "客服"}
              </Tag>
            )}
          </Space>
          <Dropdown
            menu={{
              items: [
                { key: "settings", label: "账号与工作空间" },
                { key: "logout", label: "退出当前设备", danger: true },
              ],
              onClick: ({ key }) =>
                key === "logout" ? void logout() : router.push("/console/settings"),
            }}
            trigger={["click"]}
          >
            <Button type="text">
              <Avatar size="small" style={{ background: "#e7efdf", color: "#23684e" }}>
                {(current.user.display_name || current.user.email).slice(0, 1)}
              </Avatar>
              <span style={{ marginLeft: 8 }}>
                {current.user.display_name || current.user.email}
              </span>
            </Button>
          </Dropdown>
        </header>
        <main className="workspace-content">{children}</main>
      </div>
      <Drawer
        open={mobileOpen}
        onClose={() => setMobileOpen(false)}
        placement="left"
        width={260}
        title="工作空间导航"
      >
        {nav}
      </Drawer>
    </div>
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
