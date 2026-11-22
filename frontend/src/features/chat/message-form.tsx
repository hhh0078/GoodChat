/** 文件作用说明：在聊天气泡内填写服务端表单快照，以原消息编号和稳定提交编号发送结果。 */
"use client";
import { showRequestError } from "@/lib/request-error";
import { useEffect, useRef, useState } from "react";
import { App, Button, Form, Input, Tag } from "antd";
import { useTranslator } from "@/i18n/use-translator";
import type { ChatMessage } from "@/lib/types";
import type { Outgoing } from "./use-chat";
/** MessageForm 只允许访客本人提交一次，失败时保留输入，记录结果由实时消息持久层同步。 */
export function MessageForm({
  item,
  submission,
  canFill,
  onSend,
}: {
  item: ChatMessage;
  submission?: ChatMessage;
  canFill: boolean;
  onSend: (body: Outgoing) => Promise<unknown>;
}) {
  const tr = useTranslator();
  const { message } = App.useApp();
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const client = useRef<string | undefined>(undefined);
  const [form] = Form.useForm();
  useEffect(() => {
    if (submission)
      form.setFieldsValue(
        Object.fromEntries(
          (item.form_fields || []).map((field) => [
            field.id,
            submission.form_values?.[field.id] || "",
          ]),
        ),
      );
    if (submission || saved)
      form.setFields((item.form_fields || []).map((field) => ({ name: field.id, errors: [] })));
  }, [submission, saved, item.form_fields, form]);
  /** submit 保存真实输入，相同请求保留稳定编号供网络结果未知时重试。 */
  async function submit(values: Record<string, string>) {
    if (busy) return;
    setBusy(true);
    client.current ||= crypto.randomUUID();
    try {
      const result = await onSend({
        client_message_id: client.current,
        content: "",
        message_type: "form_result",
        audience: "public",
        form_request_id: item.id,
        form_values: values,
      });
      if (result !== false) setSaved(true);
    } catch (e) {
      showRequestError(e);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div
      className="message-form"
      onClick={(e) => e.stopPropagation()}
      onKeyDown={(e) => e.stopPropagation()}
    >
      {submission || saved ? <Tag color="green">{tr("已提交")}</Tag> : null}
      <Form
        name={`message-${item.id}`}
        form={form}
        layout="vertical"
        disabled={busy || !!submission || saved || !canFill}
        onFinish={(values) => void submit(values)}
      >
        {item.form_fields?.map((field) => (
          <Form.Item
            key={field.id}
            name={field.id}
            label={
              <span>
                {field.label} <small>{tr(field.required ? "必填" : "选填")}</small>
              </span>
            }
            initialValue={submission?.form_values?.[field.id] || ""}
            rules={[
              { required: field.required, whitespace: true, message: tr("请填写所有必填项") },
              { max: 2000 },
            ]}
          >
            {field.type === "textarea" ? (
              <Input.TextArea rows={3} maxLength={2000} />
            ) : (
              <Input maxLength={2000} />
            )}
          </Form.Item>
        ))}
        {canFill && !submission && !saved && (
          <Button type="primary" htmlType="submit" loading={busy}>
            {tr("保存")}
          </Button>
        )}
      </Form>
    </div>
  );
}
