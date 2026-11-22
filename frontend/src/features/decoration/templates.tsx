/** 文件作用说明：管理租户私有与平台模板、创建装修页面，并引导使用外部AI编辑完整静态模板。 */
"use client";
import { showRequestError } from "@/lib/request-error";
import { EmptyData, ErrorNotice, PageTitle } from "@/components/page-state";
import { IndustryTemplates } from "@/features/assistant/industry-templates";
import { TemplateActions } from "@/features/assistant/template-actions";
import { TemplateImport } from "@/features/assistant/template-import";
import { TemplatePreviewModal } from "@/features/assistant/template-preview-modal";
import { useSession } from "@/features/auth/session";
import { translate as tr } from "@/i18n/runtime";
import { useTranslator } from "@/i18n/use-translator";
import { useLocale } from "next-intl";
import { api } from "@/lib/api";
import type { Resource } from "@/lib/types";
import { useResource } from "@/lib/use-resource";
import { ArrowRightOutlined, PlusOutlined, RobotOutlined, SkinOutlined } from "@ant-design/icons";
import {
  Alert,
  App,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Spin,
  Table,
  Tabs,
  Tag,
} from "antd";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { AIDecorationGuide } from "./ai-decoration-guide";
import { CodePackages } from "./code-packages";
import { starterCodeDesign } from "./starter-code";
import { TemplatePreview } from "./template-preview";
export type Design = {
  schema_version?: number;
  theme?: {
    primary_color?: string;
    background?: string;
    text_color?: string;
    radius?: number;
  };
  brand?: {
    title?: string;
    subtitle?: string;
    logo_url?: string;
  };
  blocks?: {
    id: string;
    type: string;
    title?: string;
    content?: string;
    items?: unknown[];
  }[];
  custom_css?: string;
  html?: string;
  css?: string;
  javascript?: string;
  package_id?: string;
  platform_package_id?: string;
};
export type Template = Resource & {
  industry_key?: string;
  render_mode: "schema" | "code";
  design: Design;
  version: number;
};
/** starterDesign 返回可编辑的本地初始设计，仅在用户明确保存后写入后端。 */
export function starterDesign(): Design {
  return {
    schema_version: 1,
    theme: { primary_color: "#23684e", background: "#f4f6f2", text_color: "#21342c", radius: 14 },
    brand: { title: tr("在线客服"), subtitle: tr("有问题，随时聊聊"), logo_url: "" },
    blocks: [
      {
        id: "welcome",
        type: "welcome",
        title: tr("欢迎咨询"),
        content: tr("请告诉我们你需要什么帮助。"),
        items: [],
      },
      { id: "chat", type: "chat", title: tr("在线咨询"), content: "", items: [] },
    ],
    custom_css: "",
  };
}
/** TemplatesPage 展示真实模板与页面，提供可导入与回退的静态装修流程。 */
export function TemplatesPage() {
  const tr = useTranslator();
  const locale = useLocale();
  const { tenantId, current } = useSession();
  const member = current.memberships.find((item) => item.tenant_id === tenantId);
  const manages = member?.role === "owner" || member?.role === "admin";
  const router = useRouter();
  const { message } = App.useApp();
  const [templates, setTemplates] = useState<Template[]>([]);
  const [publicTemplates, setPublicTemplates] = useState<Template[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  const [guideOpen, setGuideOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [pageTemplate, setPageTemplate] = useState<Template | null>(null);
  const [previewTemplate, setPreviewTemplate] = useState<Template | null>(null);
  const [busy, setBusy] = useState(false);
  const [submitError, setSubmitError] = useState<unknown>(null);
  const [form] = Form.useForm();
  const [pageForm] = Form.useForm();
  const pages = useResource<Resource>("/tenant/pages");
  useEffect(() => {
    setPreviewTemplate(null);
  }, [tenantId]);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    api<{
      items: Template[];
      platform_templates: Template[];
    }>("/tenant/templates", {
      tenantId,
      signal: controller.signal,
    })
      .then((data) => {
        if (controller.signal.aborted) return;
        setTemplates(data.items || []);
        setPublicTemplates(data.platform_templates || []);
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [tenantId, revision, locale]);
  /** createTemplate 保存租户初始模板，成功后才打开编辑页。 */
  async function createTemplate(values: Record<string, string>) {
    setBusy(true);
    setSubmitError(null);
    try {
      const item = await api<Template>("/tenant/templates", {
        method: "POST",
        tenantId,
        bodyJSON: {
          ...values,
          design: values.render_mode === "code" ? starterCodeDesign() : starterDesign(),
        },
        idempotent: true,
      });
      setCreateOpen(false);
      router.push(`/console/templates/${item.id}`);
    } catch (problem) {
      setSubmitError(problem);
    } finally {
      setBusy(false);
    }
  }
  /** createPage 使用选定模板创建独立页面，不覆盖模板。 */
  async function createPage(values: Record<string, string>) {
    setBusy(true);
    setSubmitError(null);
    try {
      const item = await api<Resource>("/tenant/pages", {
        method: "POST",
        tenantId,
        bodyJSON: { name: values.name, template_id: pageTemplate?.id },
        idempotent: true,
      });
      setPageTemplate(null);
      router.push(`/console/pages/${item.id}`);
    } catch (problem) {
      setSubmitError(problem);
    } finally {
      setBusy(false);
    }
  }
  /** duplicate 复制私有或公共设计为本租户自己的模板。 */
  async function duplicate(item: Template, isPublic: boolean) {
    try {
      await api(`/tenant/templates/${item.id}/duplicate`, {
        method: "POST",
        tenantId,
        bodyJSON: {},
        idempotent: true,
      });
      message.success(tr("已保存为本租户模板"));
      setRevision((value) => value + 1);
    } catch (problem) {
      showRequestError(problem);
    }
  }
  /** cards 输出明确归属的模板操作，公共模板先复制后编辑。 */
  function cards(items: Template[], isPublic = false) {
    if (loading) return <Spin />;
    if (!items.length)
      return (
        <EmptyData
          text={
            isPublic
              ? tr("平台尚未发布模板")
              : tr("还没有私有模板，可以选择行业模板，或使用 AI 编辑 ZIP 后导入。")
          }
        />
      );
    return (
      <div className="templates-grid">
        {items.map((item) => (
          <Card key={item.id} size="small" className="studio-template-object">
            <TemplatePreview
              key={`${tenantId}:${item.id}:${item.version}`}
              path={`/tenant/templates/${item.id}/preview`}
              name={item.name || tr("未命名模板")}
              tenantId={tenantId}
              enabled={manages}
              onPreview={() => setPreviewTemplate(item)}
            />
            <div className="studio-template-details">
              <strong className="studio-template-name" title={item.name}>
                {item.name}
              </strong>
              {manages && (
                <div className="studio-template-actions">
                  <TemplateActions
                    template={item}
                    isPublic={isPublic}
                    extraActions={[
                      {
                        key: "edit",
                        label: tr("编辑模板"),
                        onClick: () => router.push(`/console/templates/${item.id}`),
                      },
                      {
                        key: "duplicate",
                        label: tr("复制模板"),
                        onClick: () => void duplicate(item, false),
                      },
                      {
                        key: "page",
                        label: tr("创建页面"),
                        disabled: item.status !== undefined && item.status !== "ready",
                        onClick: () => {
                          pageForm.resetFields();
                          pageForm.setFieldsValue({
                            name: tr("{p0} 页面", { p0: String(item.name) }),
                          });
                          setSubmitError(null);
                          setPageTemplate(item);
                        },
                      },
                    ]}
                    onChanged={() => {
                      setRevision((value) => value + 1);
                      pages.reload();
                    }}
                  />
                  {isPublic && (
                    <Button size="small" onClick={() => void duplicate(item, true)}>
                      {tr("添加到我的模板 ")}
                    </Button>
                  )}
                </div>
              )}
            </div>
          </Card>
        ))}
      </div>
    );
  }
  return (
    <section className="studio-page studio-templates" aria-label={tr("装修与模板")}>
      <AIDecorationGuide open={guideOpen} onClose={() => setGuideOpen(false)} />
      <PageTitle
        title={tr("装修与模板")}
        description={tr("让客服页面，成为品牌的一部分。")}
        action={
          <Space wrap>
            {manages && (
              <TemplateImport
                tenantId={tenantId}
                onImported={() => setRevision((value) => value + 1)}
              />
            )}
            {manages && (
              <Button
                icon={<PlusOutlined />}
                onClick={() => {
                  setSubmitError(null);
                  form.resetFields();
                  form.setFieldsValue({ render_mode: "schema" });
                  setCreateOpen(true);
                }}
              >
                {tr("创建模板 ")}
              </Button>
            )}
            {manages && (
              <Button type="primary" icon={<RobotOutlined />} onClick={() => setGuideOpen(true)}>
                {tr("用 AI 装修")}
              </Button>
            )}
          </Space>
        }
      />
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      <Tabs
        className="studio-tabs"
        items={[
          {
            key: "mine",
            label: tr("我的模板"),
            destroyOnHidden: true,
            children: (
              <>
                {manages && (
                  <div className="studio-ai-entry">
                    <span className="studio-ai-entry-icon" aria-hidden="true">
                      <SkinOutlined />
                    </span>
                    <div>
                      <h2>{tr("用 AI，设计自己的客服页面")}</h2>
                      <p>
                        {tr("复制提示词和基础 ZIP，在你熟悉的 AI 工具中设计，再导入预览和发布。")}
                      </p>
                    </div>
                    <Button onClick={() => setGuideOpen(true)}>
                      {tr("查看使用方法")}
                      <ArrowRightOutlined />
                    </Button>
                  </div>
                )}
                {cards(templates)}
              </>
            ),
          },
          {
            key: "industry",
            label: tr("行业模板"),
            destroyOnHidden: true,
            children: (
              <IndustryTemplates
                tenantId={tenantId}
                canManage={manages}
                onChanged={() => setRevision((value) => value + 1)}
              />
            ),
          },
          {
            key: "platform",
            label: tr("平台模板"),
            destroyOnHidden: true,
            children: cards(publicTemplates, true),
          },
          { key: "resources", label: tr("私有资源包"), children: <CodePackages key={tenantId} /> },
          {
            key: "pages",
            label: tr("装修页面与发布"),
            children: (
              <div className="table-card">
                <ErrorNotice error={pages.error} retry={pages.reload} />
                <Table
                  rowKey="id"
                  dataSource={pages.items}
                  loading={pages.loading}
                  locale={{
                    emptyText: (
                      <EmptyData text={tr("从我的模板创建页面后，便可编辑并发布到链接。")} />
                    ),
                  }}
                  columns={[
                    { title: tr("页面名称"), dataIndex: "name" },
                    { title: tr("版本"), dataIndex: "version" },
                    {
                      title: tr("发布状态"),
                      dataIndex: "current_release_id",
                      render: (value) => (
                        <Tag color={value ? "blue" : "default"}>
                          {value ? tr("已有发布") : tr("未发布")}
                        </Tag>
                      ),
                    },
                    {
                      title: tr("操作"),
                      render: (_, row) => (
                        <Button type="link" href={`/console/pages/${row.id}`}>
                          {tr("编辑与发布 ")}
                        </Button>
                      ),
                    },
                  ]}
                />
              </div>
            ),
          },
        ]}
      />
      {manages && previewTemplate && (
        <TemplatePreviewModal
          path={`/tenant/templates/${encodeURIComponent(previewTemplate.id)}/preview`}
          name={previewTemplate.name || tr("未命名模板")}
          tenantId={tenantId}
          open
          onClose={() => setPreviewTemplate(null)}
        />
      )}

      <Modal
        rootClassName="studio-dialog"
        title={tr("从模板创建独立页面")}
        open={!!pageTemplate}
        onCancel={() => setPageTemplate(null)}
        footer={null}
      >
        <ErrorNotice error={submitError} />
        <Form form={pageForm} layout="vertical" onFinish={createPage}>
          <Form.Item
            name="name"
            label={tr("页面名称")}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={100} />
          </Form.Item>
          <p className="muted">{tr("页面是模板的独立副本，后续修改不会覆盖原模板。")}</p>
          <Button type="primary" htmlType="submit" loading={busy}>
            {tr("创建页面 ")}
          </Button>
        </Form>
      </Modal>
    </section>
  );
}
