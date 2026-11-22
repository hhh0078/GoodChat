/** 文件作用说明：提供模板副本、完整 ZIP 下载、引用检查和有边界的删除操作。 */
"use client";
import { ErrorNotice } from "@/components/page-state";
import { showRequestError } from "@/lib/request-error";
import { useSession } from "@/features/auth/session";
import type { Template } from "@/features/decoration/templates";
import { translate as tr } from "@/i18n/runtime";
import { useTranslator } from "@/i18n/use-translator";
import { api, ApiError } from "@/lib/api";
import { exportFilename, startFileDownload } from "@/lib/csv-download";
import type { MenuProps } from "antd";
import {
  Alert,
  App,
  Button,
  Dropdown,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Spin,
  Tag,
} from "antd";
import { useState } from "react";
import { preferenceHeaders } from "../../i18n/runtime.ts";
type TemplateUsage = {
  id: string;
  links: {
    id: string;
    name: string;
    status: string;
    binding_mode: string;
    page_id: string;
  }[];
  pages: {
    id: string;
    name: string;
    current_release_id?: string;
  }[];
  can_delete: boolean;
};
/** downloadTemplate 请求当前租户完整静态 ZIP，拒绝把 JSON 错误或空响应保存为文件。 */
export async function downloadTemplate(id: string, tenantId: string): Promise<void> {
  const response = await fetch(`/api/v1/tenant/templates/${encodeURIComponent(id)}/export`, {
    credentials: "same-origin",
    cache: "no-store",
    headers: { ...preferenceHeaders(), "X-Tenant-ID": tenantId },
    signal: AbortSignal.timeout(30000),
  });
  if (!response.ok) {
    const result = await response.json().catch(() => null);
    throw new ApiError(
      result?.message || tr("模板导出失败"),
      response.status,
      result?.code || "TEMPLATE_EXPORT_FAILED",
      result?.request_id,
    );
  }
  const blob = await response.blob();
  if (!response.headers.get("Content-Type")?.includes("application/zip") || !blob.size)
    throw new Error(tr("服务端没有返回有效的模板 ZIP"));
  startFileDownload(
    blob,
    exportFilename(response.headers.get("Content-Disposition"), "goodchat-template.zip"),
  );
}
/** TemplateActions 下载完整ZIP供外部编辑，删除前展示服务端的链接与页面引用。 */
export function TemplateActions({
  template,
  isPublic,
  onChanged,
  extraActions = [],
}: {
  template: Template;
  isPublic: boolean;
  onChanged: () => void;
  extraActions?: MenuProps["items"];
}) {
  const tr = useTranslator();
  const { tenantId, current } = useSession();
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
  /** openPlatformCopy 先确认名称和可见范围，默认隐藏，不会因点击入口自动公开作品。 */
  function openPlatformCopy() {
    setPlatformError(null);
    platformForm.resetFields();
    platformForm.setFieldsValue({ name: template.name || tr("平台客服模板"), status: "hidden" });
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
        values.status === "published"
          ? tr("已保存并上架平台模板")
          : tr("已保存平台模板，当前为隐藏"),
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
      message.success(tr("模板 ZIP 下载已发起"));
    } catch (problem) {
      showRequestError(problem);
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
      message.success(tr("模板及无引用资源已删除"));
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
      <Button
        size="small"
        loading={busy === "download"}
        disabled={!!busy && busy !== "download"}
        onClick={() => void download()}
      >
        {isPublic ? tr("复制并下载") : tr("下载")}
      </Button>
      {!isPublic && (
        <Dropdown
          trigger={["click"]}
          menu={{
            items: [
              ...extraActions,
              { key: "usage", label: tr("引用与删除"), onClick: () => void inspect() },
              ...(current.user.is_platform_admin
                ? [{ key: "platform", label: tr("保存为平台模板"), onClick: openPlatformCopy }]
                : []),
            ],
          }}
        >
          <Button size="small" disabled={!!busy}>
            {tr("更多 ")}
          </Button>
        </Dropdown>
      )}
      <Modal
        title={tr("保存为平台模板")}
        open={platformOpen}
        onCancel={() => {
          if (busy !== "platform") setPlatformOpen(false);
        }}
        footer={null}
      >
        <Alert
          type="info"
          showIcon
          title={tr("将创建独立的平台模板")}
          description={tr("选择上架后，所有租户都能预览和复制此作品；私有原稿继续保留。")}
          style={{ marginBottom: 20 }}
        />
        {!!platformError && <ErrorNotice error={platformError} />}
        <Form
          form={platformForm}
          layout="vertical"
          onFinish={savePlatformCopy}
          disabled={busy === "platform"}
        >
          <Form.Item
            name="name"
            label={tr("平台模板名称")}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={100} />
          </Form.Item>
          <Form.Item name="status" label={tr("可见范围")} rules={[{ required: true }]}>
            <Select
              options={[
                { value: "hidden", label: tr("隐藏，暂不上架") },
                { value: "published", label: tr("上架，所有租户可用") },
              ]}
            />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={busy === "platform"}>
            {tr("确认保存 ")}
          </Button>
        </Form>
      </Modal>
      <Modal
        title={tr("模板引用 \u00B7 {p0}", { p0: String(template.name || tr("未命名")) })}
        open={usageOpen}
        onCancel={() => setUsageOpen(false)}
        footer={null}
      >
        {!!error && <ErrorNotice error={error} />}
        {busy === "usage" ? (
          <Spin />
        ) : usage ? (
          <>
            <p>
              <Tag>
                {usage.links.length}
                {tr(" 个入口")}
              </Tag>
              <Tag>
                {usage.pages.length}
                {tr(" 个页面")}
              </Tag>
            </p>
            {usage.links.length > 0 && (
              <>
                <Alert
                  type="warning"
                  showIcon
                  title={tr("请先切换或解除这些入口的模板绑定")}
                  description={tr("停用入口仍可能引用旧版本，因此也需要处理。")}
                />
                <ul className="template-usage-list">
                  {usage.links.map((link) => (
                    <li key={link.id}>
                      {link.name || link.id} · {link.status === "active" ? tr("启用") : tr("停用")}{" "}
                      · {link.binding_mode === "PIN_RELEASE" ? tr("固定版本") : tr("跟随页面")}
                    </li>
                  ))}
                </ul>
              </>
            )}
            {usage.pages.length > 0 && (
              <>
                <p>{tr("关联的独立页面：")}</p>
                <ul className="template-usage-list">
                  {usage.pages.map((page) => (
                    <li key={page.id}>
                      <a href={`/console/pages/${page.id}`}>{page.name || page.id}</a>
                      {page.current_release_id ? tr(" \u00B7 已发布") : tr(" \u00B7 草稿")}
                    </li>
                  ))}
                </ul>
              </>
            )}
            {usage.can_delete && (
              <Alert
                type="info"
                showIcon
                title={tr("此模板当前没有入口绑定")}
                description={tr(
                  "删除会同时清理对应的无绑定页面与无引用文件；其他模板和版本仍在使用的文件会保留。",
                )}
              />
            )}
            <Space style={{ marginTop: 20 }}>
              <Button onClick={() => void inspect()} disabled={!!busy}>
                {tr("刷新引用 ")}
              </Button>
              <Popconfirm
                title={tr("删除这个模板及无引用资源？")}
                description={tr("此操作会清理对应无绑定页面及其历史发布。")}
                okText={tr("确认删除")}
                cancelText={tr("保留模板")}
                onConfirm={remove}
                disabled={!usage.can_delete || !!busy}
              >
                <Button
                  danger
                  disabled={!usage.can_delete || (!!busy && busy !== "delete")}
                  loading={busy === "delete"}
                >
                  {tr("删除模板 ")}
                </Button>
              </Popconfirm>
            </Space>
          </>
        ) : (
          <Button onClick={() => void inspect()}>{tr("重新读取引用")}</Button>
        )}
      </Modal>
    </>
  );
}
