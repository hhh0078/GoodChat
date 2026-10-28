/** 文件作用说明：管理租户私有与平台模板、创建装修页面，并真实提交和查询 AI 创作任务。 */
"use client";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import {
  Alert,
  App,
  Button,
  Card,
  Drawer,
  Descriptions,
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
import { PlusOutlined, RobotOutlined } from "@ant-design/icons";
import { starterCodeDesign } from "./starter-code";
import { CodePackages } from "./code-packages";
import { TemplatePreview } from "./template-preview";
import { api, errorMessage } from "@/lib/api";
import { aiSubmissionState, type AIConfiguration } from "@/lib/ai-configuration";
import { useSession } from "@/features/auth/session";
import { useResource } from "@/lib/use-resource";
import type { Resource } from "@/lib/types";
import { EmptyData, ErrorNotice, PageTitle } from "@/components/page-state";

export type Design = {
  schema_version?: number;
  theme?: { primary_color?: string; background?: string; text_color?: string; radius?: number };
  brand?: { title?: string; subtitle?: string; logo_url?: string };
  blocks?: { id: string; type: string; title?: string; content?: string; items?: unknown[] }[];
  custom_css?: string;
  html?: string;
  css?: string;
  javascript?: string;
  package_id?: string;
};
export type Template = Resource & {
  render_mode: "schema" | "code";
  design: Design;
  version: number;
};
/** starterDesign 返回可编辑的本地初始设计，仅在用户明确保存后写入后端。 */
export function starterDesign(): Design {
  return {
    schema_version: 1,
    theme: { primary_color: "#23684e", background: "#f4f6f2", text_color: "#21342c", radius: 14 },
    brand: { title: "在线客服", subtitle: "有问题，随时聊聊", logo_url: "" },
    blocks: [
      {
        id: "welcome",
        type: "welcome",
        title: "欢迎咨询",
        content: "请告诉我们你需要什么帮助。",
        items: [],
      },
      { id: "chat", type: "chat", title: "在线咨询", content: "", items: [] },
    ],
    custom_css: "",
  };
}

/** TemplatesPage 展示真实模板与页面，并管理可追踪的 AI 创建过程。 */
export function TemplatesPage() {
  const { tenantId } = useSession();
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
  const [busy, setBusy] = useState(false);
  const [submitError, setSubmitError] = useState<unknown>(null);
  const [job, setJob] = useState<Resource | null>(null);
  const [aiConfiguration, setAiConfiguration] = useState<AIConfiguration | null>(null);
  const [aiConfigurationError, setAiConfigurationError] = useState<unknown>(null);
  const [aiConfigurationLoading, setAiConfigurationLoading] = useState(true);
  const [aiRevision, setAiRevision] = useState(0);
  const aiSubmission = aiSubmissionState(aiConfiguration);
  const [form] = Form.useForm();
  const [pageForm] = Form.useForm();
  const [aiForm] = Form.useForm();
  const pages = useResource<Resource>("/tenant/pages");
  const aiJobs = useResource<Resource>("/tenant/ai-design-jobs");
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    api<{ items: Template[]; platform_templates: Template[] }>("/tenant/templates", {
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
    const controller = new AbortController();
    setAiConfiguration(null);
    setAiConfigurationError(null);
    setAiConfigurationLoading(true);
    api<AIConfiguration>("/tenant/ai-configuration", { tenantId, signal: controller.signal })
      .then((data) => {
        if (!controller.signal.aborted) setAiConfiguration(data);
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setAiConfigurationError(problem);
      })
      .finally(() => {
        if (!controller.signal.aborted) setAiConfigurationLoading(false);
      });
    return () => controller.abort();
  }, [tenantId, aiOpen, aiRevision]);
  /** refreshAIConfiguration 重新读取服务端次数，禁止在刷新期间依据旧次数再次提交。 */
  function refreshAIConfiguration() {
    setAiConfigurationLoading(true);
    setAiRevision((value) => value + 1);
  }
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
      message.success("已保存为本租户模板");
      setRevision((value) => value + 1);
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  /** generate 提交真实模型任务，模型未配置时保留后端错误。 */
  async function generate(values: Record<string, string>) {
    if (aiConfigurationLoading || !aiSubmission.allowed) {
      setSubmitError(new Error(aiSubmission.message));
      return;
    }
    setBusy(true);
    setSubmitError(null);
    try {
      const task = await api<Resource>("/tenant/ai-design-jobs", {
        method: "POST",
        tenantId,
        bodyJSON: values,
        idempotent: true,
      });
      setJob(task);
      aiJobs.reload();
      setRevision((value) => value + 1);
    } catch (problem) {
      setSubmitError(problem);
    } finally {
      refreshAIConfiguration();
      setBusy(false);
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
      message.success("取消请求已处理");
    } catch (problem) {
      message.error(errorMessage(problem));
    } finally {
      refreshAIConfiguration();
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
          name: `${String(job.name || "AI设计").slice(0, 65)} 候选`,
          render_mode: job.render_mode,
          design: job.result,
        },
        idempotent: true,
      });
      message.success("候选结果已另存为私有模板");
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
          text={isPublic ? "平台尚未发布模板" : "还没有私有模板，可以手工创建或使用 AI 设计。"}
        />
      );
    return (
      <div className="templates-grid">
        {items.map((item) => (
          <Card key={item.id} size="small" className="studio-template-object">
            <TemplatePreview template={item} />
            <div className="template-info">
              <Space>
                <strong>{item.name}</strong>
                <Tag>{item.render_mode === "code" ? "代码模板" : "可视化模板"}</Tag>
              </Space>
              <p className="muted" style={{ fontSize: 12 }}>
                {" "}
                {isPublic ? "平台模板 · 复制后归本租户" : "本租户私有 · 仅授权成员可用"}
              </p>
              <Space wrap>
                {!isPublic && (
                  <Button size="small" href={`/console/templates/${item.id}`}>
                    编辑模板
                  </Button>
                )}
                <Button size="small" onClick={() => void duplicate(item, isPublic)}>
                  {isPublic ? "添加到我的模板" : "复制"}
                </Button>
                {!isPublic && (
                  <Button
                    size="small"
                    disabled={item.status !== undefined && item.status !== "ready"}
                    onClick={() => {
                      pageForm.resetFields();
                      pageForm.setFieldsValue({ name: `${item.name} 页面` });
                      setSubmitError(null);
                      setPageTemplate(item);
                    }}
                  >
                    创建页面
                  </Button>
                )}
              </Space>
            </div>
          </Card>
        ))}
      </div>
    );
  }
  return (
    <section className="studio-page studio-templates" aria-label="装修与模板">
      <PageTitle
        title="装修与模板"
        description="模板归本租户，页面独立发布。AI 结果需要预览和确认后才能上线。"
        action={
          <Space>
            <Button
              icon={<PlusOutlined />}
              onClick={() => {
                setSubmitError(null);
                form.resetFields();
                form.setFieldsValue({ render_mode: "schema" });
                setCreateOpen(true);
              }}
            >
              创建模板
            </Button>
            <Button
              type="primary"
              icon={<RobotOutlined />}
              onClick={() => {
                setSubmitError(null);
                aiForm.setFieldsValue({ render_mode: "schema" });
                setAiOpen(true);
              }}
            >
              AI 创建我的模板
            </Button>
          </Space>
        }
      />
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      <Tabs
        className="studio-tabs"
        items={[
          { key: "mine", label: "我的模板", children: cards(templates) },
          { key: "platform", label: "平台模板", children: cards(publicTemplates, true) },
          { key: "resources", label: "私有资源包", children: <CodePackages key={tenantId} /> },
          {
            key: "jobs",
            label: "AI 创作记录",
            children: (
              <div className="table-card">
                <ErrorNotice error={aiJobs.error} retry={aiJobs.reload} />
                <Table
                  rowKey="id"
                  dataSource={aiJobs.items}
                  loading={aiJobs.loading}
                  locale={{ emptyText: <EmptyData text="还没有 AI 创作任务" /> }}
                  columns={[
                    { title: "作品名称", dataIndex: "name" },
                    {
                      title: "状态",
                      dataIndex: "status",
                      render: (value) => <Tag>{String(value)}</Tag>,
                    },
                    {
                      title: "结果说明",
                      render: (_, row) =>
                        row.error_message
                          ? String(row.error_message)
                          : row.status === "succeeded"
                            ? row.applied === false
                              ? "候选保留，未覆盖新草稿"
                              : "已写入私有模板"
                            : "—",
                    },
                    {
                      title: "操作",
                      render: (_, row) => (
                        <Button
                          type="link"
                          onClick={() => {
                            setJob(row);
                            setSubmitError(null);
                            setAiOpen(true);
                          }}
                        >
                          查看任务
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
            label: "装修页面与发布",
            children: (
              <div className="table-card">
                <ErrorNotice error={pages.error} retry={pages.reload} />
                <Table
                  rowKey="id"
                  dataSource={pages.items}
                  loading={pages.loading}
                  locale={{
                    emptyText: <EmptyData text="从我的模板创建页面后，便可编辑并发布到链接。" />,
                  }}
                  columns={[
                    { title: "页面名称", dataIndex: "name" },
                    { title: "版本", dataIndex: "version" },
                    {
                      title: "发布状态",
                      dataIndex: "current_release_id",
                      render: (value) => (
                        <Tag color={value ? "green" : "default"}>
                          {value ? "已有发布" : "未发布"}
                        </Tag>
                      ),
                    },
                    {
                      title: "操作",
                      render: (_, row) => (
                        <Button type="link" href={`/console/pages/${row.id}`}>
                          编辑与发布
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
      <Drawer
        rootClassName="studio-drawer"
        title="创建私有模板"
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        width={470}
      >
        <ErrorNotice error={submitError} />
        <Form form={form} layout="vertical" onFinish={createTemplate}>
          <Form.Item name="name" label="模板名称" rules={[{ required: true, whitespace: true }]}>
            <Input maxLength={100} />
          </Form.Item>
          <Form.Item name="render_mode" label="装修方式">
            <Select
              options={[
                { label: "基础设置与可视化组件", value: "schema" },
                { label: "完整 HTML CSS JavaScript", value: "code" },
              ]}
            />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>
            创建并编辑
          </Button>
        </Form>
      </Drawer>
      <Modal
        rootClassName="studio-dialog"
        title="从模板创建独立页面"
        open={!!pageTemplate}
        onCancel={() => setPageTemplate(null)}
        footer={null}
      >
        <ErrorNotice error={submitError} />
        <Form form={pageForm} layout="vertical" onFinish={createPage}>
          <Form.Item name="name" label="页面名称" rules={[{ required: true, whitespace: true }]}>
            <Input maxLength={100} />
          </Form.Item>
          <p className="muted">页面是模板的独立副本，后续修改不会覆盖原模板。</p>
          <Button type="primary" htmlType="submit" loading={busy}>
            创建页面
          </Button>
        </Form>
      </Modal>
      <Drawer
        rootClassName="studio-drawer"
        title="AI 创建我的模板"
        open={aiOpen}
        onClose={() => setAiOpen(false)}
        width={540}
      >
        <Alert
          type={!aiConfigurationLoading && !aiSubmission.allowed ? "warning" : "info"}
          showIcon
          title={
            aiConfigurationLoading ? "正在读取 AI 装修配置与当日提交次数…" : aiSubmission.message
          }
          style={{ marginBottom: 20 }}
        />
        <ErrorNotice error={aiConfigurationError} retry={refreshAIConfiguration} />
        <Descriptions
          size="small"
          bordered
          column={3}
          items={[
            {
              key: "daily",
              label: "当日可用",
              children: `${aiConfiguration?.daily_limit ?? "—"} 次`,
            },
            { key: "used", label: "已用", children: `${aiConfiguration?.used_today ?? "—"} 次` },
            {
              key: "remaining",
              label: "剩余",
              children: `${aiConfiguration?.remaining_today ?? "—"} 次`,
            },
          ]}
        />
        <p className="form-hint">
          按租户时区 {aiConfiguration?.timezone || "—"}{" "}
          的自然日统计任务提交次数，失败或取消不退次数。
          <Button
            type="link"
            size="small"
            loading={aiConfigurationLoading}
            onClick={refreshAIConfiguration}
          >
            刷新次数
          </Button>
        </p>
        <ErrorNotice error={submitError} />
        <Form form={aiForm} layout="vertical" onFinish={generate}>
          <Form.Item name="name" label="模板名称" rules={[{ required: true, whitespace: true }]}>
            <Input maxLength={100} />
          </Form.Item>
          <Form.Item name="render_mode" label="结果类型">
            <Select
              options={[
                { label: "可继续编辑的组件页面", value: "schema" },
                { label: "HTML CSS JavaScript 代码页面", value: "code" },
              ]}
            />
          </Form.Item>
          <Form.Item
            name="prompt"
            label="你希望页面是什么样子"
            rules={[
              {
                required: true,
                whitespace: true,
                min: 10,
                message: "请至少填写 10 个字的设计描述",
              },
            ]}
          >
            <Input.TextArea
              rows={6}
              maxLength={4000}
              showCount
              placeholder="例如：为售后团队设计一个简洁温暖的页面，自然绿色，顶部介绍服务时间，下方展示常见问题和聊天。"
            />
          </Form.Item>
          <Button
            type="primary"
            htmlType="submit"
            loading={busy}
            disabled={
              aiConfigurationLoading ||
              !aiSubmission.allowed ||
              (!!job && !["failed", "succeeded", "cancelled"].includes(String(job.status)))
            }
          >
            开始创作
          </Button>
        </Form>
        {job && (
          <Card size="small" style={{ marginTop: 24 }} title="创作任务">
            <p>
              状态：<Tag>{String(job.status || "等待处理")}</Tag>
            </p>
            {!!(job.error_message || job.error) && (
              <Alert type="error" title={String(job.error_message || job.error)} />
            )}
            {job.status === "succeeded" && job.applied === false && (
              <Alert
                type="warning"
                showIcon
                title="生成完成，但当前草稿已修改。候选结果已保留，没有覆盖你的编辑。"
                action={
                  <Button size="small" onClick={() => void saveCandidate()} loading={busy}>
                    另存候选模板
                  </Button>
                }
              />
            )}
            <p className="form-hint">
              任务编号 {job.id}
              {job.billing_status === "uncertain" ? " · 供应商用量待核对" : ""}
            </p>
            {job.status === "succeeded" && job.template_id ? (
              <Button type="primary" href={`/console/templates/${job.template_id}`}>
                打开生成的私有模板
              </Button>
            ) : (
              !["failed", "cancelled"].includes(String(job.status)) && (
                <Popconfirm title="取消这次创作？" onConfirm={cancelJob}>
                  <Button danger>取消任务</Button>
                </Popconfirm>
              )
            )}
          </Card>
        )}
      </Drawer>
    </section>
  );
}
