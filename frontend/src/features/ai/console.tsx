/** 文件作用说明：为租户管理员和客服集中提供知识库、媒体、AI话术、机器人及用量入口。 */
"use client";
import { useSession } from "@/features/auth/session";
import { useTranslator } from "@/i18n/use-translator";
import { Tabs } from "antd";
import { AIBotSettingsPage } from "./bot";
import { AIContentList } from "./content";
import { AIMediaLibrary } from "./media";
import { AIUsagePage } from "./usage";
import "./customer-ai.css";
/** AICustomerServicePage 租户切换时销毁旧表单及上传请求。 */
export function AICustomerServicePage() {
  const tr = useTranslator();
  const { tenantId } = useSession();
  return (
    <section className="studio-page ai-console" key={tenantId} aria-label={tr("AI客服")}>
      <Tabs
        destroyOnHidden
        items={[
          { key: "bot", label: tr("机器人设置"), children: <AIBotSettingsPage /> },
          { key: "knowledge", label: tr("知识库"), children: <AIContentList kind="knowledge" /> },
          { key: "media", label: tr("媒体库"), children: <AIMediaLibrary /> },
          { key: "scripts", label: tr("AI话术"), children: <AIContentList kind="scripts" /> },
          { key: "usage", label: tr("用量与账单"), children: <AIUsagePage /> },
        ]}
      />
    </section>
  );
}
