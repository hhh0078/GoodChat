/** 文件作用说明：为平台管理员提供独立平台管理页与真实套餐配置入口。 */
"use client";
import { useState } from "react";
import { Alert, Tabs } from "antd";
import { useSession } from "@/features/auth/session";
import { PageTitle } from "@/components/page-state";
import { PlatformTenants } from "./tenants";
import { PlatformTemplates } from "./templates";
import { PlatformOverview } from "./overview";
import { PlatformUpgrades } from "./upgrades";
import { PlatformPlans } from "./plans";
/** PlatformConsole 在成员身份加载后校验平台角色，再连接平台级管理资源。 */
export function PlatformConsole() {
  const { current } = useSession();
  const [activeTab, setActiveTab] = useState("tenants");
  if (!current.user.is_platform_admin)
    return <Alert type="error" showIcon title="此账号没有平台管理权限" />;
  return (
    <section className="studio-page studio-platform" aria-label="平台管理">
      <PageTitle title="平台管理" description="维护租户、套餐版本与公共模板。" />
      <div className={`studio-platform-body${activeTab === "tenants" ? " is-tenants" : ""}`}>
        <Tabs
          className="studio-tabs"
          activeKey={activeTab}
          onChange={setActiveTab}
          items={[
            {
              key: "overview",
              label: "平台概览",
              children: <PlatformOverview onNavigate={setActiveTab} />,
            },
            {
              key: "tenants",
              label: "租户运营",
              children: <PlatformTenants onManagePlans={() => setActiveTab("plans")} />,
            },
            { key: "templates", label: "公共装修模板", children: <PlatformTemplates /> },
            { key: "plans", label: "套餐版本与官网价格", children: <PlatformPlans /> },
            { key: "upgrades", label: "升级申请与人工开通", children: <PlatformUpgrades /> },
          ]}
        />
      </div>
    </section>
  );
}
