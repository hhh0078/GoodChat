/** 文件作用说明：读取后端维护的六行业静态模板，将用户选择的作品复制为本租户私有模板。 */
"use client";
import { EmptyData, ErrorNotice } from "@/components/page-state";
import { TemplatePreview } from "@/features/decoration/template-preview";
import type { Template } from "@/features/decoration/templates";
import { useTranslator } from "@/i18n/use-translator";
import { api, errorMessage } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import { Alert, App, Button, Card, Spin } from "antd";
import { useEffect, useState } from "react";
import { TemplatePreviewModal } from "./template-preview-modal";
type Industry = {
  id: string;
  key: string;
  name: string;
  description: string;
};
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
  const tr = useTranslator();
  const catalog = useResource<Industry>("/tenant/template-industries");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [previewIndustry, setPreviewIndustry] = useState<Industry | null>(null);
  const { message } = App.useApp();
  useEffect(() => {
    setPreviewIndustry(null);
  }, [tenantId]);
  /** copy 保存后端提供的完整行业文件包，复制或二创都使用当前租户自己的作品。 */
  async function copy(industry: Industry) {
    setBusy(industry.key);
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
      message.success(tr("「{p0}」已添加到我的模板", { p0: String(industry.name) }));
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
        title={tr("行业模板共用官方聊天核心，可复制使用，也可下载 ZIP 交给 AI 修改")}
        description={tr("每份模板包含 HTML、CSS、JS 和本地资源，复制后归当前租户。")}
        style={{ marginBottom: 20 }}
      />
      <ErrorNotice error={catalog.error} retry={catalog.reload} />
      <ErrorNotice error={error} />
      {catalog.loading ? (
        <Spin />
      ) : !catalog.items.length ? (
        <EmptyData text={tr("行业模板目录暂未准备好，请稍后刷新。")} />
      ) : (
        <div className="templates-grid">
          {catalog.items.map((industry) => (
            <Card key={industry.key} size="small" className="studio-template-object">
              <TemplatePreview
                path={`/tenant/template-industries/${encodeURIComponent(industry.key)}/preview`}
                name={industry.name}
                tenantId={tenantId}
                enabled={canManage}
                onPreview={() => setPreviewIndustry(industry)}
              />
              <div className="studio-template-details">
                <strong className="studio-template-name" title={industry.name}>
                  {industry.name}
                </strong>
                {canManage && (
                  <div className="studio-template-actions">
                    <Button size="small" onClick={() => setPreviewIndustry(industry)}>
                      {tr("预览 ")}
                    </Button>
                    <Button
                      size="small"
                      disabled={!!busy && busy !== industry.key}
                      loading={busy === industry.key}
                      onClick={() => void copy(industry)}
                    >
                      {tr("添加到我的模板 ")}
                    </Button>
                  </div>
                )}
              </div>
            </Card>
          ))}
        </div>
      )}
      <TemplatePreviewModal
        path={
          previewIndustry
            ? `/tenant/template-industries/${encodeURIComponent(previewIndustry.key)}/preview`
            : ""
        }
        name={previewIndustry?.name || tr("行业模板")}
        tenantId={tenantId}
        open={!!previewIndustry}
        onClose={() => setPreviewIndustry(null)}
      />
    </>
  );
}
