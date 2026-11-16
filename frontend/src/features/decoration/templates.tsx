/** 文件作用说明：管理租户私有与平台模板、创建装修页面，并真实提交和查询 AI 创作任务。 */
"use client";
import { EmptyData, ErrorNotice, PageTitle } from "@/components/page-state";
import { AssistantHistory } from "@/features/assistant/history";
import { IndustryTemplates } from "@/features/assistant/industry-templates";
import { useAssistantLauncher } from "@/features/assistant/launcher";
import { TemplateActions } from "@/features/assistant/template-actions";
import { TemplateImport } from "@/features/assistant/template-import";
import { TemplatePreviewModal } from "@/features/assistant/template-preview-modal";
import { useSession } from "@/features/auth/session";
import { translate as tr } from "@/i18n/runtime";
import { useTranslator } from "@/i18n/use-translator";
import { api, errorMessage } from "@/lib/api";
import type { Resource } from "@/lib/types";
import { useResource } from "@/lib/use-resource";
import { ArrowRightOutlined, PlusOutlined, RobotOutlined, SkinOutlined } from "@ant-design/icons";
import {
  Alert,
  App,
  Button,
  Card,
  Drawer,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Spin,
  Table,
  Tabs,
  Tag,
} from "antd";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
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
/** TemplatesPage 展示真实模板与页面，并管理可追踪的 AI 创建过程。 */
export function TemplatesPage() {
  const tr = useTranslator();
  const { tenantId, current } = useSession();
  const { openAssistant } = useAssistantLauncher();
  const member = current.memberships.find((item) => item.tenant_id === tenantId);
  const manages = member?.role === "owner" || member?.role === "admin";
  const router = useRouter();
  const { message } = App.useApp();
  const [templates, setTemplates] = useState<Template[]>([]);
  const [publicTemplates, setPublicTemplates] = useState<Template[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  const [createOpen, setCreateOpen] = useState(false);
  const [aiOpen, setAiOpen] = useState(false);
  const [pageTemplate, setPageTemplate] = useState<Template | null>(null);
  const [previewTemplate, setPreviewTemplate] = useState<Template | null>(null);
  const [busy, setBusy] = useState(false);
  const [submitError, setSubmitError] = useState<unknown>(null);
  const [job, setJob] = useState<Resource | null>(null);
  const [form] = Form.useForm();
  const [pageForm] = Form.useForm();
  const pages = useResource<Resource>("/tenant/pages");
  const aiJobs = useResource<Resource>("/tenant/ai-design-jobs");
  useEffect(() => {
    setPreviewTemplate(null);
  }, [tenantId]);
  useEffect(() => {
    /** refreshTemplates 在全局助手完成生成后重新读取真实模板与页面。 */
    function refreshTemplates() {
      setRevision((value) => value + 1);
      pages.reload();
      aiJobs.reload();
    }
    window.addEventListener("goodchat:templates-changed", refreshTemplates);
    return () => window.removeEventListener("goodchat:templates-changed", refreshTemplates);
  }, [pages.reload, aiJobs.reload]);
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
  }, [tenantId, revision]);
  useEffect(() => {
    if (!job?.id || ["succeeded", "failed", "cancelled"].includes(String(job.status))) return;
    let stopped = false;
    const timer = setInterval(() => {
      api<Resource>(`/tenant/ai-design-jobs/${job.id}`, { tenantId })
        .then((data) => {
          if (stopped) return;
          setJob(data);
          setSubmitError(null);
          if (["succeeded", "failed", "cancelled"].includes(String(data.status))) aiJobs.reload();
          if (data.status === "succeeded") setRevision((value) => value + 1);
        })
        .catch((problem) => {
          if (!stopped) setSubmitError(problem);
        });
    }, 2000);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  }, [job?.id, job?.status, tenantId]);
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
      message.error(errorMessage(problem));
    }
  }
  /** cancelJob 请求取消当前创作，不把客户端停止轮询当作任务取消。 */
  async function cancelJob() {
    if (!job) return;
    try {
      const result = await api<Resource>(`/tenant/ai-design-jobs/${job.id}/cancel`, {
        method: "POST",
        tenantId,
        bodyJSON: {},
      });
      setJob(result);
      aiJobs.reload();
      message.success(tr("取消请求已处理"));
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  /** saveCandidate 将未应用的模型结果另存为新私有模板，不覆盖当前人工草稿。 */
  async function saveCandidate() {
    if (!job?.result) return;
    setBusy(true);
    try {
      const saved = await api<Template>("/tenant/templates", {
        method: "POST",
        tenantId,
        bodyJSON: {
          name: tr("{p0} 候选", { p0: String(String(job.name || tr("AI设计")).slice(0, 65)) }),
          render_mode: job.render_mode,
          design: job.result,
        },
        idempotent: true,
      });
      message.success(tr("候选结果已另存为私有模板"));
      setRevision((value) => value + 1);
      router.push(`/console/templates/${saved.id}`);
    } catch (problem) {
      setSubmitError(problem);
    } finally {
      setBusy(false);
    }
  }
  /** cards 输出明确归属的模板操作，公共模板先复制后编辑。 */
  function cards(items: Template[], isPublic = false) {
    if (loading) return <Spin />;
    if (!items.length)
      return (
        <EmptyData
          text={
            isPublic ? tr("平台尚未发布模板") : tr("还没有私有模板，可以手工创建或使用 AI 设计。")
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
  /** openAI 打开现有创作流程，横幅与标题操作使用相同的配置和校验。 */
  function openAI() {
    openAssistant();
  }
  return (
    <section className="studio-page studio-templates" aria-label={tr("装修与模板")}>
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
              <Button type="primary" icon={<RobotOutlined />} onClick={openAI}>
                {tr("AI 创建我的模板 ")}
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
                      <h2>{tr("描述想法，生成你的专属模板")}</h2>
                      <p>
                        {tr("上传参考图片，边聊边预览；发送、接收和附件始终使用官方聊天核心。")}
                      </p>
                    </div>
                    <Button onClick={openAI}>
                      {tr("开始创作 ")}
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
          ...(manages
            ? [{ key: "conversations", label: tr("AI 对话记录"), children: <AssistantHistory /> }]
            : []),
          {
            key: "jobs",
            label: tr("AI 创作记录"),
            children: (
              <div className="table-card">
                <ErrorNotice error={aiJobs.error} retry={aiJobs.reload} />
                <Table
                  rowKey="id"
                  dataSource={aiJobs.items}
                  loading={aiJobs.loading}
                  locale={{ emptyText: <EmptyData text={tr("还没有 AI 创作任务")} /> }}
                  columns={[
                    { title: tr("作品名称"), dataIndex: "name" },
                    {
                      title: tr("状态"),
                      dataIndex: "status",
                      render: (value) => <Tag>{String(value)}</Tag>,
                    },
                    {
                      title: tr("结果说明"),
                      render: (_, row) =>
                        row.error_message
                          ? String(row.error_message)
                          : row.status === "succeeded"
                            ? row.applied === false
                              ? tr("候选保留，未覆盖新草稿")
                              : tr("已写入私有模板")
                            : "—",
                    },
                    {
                      title: tr("操作"),
                      render: (_, row) => (
                        <Button
                          type="link"
                          onClick={() => {
                            setJob(row);
                            setSubmitError(null);
                            setAiOpen(true);
                          }}
                        >
                          {tr("查看任务 ")}
                        </Button>
                      ),
                    },
                  ]}
                />
              </div>
            ),
          },
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
      <Drawer
        rootClassName="studio-drawer"
        title={tr("创建私有模板")}
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        width={470}
      >
        <ErrorNotice error={submitError} />
        <Form form={form} layout="vertical" onFinish={createTemplate}>
          <Form.Item
            name="name"
            label={tr("模板名称")}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={100} />
          </Form.Item>
          <Form.Item name="render_mode" label={tr("装修方式")}>
            <Select
              options={[
                { label: tr("基础设置与可视化组件"), value: "schema" },
                { label: tr("完整 HTML CSS JavaScript"), value: "code" },
              ]}
            />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>
            {tr("创建并编辑 ")}
          </Button>
        </Form>
      </Drawer>
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
      <Drawer
        rootClassName="studio-drawer"
        title={tr("原 AI 创作任务记录")}
        open={aiOpen}
        onClose={() => setAiOpen(false)}
        width={540}
      >
        <Alert
          type="info"
          showIcon
          title={tr("原任务记录仅供查看")}
          description={tr(
            "新的模板创作已迁移到全局 AI 助手，使用当前租户自己的模型配置，并提供持续对话和真实模板预览。",
          )}
          action={
            <Button
              onClick={() => {
                setAiOpen(false);
                openAssistant();
              }}
            >
              {tr("打开 AI 助手 ")}
            </Button>
          }
        />
        <ErrorNotice error={submitError} />
        {job && (
          <Card size="small" style={{ marginTop: 24 }} title={tr("创作任务")}>
            <p>
              {tr("状态：")}
              <Tag>{String(job.status || tr("等待处理"))}</Tag>
            </p>
            {!!(job.error_message || job.error) && (
              <Alert type="error" title={String(job.error_message || job.error)} />
            )}
            {job.status === "succeeded" && job.applied === false && (
              <Alert
                type="warning"
                showIcon
                title={tr("生成完成，但当前草稿已修改。候选结果已保留，没有覆盖你的编辑。")}
                action={
                  <Button size="small" onClick={() => void saveCandidate()} loading={busy}>
                    {tr("另存候选模板 ")}
                  </Button>
                }
              />
            )}
            <p className="form-hint">
              {tr("任务编号 ")}
              {job.id}
              {job.billing_status === "uncertain" ? tr(" \u00B7 供应商用量待核对") : ""}
            </p>
            {job.status === "succeeded" && job.template_id ? (
              <Button type="primary" href={`/console/templates/${job.template_id}`}>
                {tr("打开生成的私有模板 ")}
              </Button>
            ) : (
              !["failed", "cancelled"].includes(String(job.status)) && (
                <Popconfirm title={tr("取消这次创作？")} onConfirm={cancelJob}>
                  <Button danger>{tr("取消任务")}</Button>
                </Popconfirm>
              )
            )}
          </Card>
        )}
      </Drawer>
    </section>
  );
}
