/** 文件作用说明：在团队列表展示固定AI成员，允许编辑对外资料、启停及接待优先级，复用现有机器人配置。 */
"use client";
import { useEffect, useState } from "react";
import { Avatar, Button, Drawer, Form, Input, Select, Space, Switch, Tag } from "antd";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import { showRequestError } from "@/lib/request-error";
import { useSession } from "@/features/auth/session";
export type AIEmployee = {
  id: string;
  name: string;
  avatar_ref: string;
  avatar_url: string;
  enabled: boolean;
  priority: number;
  description: string;
  version: number;
  configured: boolean;
};
/** useAIEmployee 按当前租户读取固定AI成员，离开页面时取消旧请求。 */
export function useAIEmployee() {
  const { tenantId } = useSession();
  const [agent, setAgent] = useState<AIEmployee>();
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setAgent(undefined);
    if (tenantId)
      api<AIEmployee>("/tenant/ai-service-agent", { tenantId, signal: controller.signal })
        .then((value) => {
          if (!controller.signal.aborted) setAgent(value);
        })
        .catch((error) => {
          if (!controller.signal.aborted) showRequestError(error);
        });
    return () => controller.abort();
  }, [tenantId, revision]);
  return { agent, tenantId, reload: () => setRevision((value) => value + 1) };
}
/** AIMember 展示不可删除的系统AI成员及专用编辑抽屉，不占用人工坐席。 */
export function AIMember() {
  const tr = useTranslator();
  const { agent, tenantId, reload } = useAIEmployee();
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm();
  const [avatars, setAvatars] = useState<{ id: string; url: string; name: string }[]>([]);
  useEffect(() => {
    if (!editing) return;
    const controller = new AbortController();
    api<{ items: { id: string; url: string; name: string }[] }>("/tenant/avatar-assets", {
      tenantId,
      signal: controller.signal,
    })
      .then((data) => setAvatars(data.items || []))
      .catch((error) => {
        if (!controller.signal.aborted) showRequestError(error);
      });
    return () => controller.abort();
  }, [editing, tenantId]);
  /** save 保存AI资料并刷新列表，服务端拒绝删除和跨租户更新。 */
  async function save(values: Record<string, unknown>) {
    if (!agent) return;
    setSaving(true);
    try {
      await api("/tenant/ai-service-agent", {
        method: "PUT",
        tenantId,
        bodyJSON: { ...values, version: agent.version },
      });
      setEditing(false);
      reload();
    } catch (error) {
      showRequestError(error);
    } finally {
      setSaving(false);
    }
  }
  if (!agent) return null;
  return (
    <>
      <div
        className="ai-employee-card"
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: 12,
          flexWrap: "wrap",
          padding: "16px 0",
          borderBottom: "1px solid #e5ebf5",
        }}
      >
        <Space align="start">
          <Avatar src={agent.avatar_url} size={40} />
          <div>
            <strong>{agent.name}</strong> <Tag color="blue">{tr("AI客服")}</Tag>
            <Tag>{agent.enabled ? tr("启用") : tr("停用")}</Tag>
            <p className="muted" style={{ margin: "4px 0" }}>
              {tr("自动接待 · 不占人工坐席 · 暂不支持删除")}
            </p>
            {!agent.configured && (
              <Button type="link" href="/console/ai">
                {tr("配置机器人")}
              </Button>
            )}
          </div>
        </Space>
        <Button
          onClick={() => {
            form.setFieldsValue(agent);
            setEditing(true);
          }}
        >
          {tr("编辑 ")}
        </Button>
      </div>
      <Drawer
        title={tr("编辑 AI 成员")}
        open={editing}
        onClose={() => setEditing(false)}
        size={440}
        rootClassName="console-drawer"
      >
        <p className="form-hint">
          {tr("模型、知识库、话术和媒体继续使用 AI 客服中的配置。转人工遵循接入链接的分配方式。")}
        </p>
        <Form form={form} layout="vertical" onFinish={save}>
          <Form.Item
            name="name"
            label={tr("显示名称")}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={60} />
          </Form.Item>
          <Form.Item name="avatar_ref" label={tr("头像")}>
            <Select
              options={[
                { value: "goodchat", label: "GoodChat" },
                ...avatars.map((x) => ({
                  value: x.id,
                  label: (
                    <Space>
                      <Avatar src={x.url} size={24} />
                      {x.name || x.id}
                    </Space>
                  ),
                })),
              ]}
            />
          </Form.Item>
          <Form.Item name="enabled" label={tr("启用")} valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="priority" hidden>
            <Input type="hidden" />
          </Form.Item>
          <p className="form-hint">{tr("AI 默认优先接待；转人工后按原分配规则处理。")}</p>
          <Form.Item name="description" label={tr("服务说明")}>
            <Input.TextArea maxLength={500} rows={3} />
          </Form.Item>
          <Space wrap>
            <Button type="primary" htmlType="submit" loading={saving}>
              {tr("保存设置")}
            </Button>
            <Button href="/console/ai">{tr("配置机器人")}</Button>
          </Space>
        </Form>
      </Drawer>
    </>
  );
}
