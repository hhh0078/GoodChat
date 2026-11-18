/** 文件作用说明：为平台管理员提供独立平台管理页与真实套餐配置入口。 */
"use client";
import { PlatformMail } from "@/features/notifications/platform";
import { PlatformCustomerAI } from "@/features/ai/platform";
import { PageTitle } from "@/components/page-state";
import { useSession } from "@/features/auth/session";
import { useTranslator } from "@/i18n/use-translator";
import { Alert, Tabs } from "antd";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { ClientReleases } from "./client-releases";
import { PlatformOverview } from "./overview";
import { PlatformPlans } from "./plans";
import { PlatformTemplates } from "./templates";
import { PlatformTenants } from "./tenants";
import { PlatformUpgrades } from "./upgrades";
/** PlatformConsole 在成员身份加载后校验平台角色，再连接平台级管理资源。 */
export function PlatformConsole() {
  const tr = useTranslator();
  const { current } = useSession();
  const query = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const selected = query.get("tab") || "tenants";
  const activeTab = [
    "overview",
    "tenants",
    "templates",
    "plans",
    "upgrades",
    "mail",
    "customer-ai",
    "clients",
  ].includes(selected)
    ? selected
    : "tenants";
  /** setActiveTab 将页签保存在当前URL，初始化与语言刷新后仍保留用户选择。 */
  function setActiveTab(key: string) {
    const next = new URLSearchParams(query.toString());
    next.set("tab", key);
    router.replace(pathname + "?" + next.toString(), { scroll: false });
  }
  if (!current.user.is_platform_admin)
    return <Alert type="error" showIcon title={tr("此账号没有平台管理权限")} />;
  return (
    <section className="studio-page studio-platform" aria-label={tr("平台管理")}>
      <PageTitle title={tr("平台管理")} description={tr("维护租户、套餐版本与公共模板。")} />
      <div className={`studio-platform-body${activeTab === "tenants" ? " is-tenants" : ""}`}>
        <Tabs
          className="studio-tabs"
          activeKey={activeTab}
          onChange={setActiveTab}
          items={[
            {
              key: "overview",
              label: tr("平台概览"),
              children: <PlatformOverview onNavigate={setActiveTab} />,
            },
            {
              key: "tenants",
              label: tr("租户运营"),
              children: <PlatformTenants onManagePlans={() => setActiveTab("plans")} />,
            },
            { key: "templates", label: tr("公共装修模板"), children: <PlatformTemplates /> },
            { key: "plans", label: tr("套餐版本与官网价格"), children: <PlatformPlans /> },
            { key: "upgrades", label: tr("升级申请与人工开通"), children: <PlatformUpgrades /> },
            { key: "mail", label: tr("邮件与通知"), children: <PlatformMail /> },
            { key: "customer-ai", label: tr("客服AI与计费"), children: <PlatformCustomerAI /> },
            { key: "clients", label: tr("客户端下载与版本"), children: <ClientReleases /> },
          ]}
        />
      </div>
    </section>
  );
}
