/** 文件作用说明：为平台管理员提供独立平台管理页与真实套餐配置入口。 */
"use client";
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
  if (!current.user.is_platform_admin)
    return <Alert type="error" showIcon title="此账号没有平台管理权限" />;
  return (
    <>
      <PageTitle
        title="平台管理"
        description="平台配置、租户与公开套餐由平台管理员维护，租户成员不能通过注册获得此权限。"
      />
      <div className="table-card">
        <Tabs
          items={[
            { key: "overview", label: "平台概览", children: <PlatformOverview /> },
            { key: "tenants", label: "租户运营", children: <PlatformTenants /> },
            { key: "templates", label: "公共装修模板", children: <PlatformTemplates /> },
            { key: "plans", label: "套餐版本与官网价格", children: <PlatformPlans /> },
            { key: "upgrades", label: "升级申请与人工开通", children: <PlatformUpgrades /> },
          ]}
        />
      </div>
    </>
  );
}
