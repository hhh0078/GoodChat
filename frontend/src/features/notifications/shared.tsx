/** 文件作用说明：复用邮件场景名称、SMTP受理状态和按用户时区展示的通知列表。 */
"use client";
import { useTranslator } from "@/i18n/use-translator";
import { useFormats } from "@/i18n/use-formats";
import { Table, Tag } from "antd";
export type Notice = {
  id: string;
  kind: string;
  status: string;
  attempts: number;
  failure_kind: string;
  locale: string;
  created_at: string;
  recipient?: string;
  attempt_history?: { attempt: number; status: string; failure_kind: string; created_at: string }[];
};
export const kinds: Record<string, string> = {
  admin_invitation: "管理员开通邀请",
  lead: "离线留言提醒",
  code_register: "注册验证码",
  code_login: "登录验证码",
  code_reset_password: "重置密码验证码",
  code_invitation: "接受邀请验证码",
  code_ownership: "所有者移交验证码",
  invitation: "团队邀请",
  welcome: "开通欢迎",
  password_changed: "密码变更",
  permissions_changed: "权限变更",
  balance_low: "低余额提醒",
  balance_critical: "严重低余额",
  ai_blocked: "AI回复受阻",
  topup: "充值入账",
  subscription_active: "套餐开通",
  subscription_expiring: "套餐即将到期",
  subscription_expired: "套餐已到期",
  handoff_waiting: "转人工待处理",
  ai_errors: "AI持续异常",
  digest: "每周服务摘要",
  onboarding: "接入配置提醒",
  product_update: "产品更新",
  test: "测试邮件",
};
const failures: Record<string, string> = {
  configuration: "发信服务配置不完整",
  temporary: "临时失败",
  permanent: "邮件服务明确拒绝",
  recipient_permanent: "邮件服务明确拒绝",
  transport: "传输失败",
  template: "模板渲染失败",
  confirmation_lost: "最终受理确认未收到",
  worker_interrupted: "执行中断，无法确定受理结果",
  no_longer_relevant: "业务状态已更新",
  previous_permanent_failure: "收件地址已暂停",
};
const states: Record<string, string> = {
  queued: "排队中",
  running: "投递中",
  accepted: "邮件服务已受理",
  failed: "失败",
  unknown: "结果不确定",
  cancelled: "已取消",
  suppressed: "已抑制",
};
/** NoticeTable 仅展示脱敏元数据，SMTP受理与最终收件分开表述。 */
export function NoticeTable({
  items,
  loading = false,
  platform = false,
}: {
  items: Notice[];
  loading?: boolean;
  platform?: boolean;
}) {
  const tr = useTranslator();
  const { dateTimeFormatter } = useFormats();
  return (
    <Table
      rowKey="id"
      loading={loading}
      dataSource={items}
      scroll={{ x: 700 }}
      pagination={{ pageSize: 10 }}
      expandable={{
        rowExpandable: (row) => !!row.attempt_history?.length,
        expandedRowRender: (row) => (
          <Table
            rowKey="attempt"
            size="small"
            pagination={false}
            dataSource={row.attempt_history || []}
            columns={[
              { title: tr("尝试次数"), dataIndex: "attempt" },
              { title: tr("状态"), dataIndex: "status", render: (v) => tr(states[v] || v) },
              {
                title: tr("处理说明"),
                dataIndex: "failure_kind",
                render: (v) => (v ? tr(failures[v] || "需要平台核对") : "—"),
              },
              {
                title: tr("创建时间"),
                dataIndex: "created_at",
                render: (v) =>
                  dateTimeFormatter({ dateStyle: "short", timeStyle: "short" }).format(new Date(v)),
              },
            ]}
          />
        ),
      }}
      columns={[
        { title: tr("通知类型"), dataIndex: "kind", render: (v: string) => tr(kinds[v] || v) },
        ...(platform ? [{ title: tr("收件人"), dataIndex: "recipient" }] : []),
        {
          title: tr("状态"),
          dataIndex: "status",
          render: (v: string) => (
            <Tag
              color={
                v === "accepted"
                  ? "success"
                  : v === "failed" || v === "unknown"
                    ? "warning"
                    : "default"
              }
            >
              {tr(states[v] || v)}
            </Tag>
          ),
        },
        {
          title: tr("处理说明"),
          dataIndex: "failure_kind",
          render: (v: string) => (v ? tr(failures[v] || "需要平台核对") : "—"),
        },
        { title: tr("尝试次数"), dataIndex: "attempts" },
        { title: tr("语言"), dataIndex: "locale" },
        {
          title: tr("创建时间"),
          dataIndex: "created_at",
          render: (v: string) =>
            dateTimeFormatter({ dateStyle: "short", timeStyle: "short" }).format(new Date(v)),
        },
      ]}
    />
  );
}
