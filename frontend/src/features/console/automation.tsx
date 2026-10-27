/** 文件作用说明：配置真实欢迎语、关键词、快捷话术、提醒偏好并查询留言与评价。 */
"use client";
import { useEffect, useState } from "react";
import {
  App,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Select,
  Slider,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
} from "antd";
import { useSession } from "@/features/auth/session";
import { api, errorMessage } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import { ErrorNotice, PageTitle, EmptyData } from "@/components/page-state";
import { ResourcePanel } from "./resource-panel";
import type { Resource } from "@/lib/types";
/** OperationsSettings 根据配置类型维护服务端持久设置。 */
export function OperationsSettings({ notifications = false }: { notifications?: boolean }) {
  const { tenantId } = useSession();
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const path = notifications ? "/tenant/notification-preferences" : "/tenant/chat-settings";
  useEffect(() => {
    const controller = new AbortController();
    setLoaded(false);
    api<Record<string, unknown>>(path, { tenantId, signal: controller.signal })
      .then((data) => {
        if (!controller.signal.aborted) {
          form.setFieldsValue(data);
          setLoaded(true);
          setError(null);
        }
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      });
    return () => controller.abort();
  }, [tenantId, path, form]);
  /** save 只将服务端确认的设置反馈为已保存。 */
  async function save(values: Record<string, unknown>) {
    setBusy(true);
    try {
      const data = await api(path, { method: "PATCH", tenantId, bodyJSON: values });
      form.setFieldsValue(data);
      setError(null);
      message.success("设置已保存");
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Card style={{ maxWidth: 780 }}>
      <ErrorNotice error={error} />
      <Form form={form} layout="vertical" onFinish={save}>
        {notifications ? (
          <>
            <Form.Item name="muted" label="免打扰" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item
              name="system_enabled"
              label="系统通知"
              valuePropName="checked"
              extra="同时需要在客服工作台主动开启浏览器通知权限。"
            >
              <Switch />
            </Form.Item>
            <Form.Item name="sound_enabled" label="消息提示音" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item
              name="speech_enabled"
              label="语音播报"
              valuePropName="checked"
              extra="需要浏览器允许播放，建议仅在私人接待环境启用。"
            >
              <Switch />
            </Form.Item>
            <Form.Item name="content_preview" label="允许通知展示消息内容" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="volume" label="音量">
              <Slider min={0} max={1} step={0.1} />
            </Form.Item>
          </>
        ) : (
          <>
            <Form.Item name="welcome_enabled" label="启用欢迎语" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="welcome_text" label="欢迎语内容">
              <Input.TextArea rows={4} maxLength={2000} />
            </Form.Item>
            <Form.Item name="welcome_delay_seconds" label="欢迎语延迟（秒）">
              <InputNumber min={0} max={60} />
            </Form.Item>
            <Form.Item name="keywords_enabled" label="启用关键词自动回复" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="typing_enabled" label="显示对方正在输入" valuePropName="checked">
              <Switch />
            </Form.Item>
          </>
        )}
        <Button type="primary" htmlType="submit" loading={busy} disabled={!loaded}>
          保存设置
        </Button>
      </Form>
    </Card>
  );
}
/** LeadList 展示真实离线留言并更新跟进状态，不将聊天记录作为留言。 */
function LeadList() {
  const data = useResource<Resource>("/tenant/leads");
  const { message } = App.useApp();
  /** updateStatus 保存留言处理进度后重新查询。 */
  async function updateStatus(id: string, status: string) {
    try {
      await api(`/tenant/leads/${id}`, {
        method: "PATCH",
        tenantId: data.tenantId,
        bodyJSON: { status },
      });
      data.reload();
      message.success("跟进状态已保存");
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  return (
    <div className="table-card">
      <ErrorNotice error={data.error} retry={data.reload} />
      <Table
        rowKey="id"
        dataSource={data.items}
        loading={data.loading}
        scroll={{ x: 850 }}
        locale={{ emptyText: <EmptyData text="暂时没有离线留言" /> }}
        columns={[
          { title: "访客", dataIndex: "display_name" },
          { title: "电话", dataIndex: "phone" },
          { title: "留言", dataIndex: "content", ellipsis: true },
          {
            title: "状态",
            dataIndex: "status",
            render: (value, row) => (
              <Select
                value={String(value)}
                style={{ width: 120 }}
                onChange={(status) => void updateStatus(row.id, status)}
                options={[
                  { label: "待跟进", value: "new" },
                  { label: "跟进中", value: "following" },
                  { label: "已处理", value: "resolved" },
                ]}
              />
            ),
          },
          {
            title: "时间",
            dataIndex: "created_at",
            render: (value) => new Date(String(value)).toLocaleString("zh-CN"),
          },
        ]}
      />
    </div>
  );
}
/** EvaluationList 只展示服务端记录的访客评价。 */
function EvaluationList() {
  const data = useResource<Resource>("/tenant/evaluations");
  return (
    <div className="table-card">
      <ErrorNotice error={data.error} retry={data.reload} />
      <Table
        rowKey="id"
        dataSource={data.items}
        loading={data.loading}
        locale={{ emptyText: <EmptyData text="暂时没有服务评价" /> }}
        columns={[
          { title: "会话编号", dataIndex: "conversation_id", ellipsis: true },
          { title: "评分", dataIndex: "score", render: (value) => <Tag>{String(value)} / 5</Tag> },
          { title: "评价", dataIndex: "comment" },
          {
            title: "时间",
            dataIndex: "created_at",
            render: (value) => new Date(String(value)).toLocaleString("zh-CN"),
          },
        ]}
      />
    </div>
  );
}
/** AutomationPage 在同一运营入口管理自动回复、话术和独立留言评价。 */
export function AutomationPage() {
  const { current, tenantId } = useSession();
  const manages = current.memberships.some(
    (member) => member.tenant_id === tenantId && ["owner", "admin"].includes(member.role),
  );
  return (
    <>
      <PageTitle
        title="回复与运营"
        description="自动回复由服务端写入实际消息，客服无需手动插入欢迎语。"
      />
      <Tabs
        items={[
          { key: "settings", label: "服务与欢迎语", children: <OperationsSettings /> },
          {
            key: "replies",
            label: "快捷话术",
            children: (
              <div className="table-card">
                <ResourcePanel
                  title="话术"
                  path="/tenant/quick-replies"
                  canEdit={(item) => manages || item.scope === "personal"}
                  columns={[
                    { title: "标题", dataIndex: "title" },
                    { title: "内容", dataIndex: "content", ellipsis: true },
                    {
                      title: "范围",
                      dataIndex: "scope",
                      render: (value) => (value === "personal" ? "个人" : "团队公共"),
                    },
                  ]}
                  fields={[
                    { name: "title", label: "话术标题", required: true },
                    { name: "content", label: "话术内容", type: "textarea", required: true },
                    { name: "shortcut", label: "快捷编号" },
                    { name: "category", label: "话术分类" },
                    {
                      name: "scope",
                      label: "使用范围",
                      type: "select",
                      initial: manages ? "public" : "personal",
                      options: [
                        ...(manages ? [{ label: "团队公共", value: "public" }] : []),
                        { label: "仅自己", value: "personal" },
                      ],
                    },
                    { name: "enabled", label: "启用", type: "switch", initial: true },
                  ]}
                />
              </div>
            ),
          },
          {
            key: "keywords",
            label: "关键词回复",
            children: (
              <div className="table-card">
                <ResourcePanel
                  title="关键词规则"
                  path="/tenant/keyword-rules"
                  columns={[
                    { title: "规则名称", dataIndex: "name" },
                    { title: "关键词", dataIndex: "pattern" },
                    { title: "回复", dataIndex: "reply", ellipsis: true },
                  ]}
                  fields={[
                    { name: "name", label: "规则名称", required: true },
                    {
                      name: "match_mode",
                      label: "匹配方式",
                      type: "select",
                      initial: "contains",
                      options: [
                        { label: "包含关键词", value: "contains" },
                        { label: "完整匹配", value: "exact" },
                      ],
                    },
                    { name: "pattern", label: "关键词", required: true },
                    { name: "reply", label: "回复内容", required: true, type: "textarea" },
                    { name: "priority", label: "优先级", type: "number", initial: 1 },
                    {
                      name: "cooldown_seconds",
                      label: "重复触发间隔（秒）",
                      type: "number",
                      initial: 60,
                    },
                    { name: "enabled", label: "启用", type: "switch", initial: true },
                  ]}
                />
              </div>
            ),
          },
          { key: "leads", label: "离线留言", children: <LeadList /> },
          { key: "evaluations", label: "服务评价", children: <EvaluationList /> },
          {
            key: "notifications",
            label: "消息提醒",
            children: <OperationsSettings notifications />,
          },
        ].filter((item) => manages || ["replies", "leads", "notifications"].includes(item.key))}
      />
    </>
  );
}
