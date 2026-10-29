/** 文件作用说明：提供模板二创、完整 ZIP 下载、引用检查和有边界的删除操作。 */
"use client";
import { useState } from "react";
import { App, Alert, Button, Form, Input, Modal, Popconfirm, Select, Space, Spin, Tag } from "antd";
import { api, ApiError, errorMessage } from "@/lib/api";
import { exportFilename, startFileDownload } from "@/lib/csv-download";
import { useSession } from "@/features/auth/session";
import { useAssistantLauncher } from "./launcher";
import type { Template } from "@/features/decoration/templates";

type TemplateUsage = {
  id: string;
  links: { id: string; name: string; status: string; binding_mode: string; page_id: string }[];
  pages: { id: string; name: string; current_release_id?: string }[];
  active_jobs: { id: string; status: string; source?: string }[];
  can_delete: boolean;
};

/** downloadTemplate 请求当前租户完整静态 ZIP，拒绝把 JSON 错误或空响应保存为文件。 */
export async function downloadTemplate(id: string, tenantId: string): Promise<void> {
  const response = await fetch(`/api/v1/tenant/templates/${encodeURIComponent(id)}/export`, {
    credentials: "same-origin",
    cache: "no-store",
    headers: { "X-Tenant-ID": tenantId },
    signal: AbortSignal.timeout(30000),
  });
  if (!response.ok) {
    const result = await response.json().catch(() => null);
    throw new ApiError(
      result?.message || "模板导出失败",
      response.status,
      result?.code || "TEMPLATE_EXPORT_FAILED",
      result?.request_id,
    );
  }
  const blob = await response.blob();
  if (!response.headers.get("Content-Type")?.includes("application/zip") || !blob.size)
    throw new Error("服务端没有返回有效的模板 ZIP");
  startFileDownload(
    blob,
    exportFilename(response.headers.get("Content-Disposition"), "goodchat-template.zip"),
  );
}

