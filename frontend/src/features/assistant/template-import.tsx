/** 文件作用说明：校验并导入完整 HTML/CSS/JS 模板 ZIP，服务端为其创建独立私有资源。 */
"use client";
import { useRef, useState } from "react";
import { App, Button, Form, Input, Modal } from "antd";
import { ImportOutlined } from "@ant-design/icons";
import { api } from "@/lib/api";
import { ErrorNotice } from "@/components/page-state";

/** TemplateImport 导入完整静态文件包，不覆盖同名作品或自动发布入口。 */
export function TemplateImport({
  tenantId,
  onImported,
}: {
  tenantId: string;
  onImported: () => void;
}) {
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
      setError(new Error("请选择不超过 10MiB 的非空 ZIP 模板文件"));
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
      message.success("模板文件已校验并导入");
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
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
        }}
      >
        导入模板
      </Button>
      <Modal
        title="导入完整静态模板"
        open={open}
        onCancel={() => {
          if (!busy) setOpen(false);
        }}
        footer={null}
      >
        <ErrorNotice error={error} />
        <Form form={form} layout="vertical" onFinish={importTemplate}>
          <Form.Item
            name="name"
            label="导入后的模板名称"
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={100} />
          </Form.Item>
          <Form.Item label="模板 ZIP">
            <Button
              icon={<ImportOutlined />}
              onClick={() => input.current?.click()}
              disabled={busy}
            >
              选择模板文件
            </Button>
            <span style={{ marginLeft: 12 }}>{file?.name || "尚未选择"}</span>
            <input
              ref={input}
              type="file"
              accept=".zip,application/zip"
              hidden
              aria-label="选择模板 ZIP 文件"
              onChange={(event) => {
                setFile(event.target.files?.[0] || null);
                event.target.value = "";
              }}
            />
          </Form.Item>
          <p className="form-hint">
            导入 HTML、CSS、JS 和 static 资源，无需二次编译。ZIP 最多 10MiB，核心 JS
            和文件引用必须通过校验。
          </p>
          <Button type="primary" htmlType="submit" loading={busy}>
            校验并导入
          </Button>
        </Form>
      </Modal>
    </>
  );
}
