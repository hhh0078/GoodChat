/** 文件作用说明：校验并导入完整 HTML/CSS/JS 模板 ZIP，服务端为其创建独立私有资源。 */
"use client";
import { ErrorNotice } from "@/components/page-state";
import { useTranslator } from "@/i18n/use-translator";
import { startFileDownload, exportFilename } from "@/lib/csv-download";
import { preferenceHeaders } from "@/i18n/runtime";
import { api } from "@/lib/api";
import { ImportOutlined } from "@ant-design/icons";
import { App, Button, Form, Input, Modal, Space, Collapse } from "antd";
import { useRef, useState } from "react";
/** TemplateImport 导入完整静态文件包，不覆盖同名作品或自动发布入口。 */
export function TemplateImport({
  tenantId,
  onImported,
}: {
  tenantId: string;
  onImported: () => void;
}) {
  const tr = useTranslator();
  const [guide, setGuide] = useState<{ prompt: string; version: string } | null>(null);
  const [downloading, setDownloading] = useState(false);
  const [open, setOpen] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [form] = Form.useForm();
  const input = useRef<HTMLInputElement | null>(null);
  const { message } = App.useApp();
  /** importTemplate 只有后端检查文件、引用和核心 JS 成功后才显示导入完成。 */
  async function importTemplate(values: { name: string }) {
    setError(null);
    if (!file || !file.size || file.size > 10 * 1024 * 1024) {
      setError(new Error(tr("请选择不超过 10MiB 的非空 ZIP 模板文件")));
      return;
    }
    setBusy(true);
    try {
      const data = new FormData();
      data.set("file", file);
      data.set("name", values.name);
      await api("/tenant/templates/import", { method: "POST", tenantId, body: data });
      setOpen(false);
      setFile(null);
      onImported();
      message.success(tr("模板文件已校验并导入"));
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  /** loadGuide 从服务端读取与ZIP一致的提示词，失败保留导入入口供已有模板使用。 */
  async function loadGuide() {
    try {
      setGuide(
        await api<{ prompt: string; version: string }>("/tenant/code-packages/example/prompt", {
          tenantId,
        }),
      );
    } catch (problem) {
      setError(problem);
    }
  }
  /** downloadStarter 下载受认证的完整基础包，不将请求发起视为文件已保存。 */
  async function downloadStarter() {
    setDownloading(true);
    setError(null);
    try {
      const response = await fetch("/api/v1/tenant/code-packages/example", {
        credentials: "same-origin",
        cache: "no-store",
        headers: { ...preferenceHeaders(), "X-Tenant-ID": tenantId },
        signal: AbortSignal.timeout(30000),
      });
      if (!response.ok || !response.headers.get("Content-Type")?.includes("application/zip"))
        throw new Error(tr("示例资源包下载失败"));
      const blob = await response.blob();
      if (!blob.size) throw new Error(tr("示例资源包没有返回有效ZIP文件"));
      startFileDownload(
        blob,
        exportFilename(response.headers.get("Content-Disposition"), "goodchat-template-1.0.0.zip"),
      );
    } catch (problem) {
      setError(problem);
    } finally {
      setDownloading(false);
    }
  }
  /** copyPrompt 仅复制真实基础包附带的AI编辑规范。 */
  async function copyPrompt() {
    if (!guide) return;
    try {
      await navigator.clipboard.writeText(guide.prompt);
      message.success(tr("已复制"));
    } catch (problem) {
      setError(problem);
    }
  }
  return (
    <>
      <Button
        icon={<ImportOutlined />}
        onClick={() => {
          form.resetFields();
          setFile(null);
          setError(null);
          setOpen(true);
          void loadGuide();
        }}
      >
        {tr("导入模板 ")}
      </Button>
      <Modal
        title={tr("导入完整静态模板")}
        width={760}
        open={open}
        onCancel={() => {
          if (!busy) setOpen(false);
        }}
        footer={null}
      >
        <ErrorNotice error={error} />
        <section className="template-import-guide" aria-label={tr("使用步骤")}>
          <h3>{tr("用 AI 制作你的聊天页面")}</h3>
          <ol>
            <li>{tr("复制提示词，下载基础模板 ZIP。")}</li>
            <li>{tr("打开 AI 工具，上传 ZIP，粘贴提示词并描述想要的效果。")}</li>
            <li>{tr("等待 AI 返回带新版本号的完整 ZIP，再到这里上传。")}</li>
            <li>{tr("导入后预览并测试；不满意就让 AI 继续修改，再上传新版本。")}</li>
          </ol>
          <Space wrap>
            <Button onClick={() => void copyPrompt()} disabled={!guide}>
              {tr("复制 AI 提示词")}
            </Button>
            <Button onClick={() => void downloadStarter()} loading={downloading}>
              {tr("下载基础模板 ZIP")}
              {guide ? ` · v${guide.version}` : ""}
            </Button>
            {!guide && (
              <Button type="text" onClick={() => void loadGuide()}>
                {tr("重试")}
              </Button>
            )}
          </Space>
          <Collapse
            ghost
            items={[
              {
                key: "prompt",
                label: tr("查看提示词与版本要求"),
                children: (
                  <Input.TextArea
                    value={guide?.prompt || ""}
                    readOnly
                    autoSize={{ minRows: 5, maxRows: 10 }}
                    aria-label={tr("AI 编辑提示词")}
                  />
                ),
              },
            ]}
          />
          <p className="form-hint">
            {tr(
              "每轮修改都递增版本号并交付 ZIP。导入会创建独立模板，预览满意后再发布并绑定接入链接。",
            )}
          </p>
        </section>
        <Form form={form} layout="vertical" onFinish={importTemplate}>
          <Form.Item
            name="name"
            label={tr("导入后的模板名称")}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={100} />
          </Form.Item>
          <Form.Item label={tr("模板 ZIP")}>
            <Button
              icon={<ImportOutlined />}
              onClick={() => input.current?.click()}
              disabled={busy}
            >
              {tr("选择模板文件 ")}
            </Button>
            <span style={{ marginLeft: 12 }}>{file?.name || tr("尚未选择")}</span>
            <input
              ref={input}
              type="file"
              accept=".zip,application/zip"
              hidden
              style={{ display: "none" }}
              aria-label={tr("选择模板 ZIP 文件")}
              onChange={(event) => {
                setFile(event.target.files?.[0] || null);
                event.target.value = "";
              }}
            />
          </Form.Item>
          <p className="form-hint">
            {tr(
              "导入 HTML、CSS、JS 和 static 资源，无需二次编译。ZIP 最多 10MiB，核心 JS 和文件引用必须通过校验。 ",
            )}
          </p>
          <Button type="primary" htmlType="submit" loading={busy}>
            {tr("校验并导入 ")}
          </Button>
        </Form>
      </Modal>
    </>
  );
}
