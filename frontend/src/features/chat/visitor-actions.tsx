/** 文件作用说明：提交访客离线留言与已结束会话评价，复用独立访客授权且保持请求幂等。 */
"use client";
import { useRef, useState } from "react";
import { App, Button, Drawer, Form, Input, Rate, Space } from "antd";
import { api } from "@/lib/api";
import { ErrorNotice } from "@/components/page-state";
/** VisitorActions 让访客主动留言和评价，成功反馈只来自真实后端。 */
export function VisitorActions({
  entryKey,
  conversationId,
  token,
  status,
}: {
  entryKey: string;
  conversationId: string;
  token: string;
  status: string;
}) {
  const [mode, setMode] = useState<"lead" | "evaluation" | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [evaluated, setEvaluated] = useState(false);
  const requestID = useRef("");
  const [form] = Form.useForm();
  const { message } = App.useApp();
  /** submit 向服务端提交留言或评价，留言失败重试保留同一请求编号。 */
  async function submit(values: Record<string, unknown>) {
    setBusy(true);
    setError(null);
    try {
      if (mode === "lead") {
        if (!requestID.current) requestID.current = crypto.randomUUID();
        await api("/visitor/leads", {
          method: "POST",
          headers: { Authorization: `Bearer ${token}` },
          bodyJSON: {
            ...values,
            entry_key: entryKey,
            conversation_id: conversationId,
            client_request_id: requestID.current,
          },
        });
        requestID.current = "";
        message.success("留言已提交，团队会按所留联系方式跟进");
      } else {
        await api(`/conversations/${conversationId}/evaluation`, {
          method: "POST",
          headers: { Authorization: `Bearer ${token}` },
          bodyJSON: values,
        });
        setEvaluated(true);
        message.success("感谢你的真实反馈");
      }
      setMode(null);
      form.resetFields();
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <Space
        style={{
          justifyContent: "center",
          width: "100%",
          padding: "8px 16px",
          borderTop: "1px solid #e4e9df",
        }}
      >
        {status !== "closed" && (
          <Button
            type="link"
            size="small"
            onClick={() => {
              setError(null);
              form.resetFields();
              setMode("lead");
            }}
          >
            不方便等待？留下留言
          </Button>
        )}
        {status === "closed" && (
          <Button
            type="link"
            disabled={evaluated}
            onClick={() => {
              setError(null);
              form.resetFields();
              form.setFieldsValue({ score: 5 });
              setMode("evaluation");
            }}
          >
            {evaluated ? "感谢评价" : "评价本次服务"}
          </Button>
        )}
      </Space>
      <Drawer
        title={mode === "lead" ? "留言给接待团队" : "评价本次服务"}
        open={!!mode}
        onClose={() => setMode(null)}
        width={420}
        destroyOnHidden
      >
        <ErrorNotice error={error} />
        <Form form={form} layout="vertical" onFinish={submit}>
          {mode === "lead" ? (
            <>
              <Form.Item
                name="display_name"
                label="你的称呼"
                rules={[{ required: true, whitespace: true }]}
              >
                <Input maxLength={80} />
              </Form.Item>
              <Form.Item
                name="phone"
                label="联系电话"
                rules={[{ required: true, whitespace: true }]}
              >
                <Input maxLength={32} />
              </Form.Item>
              <Form.Item
                name="content"
                label="需要什么帮助"
                rules={[{ required: true, whitespace: true }]}
              >
                <Input.TextArea rows={5} maxLength={5000} />
              </Form.Item>
            </>
          ) : (
            <>
              <Form.Item name="score" label="服务评分" rules={[{ required: true }]}>
                <Rate />
              </Form.Item>
              <Form.Item name="comment" label="想对我们说的话（选填）">
                <Input.TextArea rows={4} maxLength={2000} />
              </Form.Item>
            </>
          )}
          <Button type="primary" htmlType="submit" loading={busy}>
            提交{mode === "lead" ? "留言" : "评价"}
          </Button>
        </Form>
      </Drawer>
    </>
  );
}
