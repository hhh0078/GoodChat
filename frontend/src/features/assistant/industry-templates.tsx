/** 文件作用说明：读取后端维护的六行业静态模板，将用户选择的作品复制为本租户私有模板。 */
"use client";
import { useState } from "react";
import { Alert, App, Button, Spin } from "antd";
import {
  BankOutlined,
  ShoppingOutlined,
  ReadOutlined,
  HomeOutlined,
  MedicineBoxOutlined,
  CustomerServiceOutlined,
} from "@ant-design/icons";
import { api, errorMessage } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import { EmptyData, ErrorNotice } from "@/components/page-state";
import { useAssistantLauncher } from "./launcher";
import { TemplatePreviewButton } from "./template-preview-modal";
import type { Template } from "@/features/decoration/templates";

type Industry = { id: string; key: string; name: string; description: string };
const industryIcons = [
  BankOutlined,
  ShoppingOutlined,
  ReadOutlined,
  MedicineBoxOutlined,
  HomeOutlined,
  CustomerServiceOutlined,
];

/** IndustryTemplates 只展示真实目录和复制接口结果，不在客户端伪造已保存模板。 */
export function IndustryTemplates({
  tenantId,
  canManage,
  onChanged,
}: {
  tenantId: string;
  canManage: boolean;
  onChanged: () => void;
}) {
  const catalog = useResource<Industry>("/tenant/template-industries");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState<unknown>(null);
  const { message } = App.useApp();
  const { openAssistant } = useAssistantLauncher();

  /** copy 保存后端提供的完整行业文件包，复制或二创都使用当前租户自己的作品。 */
  async function copy(industry: Industry, remix: boolean) {
    setBusy(`${industry.key}:${remix}`);
    setError(null);
    try {
      const template = await api<Template>(
        `/tenant/template-industries/${encodeURIComponent(industry.key)}/copy`,
        {
          tenantId,
          method: "POST",
          bodyJSON: {},
          idempotent: true,
        },
      );
      onChanged();
      if (remix)
        openAssistant({
          templateId: template.id,
          templateName: industry.name,
          prompt: `请在${industry.name}的基础上为我的品牌调整设计。`,
        });
      else message.success(`「${industry.name}」已添加到我的模板`);
    } catch (problem) {
      setError(new Error(errorMessage(problem)));
    } finally {
      setBusy("");
    }
  }

  return (
    <>
      <Alert
        type="info"
        showIcon
        title="行业模板共用官方聊天核心，可直接使用，也可继续 AI 二创"
        description="每份模板包含 HTML、CSS、JS 和本地资源，复制后归当前租户。"
        style={{ marginBottom: 20 }}
      />
      <ErrorNotice error={catalog.error} retry={catalog.reload} />
      <ErrorNotice error={error} />
      {catalog.loading ? (
        <Spin />
      ) : !catalog.items.length ? (
        <EmptyData text="行业模板目录暂未准备好，请稍后刷新。" />
      ) : (
        <div className="assistant-industry-grid">
          {catalog.items.map((industry, index) => {
            const Icon = industryIcons[index % industryIcons.length];
            return (
              <article key={industry.key} className="assistant-industry-card">
                <span className="industry-mark" aria-hidden="true">
                  <Icon />
                </span>
                <h3>{industry.name}</h3>
                <p>{industry.description}</p>
                {canManage && (
                  <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
                    <TemplatePreviewButton
                      path={`/tenant/template-industries/${encodeURIComponent(industry.key)}/preview`}
                      name={industry.name}
                      tenantId={tenantId}
                    />
                    <Button
                      disabled={!canManage || (!!busy && busy !== `${industry.key}:false`)}
                      loading={busy === `${industry.key}:false`}
                      onClick={() => void copy(industry, false)}
                    >
                      添加到我的模板
                    </Button>
                    <Button
                      type="link"
                      disabled={!canManage || (!!busy && busy !== `${industry.key}:true`)}
                      loading={busy === `${industry.key}:true`}
                      onClick={() => void copy(industry, true)}
                    >
                      AI 二创
                    </Button>
                  </div>
                )}
              </article>
            );
          })}
        </div>
      )}
    </>
  );
}