/** TemplateActions 复制后发起 AI 二创，删除前展示服务端的链接与任务引用。 */
export function TemplateActions({
  template,
  isPublic,
  onChanged,
}: {
  template: Template;
  isPublic: boolean;
  onChanged: () => void;
}) {
  const { tenantId, current } = useSession();
  const { openAssistant } = useAssistantLauncher();
  const [busy, setBusy] = useState("");
  const [usageOpen, setUsageOpen] = useState(false);
  const [usage, setUsage] = useState<TemplateUsage | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [platformOpen, setPlatformOpen] = useState(false);
  const [platformError, setPlatformError] = useState<unknown>(null);
  const [platformForm] = Form.useForm();
  const { message } = App.useApp();
  const member = current.memberships.find((item) => item.tenant_id === tenantId);
  const manages = member?.role === "owner" || member?.role === "admin";
  if (!manages) return null;

  /** copy 为二创或公共模板下载创建独立副本，保留源模板及已发布入口。 */
  async function copy(): Promise<Template> {
    const created = await api<Template>(`/tenant/templates/${template.id}/duplicate`, {
      tenantId,
      method: "POST",
      bodyJSON: {},
      idempotent: true,
    });
    onChanged();
    return created;
  }

  /** remix 由助手服务原子复制并升级原模板，首次发送描述才调用模型。 */
  function remix() {
    openAssistant({
      templateId: template.id,
      templateName: template.name,
      prompt: "请在当前模板基础上调整设计，保留完整聊天能力。",
    });
  }

  /** openPlatformCopy 先确认名称和可见范围，默认隐藏，不会因点击入口自动公开作品。 */
  function openPlatformCopy() {
    setPlatformError(null);
    platformForm.resetFields();
    platformForm.setFieldsValue({ name: template.name || "平台客服模板", status: "hidden" });
    setPlatformOpen(true);
  }

  /** savePlatformCopy 按管理员明确选择复制成平台独立资源包，不引用租户私有文件。 */
  async function savePlatformCopy(values: { name: string; status: "published" | "hidden" }) {
    setBusy("platform");
    setPlatformError(null);
    try {
      await api("/platform/templates/from-private", {
        tenantId,
        method: "POST",
        bodyJSON: {
          source_template_id: template.id,
          name: values.name.trim(),
          status: values.status,
        },
        idempotent: true,
      });
      setPlatformOpen(false);
      onChanged();
      message.success(
        values.status === "published" ? "已保存并上架平台模板" : "已保存平台模板，当前为隐藏",
      );
    } catch (problem) {
      setPlatformError(problem);
    } finally {
      setBusy("");
    }
  }

  /** download 公共模板先明确复制到本租户，再导出同一份完整静态文件。 */
  async function download() {
    setBusy("download");
    setError(null);
    try {
      const selected = isPublic ? await copy() : template;
      await downloadTemplate(selected.id, tenantId);
      message.success("模板 ZIP 下载已发起");
    } catch (problem) {
      message.error(errorMessage(problem));
    } finally {
      setBusy("");
    }
  }

  /** inspect 实时获取绑定引用，避免以列表缓存判断是否能删除。 */
  async function inspect() {
    setUsageOpen(true);
    setBusy("usage");
    setError(null);
    setUsage(null);
    try {
      setUsage(await api<TemplateUsage>(`/tenant/templates/${template.id}/usage`, { tenantId }));
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy("");
    }
  }

  /** remove 由服务端重新检查全部引用，只删除无链接绑定的模板及无引用资源。 */
  async function remove() {
    setBusy("delete");
    setError(null);
    try {
      await api(`/tenant/templates/${template.id}`, { tenantId, method: "DELETE" });
      setUsageOpen(false);
      onChanged();
      message.success("模板及无引用资源已删除");
    } catch (problem) {
      setError(problem);
      try {
        setUsage(await api<TemplateUsage>(`/tenant/templates/${template.id}/usage`, { tenantId }));
      } catch {
        /* 保留原始删除错误。 */
      }
    } finally {
      setBusy("");
    }
  }

  return (
    <>
      <Space wrap>
        <Button
          size="small"
          loading={busy === "remix"}
          disabled={!!busy && busy !== "remix"}
          onClick={() => void remix()}
        >
          AI 二创
        </Button>
        <Button
          size="small"
          loading={busy === "download"}
          disabled={!!busy && busy !== "download"}
          onClick={() => void download()}
        >
          {isPublic ? "复制并下载" : "下载"}
        </Button>
        {!isPublic && (
          <Button size="small" disabled={!!busy} onClick={() => void inspect()}>
            引用与删除
          </Button>
        )}
        {!isPublic && current.user.is_platform_admin && (
          <Button size="small" disabled={!!busy} onClick={openPlatformCopy}>
            保存为平台模板
          </Button>
        )}
      </Space>
      <Modal
        title="保存为平台模板"
        open={platformOpen}
        onCancel={() => {
          if (busy !== "platform") setPlatformOpen(false);
        }}
        footer={null}
      >
        <Alert
          type="info"
          showIcon
          title="将创建独立的平台模板"
          description="选择上架后，所有租户都能预览和复制此作品；私有原稿继续保留。"
          style={{ marginBottom: 20 }}
        />
        {!!platformError && <Alert type="error" showIcon title={errorMessage(platformError)} />}
        <Form
          form={platformForm}
          layout="vertical"
          onFinish={savePlatformCopy}
          disabled={busy === "platform"}
        >
          <Form.Item
            name="name"
            label="平台模板名称"
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={100} />
          </Form.Item>
          <Form.Item name="status" label="可见范围" rules={[{ required: true }]}>
            <Select
              options={[
                { value: "hidden", label: "隐藏，暂不上架" },
                { value: "published", label: "上架，所有租户可用" },
              ]}
            />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={busy === "platform"}>
            确认保存
          </Button>
        </Form>
      </Modal>
      <Modal
        title={`模板引用 · ${template.name || "未命名"}`}
        open={usageOpen}
        onCancel={() => setUsageOpen(false)}
        footer={null}
      >
        {!!error && <Alert type="error" showIcon title={errorMessage(error)} />}
        {busy === "usage" ? (
          <Spin />
        ) : usage ? (
          <>
            <p>
              <Tag>{usage.links.length} 个入口</Tag>
              <Tag>{usage.pages.length} 个页面</Tag>
              <Tag>{usage.active_jobs.length} 个生成任务</Tag>
            </p>
            {usage.links.length > 0 && (
              <>
                <Alert
                  type="warning"
                  showIcon
                  title="请先切换或解除这些入口的模板绑定"
                  description="停用入口仍可能引用旧版本，因此也需要处理。"
                />
                <ul className="template-usage-list">
                  {usage.links.map((link) => (
                    <li key={link.id}>
                      {link.name || link.id} · {link.status === "active" ? "启用" : "停用"} ·{" "}
                      {link.binding_mode === "PIN_RELEASE" ? "固定版本" : "跟随页面"}
                    </li>
                  ))}
                </ul>
              </>
            )}
            {usage.pages.length > 0 && (
              <>
                <p>关联的独立页面：</p>
                <ul className="template-usage-list">
                  {usage.pages.map((page) => (
                    <li key={page.id}>
                      <a href={`/console/pages/${page.id}`}>{page.name || page.id}</a>
                      {page.current_release_id ? " · 已发布" : " · 草稿"}
                    </li>
                  ))}
                </ul>
              </>
            )}
            {usage.active_jobs.length > 0 && (
              <Alert type="warning" title="仍有 AI 任务正在使用此模板，请先等待完成或取消任务。" />
            )}
            {usage.can_delete && (
              <Alert
                type="info"
                showIcon
                title="此模板当前没有入口或运行任务绑定"
                description="删除会同时清理对应的无绑定页面与无引用文件；其他模板和版本仍在使用的文件会保留。"
              />
            )}
            <Space style={{ marginTop: 20 }}>
              <Button onClick={() => void inspect()} disabled={!!busy}>
                刷新引用
              </Button>
              <Popconfirm
                title="删除这个模板及无引用资源？"
                description="此操作会清理对应无绑定页面及其历史发布。"
                okText="确认删除"
                cancelText="保留模板"
                onConfirm={remove}
                disabled={!usage.can_delete || !!busy}
              >
                <Button
                  danger
                  disabled={!usage.can_delete || (!!busy && busy !== "delete")}
                  loading={busy === "delete"}
                >
                  删除模板
                </Button>
              </Popconfirm>
            </Space>
          </>
        ) : (
          <Button onClick={() => void inspect()}>重新读取引用</Button>
        )}
      </Modal>
    </>
  );
}
