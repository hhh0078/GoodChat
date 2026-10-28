/** 文件作用说明：配置真实欢迎语、关键词、快捷话术、提醒偏好并查询留言与评价。 */
"use client";
import { useEffect, useState } from "react";
import {
  App,
  Button,
  Drawer,
  Form,
  Grid,
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
import { AutomationPolicy } from "./automation-policy";
import { WelcomeRules } from "./welcome-rules";
import { LeadNotifications } from "./lead-notifications";
import { AttachmentView } from "@/features/chat/attachment";
import { QuickReplyManager } from "./quick-reply-manager";
import type { Resource } from "@/lib/types";
type LeadRecord = Resource & {
  status: string;
  notes?: string;
  attachments?: { id: string; file_name: string; content_type: string; size_bytes: number }[];
};
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
      if (notifications)
        window.dispatchEvent(
          new CustomEvent("goodchat:notification-preferences", { detail: { tenantId } }),
        );
      message.success("设置已保存");
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="studio-settings-form">
      <div className="studio-panel-heading">
        <h2>{notifications ? "消息提醒" : "服务与欢迎语"}</h2>
        <p>{notifications ? "为当前账号设置接待提醒。" : "控制基础欢迎内容与输入提示。"}</p>
      </div>
      <ErrorNotice error={error} />
      <Form form={form} layout="vertical" onFinish={save} className="studio-preferences-form">
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
    </section>
  );
}
/** LeadList 展示真实离线留言并更新跟进状态，不将聊天记录作为留言。 */
function LeadList() {
  const data = useResource<LeadRecord>("/tenant/leads");
  const { current } = useSession();
  const actorId = current.memberships.find((item) => item.tenant_id === data.tenantId)?.id || "";
  const [selected, setSelected] = useState<LeadRecord | null>(null);
  const [notes, setNotes] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<unknown>(null);
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
  /** saveDetails 保存内部跟进内容与状态，附件读取仍由服务端逐次检查权限。 */
  async function saveDetails() {
    if (!selected) return;
    setSaving(true);
    setSaveError(null);
    try {
      const value = await api<LeadRecord>(`/tenant/leads/${selected.id}`, {
        method: "PATCH",
        tenantId: data.tenantId,
        bodyJSON: { status: selected.status, notes },
      });
      setSelected((previous) => (previous ? { ...previous, ...value } : null));
      data.reload();
      message.success("内部跟进记录已保存");
    } catch (problem) {
      setSaveError(problem);
    } finally {
      setSaving(false);
    }
  }
  return (
    <div className="table-card studio-table-surface">
      <div className="studio-panel-heading">
        <h2>离线留言</h2>
        <p>查看客户留言并记录内部跟进。</p>
      </div>
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
          { title: "附件", render: (_, row) => `${row.attachments?.length || 0} 个` },
          {
            title: "详情",
            render: (_, row) => (
              <Button
                type="link"
                onClick={() => {
                  setSelected(row);
                  setNotes(row.notes || "");
                  setSaveError(null);
                }}
              >
                查看与跟进
              </Button>
            ),
          },
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
      <Drawer
        rootClassName="studio-drawer"
        title="留言详情与跟进"
        open={!!selected}
        onClose={() => setSelected(null)}
        width={600}
      >
        <p>
          {String(selected?.display_name || "访客")} · {String(selected?.phone || "未留电话")}
        </p>
        <p style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
          {String(selected?.content || "")}
        </p>
        <Space direction="vertical" style={{ width: "100%", marginBottom: 16 }}>
          {selected?.attachments?.map((attachment) => (
            <AttachmentView
              key={attachment.id}
              attachment={attachment}
              identity={{ tenantId: data.tenantId, actorId, kind: "agent" }}
            />
          ))}
        </Space>
        <p className="form-hint">
          附件由访客主动共享给留言处理团队，聊天撤回不会撤销这次独立共享。
        </p>
        <Form layout="vertical">
          <Form.Item label="处理状态">
            <Select
              value={selected?.status}
              onChange={(status) =>
                setSelected((previous) => (previous ? { ...previous, status } : null))
              }
              options={[
                { label: "待跟进", value: "new" },
                { label: "跟进中", value: "following" },
                { label: "已处理", value: "resolved" },
              ]}
            />
          </Form.Item>
          <Form.Item label="内部跟进备注">
            <Input.TextArea
              rows={5}
              value={notes}
              onChange={(event) => setNotes(event.target.value)}
              maxLength={5000}
            />
          </Form.Item>
          <ErrorNotice error={saveError} />
          <Button type="primary" loading={saving} onClick={() => void saveDetails()}>
            保存内部跟进
          </Button>
        </Form>
        <p className="form-hint">保存处理记录不代表客户已收到外部回复。</p>
      </Drawer>
    </div>
  );
}
/** EvaluationList 只展示服务端记录的访客评价。 */
function EvaluationList() {
  const data = useResource<Resource>("/tenant/evaluations");
  const [selected, setSelected] = useState<Resource | null>(null);
  const [versions, setVersions] = useState<Resource[]>([]);
  const [historyError, setHistoryError] = useState<unknown>(null);
  const [historyLoading, setHistoryLoading] = useState(false);
  useEffect(() => {
    setVersions([]);
    setHistoryError(null);
    if (!selected) return;
    const controller = new AbortController();
    setHistoryLoading(true);
    api<{ items: Resource[] }>(`/tenant/evaluations/${selected.id}/versions`, {
      tenantId: data.tenantId,
      signal: controller.signal,
    })
      .then((result) => {
        if (!controller.signal.aborted) setVersions(result.items || []);
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setHistoryError(problem);
      })
      .finally(() => {
        if (!controller.signal.aborted) setHistoryLoading(false);
      });
    return () => controller.abort();
  }, [selected, data.tenantId]);
  return (
    <div className="table-card studio-table-surface">
      <div className="studio-panel-heading">
        <h2>服务评价</h2>
        <p>查看访客提交的评分与修改历史。</p>
      </div>
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
          { title: "版本", dataIndex: "version", render: (value) => `v${value || 1}` },
          {
            title: "原始记录",
            render: (_, row) => (
              <Button type="link" onClick={() => setSelected(row)}>
                查看评价版本
              </Button>
            ),
          },
          {
            title: "时间",
            dataIndex: "created_at",
            render: (value) => new Date(String(value)).toLocaleString("zh-CN"),
          },
        ]}
      />
      <Drawer
        rootClassName="studio-drawer"
        title="访客评价版本"
        open={!!selected}
        onClose={() => setSelected(null)}
        width={650}
      >
        <p className="muted">仅展示访客实际提交，不提供改写历史评分入口。</p>
        <ErrorNotice error={historyError} />
        <Table
          rowKey="id"
          dataSource={versions}
          loading={historyLoading}
          columns={[
            { title: "版本", dataIndex: "version" },
            { title: "评分", dataIndex: "score" },
            { title: "意见", dataIndex: "comment" },
            {
              title: "时间",
              dataIndex: "created_at",
              render: (value) => new Date(String(value)).toLocaleString("zh-CN"),
            },
          ]}
        />
      </Drawer>
    </div>
  );
}
/** AutomationPage 在同一运营入口管理自动回复、话术和独立留言评价。 */
export function AutomationPage() {
  const { current, tenantId } = useSession();
  const screens = Grid.useBreakpoint();
  const manages = current.memberships.some(
    (member) => member.tenant_id === tenantId && ["owner", "admin"].includes(member.role),
  );
  return (
    <section className="studio-page studio-operations" aria-label="回复与运营">
      <PageTitle title="回复与运营" description="管理欢迎内容、快捷话术与服务提醒。" />
      <Tabs
        className="studio-operations-tabs"
        tabPosition={screens.lg ? "left" : "top"}
        items={[
          { key: "settings", label: "服务与欢迎语", children: <OperationsSettings /> },
          {
            key: "welcome-rules",
            label: "高级欢迎规则",
            children: <WelcomeRules key={tenantId} />,
          },
          { key: "policy", label: "营业与评价策略", children: <AutomationPolicy key={tenantId} /> },
          {
            key: "replies",
            label: "快捷话术",
            children: <QuickReplyManager key={tenantId} manages={manages} />,
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
          { key: "leads", label: "离线留言", children: <LeadList key={tenantId} /> },
          {
            key: "lead-notifications",
            label: "留言邮件提醒",
            children: <LeadNotifications key={tenantId} />,
          },
          { key: "evaluations", label: "服务评价", children: <EvaluationList /> },
          {
            key: "notifications",
            label: "消息提醒",
            children: <OperationsSettings notifications />,
          },
        ].filter((item) => manages || ["replies", "leads", "notifications"].includes(item.key))}
      />
    </section>
  );
}
