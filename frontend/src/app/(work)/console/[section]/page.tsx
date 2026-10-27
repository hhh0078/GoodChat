/** 文件作用说明：将租户功能路由连接到统一工作端中的真实资源页面。 */
import { notFound } from "next/navigation";
import { MembersPage } from "@/features/console/members";
import { LinksPage } from "@/features/console/links";
import { RoutingPage } from "@/features/console/routing";
import { ContactsPage } from "@/features/console/contacts";
import { SettingsPage } from "@/features/console/settings";
import { ReportsPage } from "@/features/console/reports";
import { RecordsPage } from "@/features/console/records";
import { AutomationPage } from "@/features/console/automation";
import { BillingPage } from "@/features/console/billing";
import { TemplatesPage } from "@/features/decoration/templates";
/** ConsoleSection 根据经过白名单验证的路由显示功能页。 */
export default async function ConsoleSection({ params }: { params: Promise<{ section: string }> }) {
  const { section } = await params;
  const pages: Record<string, React.ComponentType> = {
    members: MembersPage,
    links: LinksPage,
    routing: RoutingPage,
    contacts: ContactsPage,
    settings: SettingsPage,
    reports: ReportsPage,
    records: RecordsPage,
    templates: TemplatesPage,
    billing: BillingPage,
    automation: AutomationPage,
  };
  const Page = pages[section];
  if (!Page) notFound();
  return <Page />;
}
