/** 文件作用说明：配置复访历史客服优先窗口与普通客服跨接待历史可见性，策略以服务端持久配置为准。 */
"use client";
import { useEffect, useState } from "react";
import { App, Button, Form, InputNumber, Switch } from "antd";
import { api } from "@/lib/api";
import { useSession } from "@/features/auth/session";
import { ErrorNotice } from "@/components/page-state";
type RoutingSettings = { history_agent_minutes: number; share_previous_chats: boolean };

/** VisitorRoutingSettings 保存历史匹配和可见策略，不把浏览器缓存编号作为访问授权。 */
export function VisitorRoutingSettings() {
  const { tenantId } = useSession();
  const { message } = App.useApp();
  const [form] = Form.useForm<RoutingSettings>();
  const [loading, setLoading] = useState(true),
    [saving, setSaving] = useState(false),
    [revision, setRevision] = useState(0);
  const [error, setError] = useState<unknown>(null);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    form.resetFields();
    api<RoutingSettings>("/tenant/chat-settings", { tenantId, signal: controller.signal })
      .then((values) => {
        if (!controller.signal.aborted)
          form.setFieldsValue({
            history_agent_minutes: values.history_agent_minutes ?? 0,
            share_previous_chats: values.share_previous_chats !== false,
          });
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [tenantId, revision, form]);
  /** save 只发送本表单字段并等待服务器确认，失败保留输入。 */
  async function save(values: RoutingSettings) {
    setSaving(true);
    setError(null);
    try {
      const saved = await api<RoutingSettings>("/tenant/chat-settings", {
        tenantId,
        method: "PATCH",
        bodyJSON: values,
      });
      form.setFieldsValue(saved);
      message.success("复访与历史策略已保存");
    } catch (problem) {
      setError(problem);
    } finally {
      setSaving(false);
    }
  }
  return (
    <section style={{ maxWidth: 640 }}>
      <h2>复访与历史</h2>
      <p className="muted">
        同一浏览器会保存访客凭证、会话编号、客户资料与入链参数；每次重新进入仍由服务端确认当前接待和可见范围。
      </p>
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      <Form form={form} layout="vertical" onFinish={save}>
        <Form.Item
          name="history_agent_minutes"
          label="历史客服优先窗口（分钟）"
          rules={[
            {
              required: true,
              type: "integer",
              min: 0,
              max: 525600,
              message: "请输入0至525600分钟整数",
            },
          ]}
          extra="0表示永久优先。大于0时，从会话最后一条公开消息起算，无消息按创建时间；超时重新进入正常匹配，心跳和已读不会延长窗口。"
        >
          <InputNumber min={0} max={525600} precision={0} disabled={loading} />
        </Form.Item>
        <p className="form-hint">
          优先顺序：指定客服 → 窗口内历史客服 →
          正常流程。指定客服离线时无法访问；历史客服离线时回到正常分配。资格、技能和容量要求继续生效。
        </p>
        <Form.Item
          name="share_previous_chats"
          label="允许接待客服查看此前其他客服的聊天"
          valuePropName="checked"
          extra="默认开启。关闭后普通客服只能查看其授权接待段内的消息；管理员仍可审计。历史分享继续受客户、服务空间与业务组授权限制。"
        >
          <Switch disabled={loading} />
        </Form.Item>
        <Button type="primary" htmlType="submit" loading={saving} disabled={loading}>
          保存复访与历史策略
        </Button>
      </Form>
    </section>
  );
}
