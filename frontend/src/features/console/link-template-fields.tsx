/** 文件作用说明：为咨询链接选择已发布用户端模板，支持恢复默认页面、跟随发布及固定版本。 */
"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { Form, Select } from "antd";
import type { FormInstance } from "antd";
import { api } from "@/lib/api";
import { ErrorNotice } from "@/components/page-state";

export type PublishedTemplatePage = {
  id: string;
  name: string;
  template_id?: string;
  current_release_id: string | null;
};
type Release = { id: string; draft_version: number; created_at: string };

/** LinkTemplateFields 仅展示可用发布页面，未选择时保持现有默认聊天界面。 */
export function LinkTemplateFields({
  form,
  tenantId,
  pages,
}: {
  form: FormInstance;
  tenantId: string;
  pages: PublishedTemplatePage[];
}) {
  const pageId = Form.useWatch("page_id", form) as string | undefined;
  const mode = Form.useWatch("binding_mode", form) as string | undefined;
  const [releases, setReleases] = useState<{ pageId: string; items: Release[] } | null>(null);
  const [error, setError] = useState<unknown>(null);
  useEffect(() => {
    if (!pageId || !tenantId) return;
    const controller = new AbortController();
    setError(null);
    api<{ items: Release[] }>(`/tenant/pages/${pageId}/releases`, {
      tenantId,
      signal: controller.signal,
    })
      .then((data) => {
        if (!controller.signal.aborted) setReleases({ pageId, items: data.items || [] });
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      });
    return () => controller.abort();
  }, [pageId, tenantId]);
  const currentReleases = releases && releases.pageId === pageId ? releases.items : [];
  return (
    <>
      <Form.Item
        name="page_id"
        label="用户端模板"
        extra={
          <>
            不绑定时使用现在的默认用户端页面。<Link href="/console/templates">管理和发布模板</Link>
          </>
        }
      >
        <Select
          allowClear
          placeholder="不绑定额外模板，使用默认页面"
          options={pages.map((page) => ({
            value: page.id,
            label: `${page.name}${page.current_release_id ? "" : "（尚未发布）"}`,
            disabled: !page.current_release_id,
          }))}
          onChange={(value) => {
            const page = pages.find((item) => item.id === value);
            form.setFieldsValue({
              binding_mode: value ? "PIN_RELEASE" : "FOLLOW_PAGE",
              pinned_release_id: page?.current_release_id || null,
            });
          }}
        />
      </Form.Item>
      {pageId && (
        <>
          <Form.Item name="binding_mode" label="模板更新方式" rules={[{ required: true }]}>
            <Select
              options={[
                { value: "PIN_RELEASE", label: "固定已发布版本" },
                { value: "FOLLOW_PAGE", label: "跟随此页面之后的发布" },
              ]}
              onChange={(value) => {
                if (value === "PIN_RELEASE" && !form.getFieldValue("pinned_release_id"))
                  form.setFieldValue(
                    "pinned_release_id",
                    pages.find((page) => page.id === pageId)?.current_release_id || null,
                  );
              }}
            />
          </Form.Item>
          {mode === "PIN_RELEASE" && (
            <Form.Item name="pinned_release_id" label="发布版本" rules={[{ required: true }]}>
              <Select
                loading={releases?.pageId !== pageId}
                options={currentReleases.map((release) => ({
                  value: release.id,
                  label: `草稿版本 ${release.draft_version} · ${new Date(release.created_at).toLocaleString("zh-CN")}`,
                }))}
              />
            </Form.Item>
          )}
          <ErrorNotice error={error} />
        </>
      )}
    </>
  );
}
