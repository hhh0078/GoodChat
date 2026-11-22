/** 文件作用说明：平台管理员维护真实SMTP配置，密码仅提交不回显，设置与测试发送分开。 */
"use client";
import { showRequestError } from "@/lib/request-error";
import { api } from "@/lib/api";
import { ErrorNotice } from "@/components/page-state";
import { useTranslator } from "@/i18n/use-translator";
import { Alert, App, Button, Form, Input } from "antd";
import { useEffect, useState } from "react";
import { useLocale } from "next-intl";
/** SMTPSettings 读取脱敏状态，保存后后续邮件立即使用新配置。 */
export function SMTPSettings() {
  const tr = useTranslator();
  const locale = useLocale();
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const c = new AbortController();
    setLoaded(false);
    api<Record<string, unknown>>("/platform/mail-settings", { signal: c.signal })
      .then((v) => {
        if (!c.signal.aborted) {
          form.setFieldsValue({ ...v, password: "" });
          setLoaded(true);
        }
      })
      .catch((e) => {
        if (!c.signal.aborted) setError(e);
      });
    return () => c.abort();
  }, [form, locale, revision]);
  /** save 仅提交管理员填写的配置，密码空值保留已有密钥。 */
  async function save(v: Record<string, unknown>) {
    setBusy(true);
    setError(null);
    try {
      await api("/platform/mail-settings", { method: "PUT", bodyJSON: v });
      message.success(tr("邮件服务配置已保存，请发送本人测试邮件核验"));
      setRevision((n) => n + 1);
    } catch (e) {
      setError(e);
      showRequestError(e);
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <ErrorNotice error={error} />
      <Alert
        type="info"
        title={tr("生产发信强制TLS。密码加密保存，留空保留原密码；更改前保存恢复副本。")}
      />
      <Form form={form} layout="vertical" onFinish={save} style={{ maxWidth: 650 }}>
        <Form.Item name="host" label={tr("SMTP服务器")} rules={[{ required: true }]}>
          <Input autoComplete="off" />
        </Form.Item>
        <Form.Item name="port" label={tr("SMTP端口")} rules={[{ required: true }]}>
          <Input inputMode="numeric" />
        </Form.Item>
        <Form.Item name="from" label={tr("发件邮箱")} rules={[{ required: true, type: "email" }]}>
          <Input />
        </Form.Item>
        <Form.Item name="username" label={tr("SMTP用户名")}>
          <Input autoComplete="off" />
        </Form.Item>
        <Form.Item name="password" label={tr("SMTP密码（留空保留）")}>
          <Input.Password autoComplete="new-password" />
        </Form.Item>
        <Button type="primary" htmlType="submit" loading={busy} disabled={!loaded}>
          {tr("保存设置")}
        </Button>
      </Form>
    </>
  );
}
