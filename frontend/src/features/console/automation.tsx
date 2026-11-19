/** 文件作用说明：配置租户欢迎语、关键词与快捷话术，并查询留言与评价；个人音量统一在工作台调整。 */
"use client";
import { EmptyData, ErrorNotice, PageTitle } from "@/components/page-state";
import { useSession } from "@/features/auth/session";
import { AttachmentView } from "@/features/chat/attachment";
import { useFormats } from "@/i18n/use-formats";
import { useTranslator } from "@/i18n/use-translator";
import { api, errorMessage } from "@/lib/api";
import type { QuickReply } from "@/lib/quick-replies";
import type { Resource } from "@/lib/types";
import { useResource } from "@/lib/use-resource";
import {
  App,
  Button,
  Drawer,
  Form,
  Grid,
  Input,
  InputNumber,
  Select,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
} from "antd";
import { useEffect, useState } from "react";
import { AutomationPolicy } from "./automation-policy";
import { LeadNotifications } from "./lead-notifications";
import { QuickReplyManager } from "./quick-reply-manager";
import { ResourcePanel } from "./resource-panel";
import { WelcomeRules } from "./welcome-rules";
type LeadRecord = Resource & {
  status: string;
  notes?: string;
  attachments?: {
    id: string;
    file_name: string;
    content_type: string;
    size_bytes: number;
  }[];
};
/** OperationsSettings 维护租户基础欢迎设置，保存时仅提交当前表单字段。 */
export function OperationsSettings() {
  const tr = useTranslator();
  const { tenantId } = useSession();
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const path = "/tenant/chat-settings";
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
      message.success(tr("设置已保存"));
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="studio-settings-form">
      <div className="studio-panel-heading">
        <h2>{tr("服务与欢迎语")}</h2>
        <p>
          {tr(
            "以下设置对整个租户生效。只需要一句问候时，填写基础欢迎语即可；图片、多条消息或不同入口的问候请在\u201C高级欢迎规则\u201D配置。 ",
          )}
        </p>
      </div>
      <ErrorNotice error={error} />
      <Form form={form} layout="vertical" onFinish={save} className="studio-preferences-form">
        <Form.Item
          name="welcome_enabled"
          label={tr("欢迎消息总开关")}
          valuePropName="checked"
          extra={tr(
            "新会话创建后自动发送问候。关闭后，基础欢迎语和高级欢迎规则都不再发送；普通刷新不会重复欢迎。",
          )}
        >
          <Switch />
        </Form.Item>
        <Form.Item
          name="welcome_text"
          label={tr("基础文字欢迎语")}
          extra={tr(
            "例如：您好，请问有什么可以帮您？没有匹配的高级欢迎规则时才发送这段文字；命中高级规则后只发送该规则的一组内容。留空则不发送基础欢迎语。",
          )}
        >
          <Input.TextArea rows={4} maxLength={2000} />
        </Form.Item>
        <Form.Item
          name="welcome_delay_seconds"
          label={tr("基础欢迎语等待时间（秒）")}
          extra={tr(
            "从新会话创建时开始计时，0表示立即发送，最多60秒。这里只控制基础文字；高级规则中每条消息的等待时间单独设置。",
          )}
        >
          <InputNumber min={0} max={60} precision={0} />
        </Form.Item>
        <Form.Item
          name="keywords_enabled"
          label={tr("关键词自动回复总开关")}
          valuePropName="checked"
          extra={tr(
            "开启后，访客发来的文字命中\u201C关键词回复\u201D中的已启用规则时，系统自动回复。例如访客问价格，自动发送价目说明；关闭后所有关键词规则暂停。",
          )}
        >
          <Switch />
        </Form.Item>
        <Button type="primary" htmlType="submit" loading={busy} disabled={!loaded}>
          {tr("保存设置 ")}
        </Button>
      </Form>
      <p className="form-hint">
        {tr(
          "个人音量在客服工作台右上角调整；团队统一的未读提醒间隔和提示音在\u201C租户设置 \u2192 接待提醒\u201D配置。 ",
        )}
      </p>
    </section>
  );
}
/** LeadList 展示真实离线留言并更新跟进状态，不将聊天记录作为留言。 */
function LeadList() {
  const { currentPreferences, numberFormatter } = useFormats();

  const tr = useTranslator();
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
      message.success(tr("跟进状态已保存"));
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
      message.success(tr("内部跟进记录已保存"));
    } catch (problem) {
      setSaveError(problem);
    } finally {
      setSaving(false);
    }
  }
  return (
    <div className="table-card studio-table-surface">
      <div className="studio-panel-heading">
        <h2>{tr("离线留言")}</h2>
        <p>{tr("查看客户留言并记录内部跟进。")}</p>
      </div>
      <ErrorNotice error={data.error} retry={data.reload} />
      <Table
        rowKey="id"
        dataSource={data.items}
        loading={data.loading}
        scroll={{ x: 850 }}
        locale={{ emptyText: <EmptyData text={tr("暂时没有离线留言")} /> }}
        columns={[
          { title: tr("访客"), dataIndex: "display_name" },
          { title: tr("电话"), dataIndex: "phone" },
          { title: tr("留言"), dataIndex: "content", ellipsis: true },
          {
            title: tr("附件"),
            render: (_, row) => tr("{p0} 个", { p0: String(row.attachments?.length || 0) }),
          },
          {
            title: tr("详情"),
            render: (_, row) => (
              <Button
                type="link"
                onClick={() => {
                  setSelected(row);
                  setNotes(row.notes || "");
                  setSaveError(null);
                }}
              >
                {tr("查看与跟进 ")}
              </Button>
            ),
          },
          {
            title: tr("状态"),
            dataIndex: "status",
            render: (value, row) => (
              <Select
                value={String(value)}
                style={{ width: 120 }}
                onChange={(status) => void updateStatus(row.id, status)}
                options={[
                  { label: tr("待跟进"), value: "new" },
                  { label: tr("跟进中"), value: "following" },
                  { label: tr("已处理"), value: "resolved" },
                ]}
              />
            ),
          },
          {
            title: tr("时间"),
            dataIndex: "created_at",
            render: (value) =>
              new Date(String(value)).toLocaleString(currentPreferences().locale, {
                timeZone: currentPreferences().timezone,
              }),
          },
        ]}
      />
      <Drawer
        rootClassName="studio-drawer"
        title={tr("留言详情与跟进")}
        open={!!selected}
        onClose={() => setSelected(null)}
        width={600}
      >
        <p>
          {String(selected?.display_name || tr("访客"))} ·{" "}
          {String(selected?.phone || tr("未留电话"))}
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
          {tr("附件由访客主动共享给留言处理团队，聊天撤回不会撤销这次独立共享。 ")}
        </p>
        <Form layout="vertical">
          <Form.Item label={tr("处理状态")}>
            <Select
              value={selected?.status}
              onChange={(status) =>
                setSelected((previous) => (previous ? { ...previous, status } : null))
              }
              options={[
                { label: tr("待跟进"), value: "new" },
                { label: tr("跟进中"), value: "following" },
                { label: tr("已处理"), value: "resolved" },
              ]}
            />
          </Form.Item>
          <Form.Item label={tr("内部跟进备注")}>
            <Input.TextArea
              rows={5}
              value={notes}
              onChange={(event) => setNotes(event.target.value)}
              maxLength={5000}
            />
          </Form.Item>
          <ErrorNotice error={saveError} />
          <Button type="primary" loading={saving} onClick={() => void saveDetails()}>
            {tr("保存内部跟进 ")}
          </Button>
        </Form>
        <p className="form-hint">{tr("保存处理记录不代表客户已收到外部回复。")}</p>
      </Drawer>
    </div>
  );
}
/** EvaluationList 只展示服务端记录的访客评价。 */
function EvaluationList() {
  const { currentPreferences, numberFormatter } = useFormats();

  const tr = useTranslator();
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
    api<{
      items: Resource[];
    }>(`/tenant/evaluations/${selected.id}/versions`, {
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
        <h2>{tr("服务评价")}</h2>
        <p>{tr("查看访客提交的评分与修改历史。")}</p>
      </div>
      <ErrorNotice error={data.error} retry={data.reload} />
      <Table
        rowKey="id"
        dataSource={data.items}
        loading={data.loading}
        locale={{ emptyText: <EmptyData text={tr("暂时没有服务评价")} /> }}
        columns={[
          { title: tr("会话编号"), dataIndex: "conversation_id", ellipsis: true },
          {
            title: tr("评分"),
            dataIndex: "score",
            render: (value) => <Tag>{String(value)} / 5</Tag>,
          },
          { title: tr("评价"), dataIndex: "comment" },
          { title: tr("版本"), dataIndex: "version", render: (value) => `v${value || 1}` },
          {
            title: tr("原始记录"),
            render: (_, row) => (
              <Button type="link" onClick={() => setSelected(row)}>
                {tr("查看评价版本 ")}
              </Button>
            ),
          },
          {
            title: tr("时间"),
            dataIndex: "created_at",
            render: (value) =>
              new Date(String(value)).toLocaleString(currentPreferences().locale, {
                timeZone: currentPreferences().timezone,
              }),
          },
        ]}
      />
      <Drawer
        rootClassName="studio-drawer"
        title={tr("访客评价版本")}
        open={!!selected}
        onClose={() => setSelected(null)}
        width={650}
      >
        <p className="muted">{tr("仅展示访客实际提交，不提供改写历史评分入口。")}</p>
        <ErrorNotice error={historyError} />
        <Table
          rowKey="id"
          dataSource={versions}
          loading={historyLoading}
          columns={[
            { title: tr("版本"), dataIndex: "version" },
            { title: tr("评分"), dataIndex: "score" },
            { title: tr("意见"), dataIndex: "comment" },
            {
              title: tr("时间"),
              dataIndex: "created_at",
              render: (value) =>
                new Date(String(value)).toLocaleString(currentPreferences().locale, {
                  timeZone: currentPreferences().timezone,
                }),
            },
          ]}
        />
      </Drawer>
    </div>
  );
}
/** AutomationPage 在同一运营入口管理自动回复、话术和独立留言评价。 */
export function AutomationPage() {
  const quickReplies = useResource<QuickReply>("/tenant/quick-replies");
  const tr = useTranslator();
  const { current, tenantId } = useSession();
  const screens = Grid.useBreakpoint();
  const manages = current.memberships.some(
    (member) => member.tenant_id === tenantId && ["owner", "admin"].includes(member.role),
  );
  return (
    <section className="studio-page studio-operations" aria-label={tr("回复与运营")}>
      <PageTitle title={tr("回复与运营")} description={tr("管理欢迎内容、快捷话术与服务提醒。")} />
      <Tabs
        className="studio-operations-tabs"
        tabPosition={screens.lg ? "left" : "top"}
        onChange={(key) => {
          if (key === "keywords") quickReplies.reload();
        }}
        items={[
          {
            key: "settings",
            label: tr("服务与欢迎语"),
            children: <OperationsSettings key={tenantId} />,
          },
          {
            key: "welcome-rules",
            label: tr("高级欢迎规则"),
            children: <WelcomeRules key={tenantId} />,
          },
          {
            key: "policy",
            label: tr("营业与评价策略"),
            children: <AutomationPolicy key={tenantId} />,
          },
          {
            key: "replies",
            label: tr("快捷话术"),
            children: <QuickReplyManager key={tenantId} manages={manages} />,
          },
          {
            key: "keywords",
            label: tr("关键词回复"),
            children: (
              <div className="table-card">
                <ResourcePanel
                  title={tr("关键词规则")}
                  path="/tenant/keyword-rules"
                  help={tr(
                    "访客发送文字后自动匹配，每条消息最多回复一条规则。先在\u201C服务与欢迎语\u201D开启关键词总开关，再新增并启用规则。也可选择快捷回复，命中后发送文字、媒体、选项或表单。",
                  )}
                  columns={[
                    { title: tr("规则名称"), dataIndex: "name" },
                    { title: tr("关键词"), dataIndex: "pattern" },
                    { title: tr("回复"), dataIndex: "reply", ellipsis: true },
                  ]}
                  fields={[
                    {
                      name: "name",
                      label: tr("规则名称"),
                      required: true,
                      hint: tr("仅供团队识别，例如\u201C价格咨询\u201D，不会发给访客。"),
                    },
                    {
                      name: "match_mode",
                      label: tr("匹配方式"),
                      type: "select",
                      initial: "contains",
                      hint: tr(
                        "\u201C包含\u201D会匹配整句话中的关键词，例如\u201C价格\u201D能命中\u201C请问价格多少\u201D；\u201C完整匹配\u201D要求整条消息相同。英文字母不区分大小写。",
                      ),
                      options: [
                        { label: tr("包含关键词"), value: "contains" },
                        { label: tr("完整匹配"), value: "exact" },
                      ],
                    },
                    {
                      name: "pattern",
                      label: tr("关键词"),
                      required: true,
                      hint: tr(
                        "填写一段要匹配的文字，例如\u201C营业时间\u201D。多个关键词请分别建规则；这里不支持用逗号分隔或正则表达式。",
                      ),
                    },
                    {
                      name: "reply",
                      label: tr("自动回复文字"),
                      required: false,
                      type: "textarea",
                      hint: tr(
                        "命中后直接发送给访客的内容，例如\u201C我们的营业时间为每天9:00至18:00\u201D。",
                      ),
                    },
                    {
                      name: "quick_reply_id",
                      label: tr("选择快捷回复"),
                      type: "select",
                      options: quickReplies.items
                        .filter((q) => q.scope === "public" && q.enabled)
                        .map((q) => ({ value: q.id, label: q.title })),
                      hint: tr(
                        "选择后发送该快捷回复；不选择时使用上方文字。公共规则仅可选公共话术。",
                      ),
                    },
                    {
                      name: "priority",
                      label: tr("优先级"),
                      type: "number",
                      initial: 1,
                      hint: tr(
                        "数字越大越先匹配，例如10优先于1。多条规则同时命中时只取最优先的一条，建议不同规则使用不同数字。",
                      ),
                    },
                    {
                      name: "cooldown_seconds",
                      label: tr("重复触发间隔（秒）"),
                      type: "number",
                      initial: 60,
                      hint: tr(
                        "同一会话内这条规则再次回复前要等待多久，默认60秒；0表示不限制。等待期间再次命中会跳过本次自动回复，避免连续刷屏。",
                      ),
                    },
                    {
                      name: "enabled",
                      label: tr("启用这条规则"),
                      type: "switch",
                      initial: true,
                      hint: tr(
                        "关闭只暂停当前规则，内容仍保留。还需要开启\u201C服务与欢迎语\u201D中的关键词总开关才会生效。",
                      ),
                    },
                  ]}
                />
              </div>
            ),
          },
          { key: "leads", label: tr("离线留言"), children: <LeadList key={tenantId} /> },
          {
            key: "lead-notifications",
            label: tr("留言邮件提醒"),
            children: <LeadNotifications key={tenantId} />,
          },
          { key: "evaluations", label: tr("服务评价"), children: <EvaluationList /> },
        ].filter((item) => manages || ["replies", "leads"].includes(item.key))}
      />
    </section>
  );
}
