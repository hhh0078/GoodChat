/** 文件作用说明：提供个人邮件订阅、静默时间、租户通知规则与本人投递历史，权限由服务端再次校验。 */
"use client";
import { PageTitle, ErrorNotice } from "@/components/page-state";
import { useSession } from "@/features/auth/session";
import { LeadNotifications } from "@/features/console/lead-notifications";
import { useTranslator } from "@/i18n/use-translator";
import { useFormats } from "@/i18n/use-formats";
import { api, errorMessage } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import { Alert, App, Button, Card, Form, InputNumber, Select, Space, Switch, Tabs } from "antd";
import { useEffect, useState } from "react";
import { useLocale } from "next-intl";
import { NoticeTable, type Notice } from "./shared";
/** NotificationsPage 在语言或租户切换时取消旧请求并重新读取设置。 */
export function NotificationsPage() {
  const tr = useTranslator();
  const locale = useLocale();
  const { current, tenantId } = useSession();
  const { currentPreferences } = useFormats();
  const { message } = App.useApp();
  const [personal] = Form.useForm();
  const [tenant] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [members, setMembers] = useState<{ id: string; email: string; role: string }[]>([]);
  const [loaded, setLoaded] = useState(false);
  const history = useResource<Notice>("/notifications", true);
  const role = current.memberships.find((m) => m.tenant_id === tenantId)?.role;
  const manages = role === "owner" || role === "admin";
  useEffect(() => {
    const controller = new AbortController();
    setLoaded(false);
    personal.resetFields();
    tenant.resetFields();
    setMembers([]);
    setError(null);
    const tasks = [
      api<Record<string, unknown>>("/notifications/preferences", {
        signal: controller.signal,
      }).then((v) => {
        if (!controller.signal.aborted) personal.setFieldsValue(v);
      }),
    ];
    if (manages && tenantId)
      tasks.push(
        api<Record<string, unknown> & { members: { id: string; email: string; role: string }[] }>(
          "/tenant/notifications/settings",
          { tenantId, signal: controller.signal },
        ).then((v) => {
          if (!controller.signal.aborted) {
            tenant.setFieldsValue({
              ...v,
              low: Number(v.low_micro) / 1000000,
              critical: Number(v.critical_micro) / 1000000,
            });
            setMembers(v.members || []);
          }
        }),
      );
    Promise.all(tasks)
      .then(() => {
        if (!controller.signal.aborted) setLoaded(true);
      })
      .catch((e) => {
        if (!controller.signal.aborted) setError(e);
      });
    return () => controller.abort();
  }, [tenantId, manages, locale, personal, tenant]);
  /** save 保存本人订阅或企业规则，失败保留表单并明确反馈。 */
  async function save(path: string, values: Record<string, unknown>) {
    setBusy(true);
    try {
      await api(path, { method: "PUT", tenantId, bodyJSON: values });
      message.success(tr("通知设置已保存"));
      history.reload();
    } catch (e) {
      message.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  const preferences = currentPreferences();
  return (
    <section className="studio-page">
      <PageTitle
        title={tr("邮件与通知")}
        description={tr("管理邮件订阅、收件时段和重要业务提醒。")}
      />
      <ErrorNotice error={error} />
      <Alert
        type="info"
        showIcon
        title={tr(
          "安全通知即时发送，不能关闭。普通提醒遵守个人静默时间；SMTP受理不代表进入收件箱。",
        )}
      />
      <Tabs
        items={[
          {
            key: "personal",
            label: tr("我的订阅"),
            children: (
              <Card loading={!loaded && !error}>
                <p>{tr("邮件使用账号已保存的语言与时区；可通过语言和时区按钮调整并保存。")}</p>
                <p>
                  {preferences.locale} · {preferences.timezone}
                </p>
                <Form
                  form={personal}
                  layout="vertical"
                  onFinish={(v) => save("/notifications/preferences", v)}
                >
                  <Form.Item
                    name="business_enabled"
                    label={tr("一般业务邮件")}
                    valuePropName="checked"
                  >
                    <Switch />
                  </Form.Item>
                  <Form.Item
                    name="digest_enabled"
                    label={tr("每周服务摘要")}
                    valuePropName="checked"
                  >
                    <Switch />
                  </Form.Item>
                  <Form.Item
                    name="updates_enabled"
                    label={tr("产品更新与接入引导")}
                    valuePropName="checked"
                  >
                    <Switch />
                  </Form.Item>
                  <Space wrap>
                    <Form.Item name="quiet_start" label={tr("静默开始小时")}>
                      <InputNumber min={0} max={23} />
                    </Form.Item>
                    <Form.Item name="quiet_end" label={tr("静默结束小时")}>
                      <InputNumber min={0} max={23} />
                    </Form.Item>
                  </Space>
                  <p>{tr("按个人时区计算；开始与结束相同表示关闭静默。")}</p>
                  <Button type="primary" htmlType="submit" loading={busy} disabled={!loaded}>
                    {tr("保存设置")}
                  </Button>
                </Form>
              </Card>
            ),
          },
          ...(manages
            ? [
                {
                  key: "tenant",
                  label: tr("企业提醒规则"),
                  children: (
                    <Card loading={!loaded && !error}>
                      <Form
                        form={tenant}
                        layout="vertical"
                        onFinish={(v) =>
                          save("/tenant/notifications/settings", {
                            recipient_ids: v.recipient_ids || [],
                            low_micro: Math.round(Number(v.low) * 1000000),
                            critical_micro: Math.round(Number(v.critical) * 1000000),
                            handoff_minutes: v.handoff_minutes,
                            service_enabled: v.service_enabled,
                          })
                        }
                      >
                        <Alert
                          type="info"
                          title={tr("所有者默认接收账务提醒，只能额外选择本租户有效管理员。")}
                        />
                        <Form.Item name="recipient_ids" label={tr("额外通知管理员")}>
                          <Select
                            mode="multiple"
                            options={members
                              .filter((m) => m.role !== "owner")
                              .map((m) => ({ value: m.id, label: m.email }))}
                          />
                        </Form.Item>
                        <Form.Item
                          name="low"
                          label={tr("低余额阈值（元）")}
                          rules={[{ required: true }]}
                        >
                          <InputNumber min={0} max={1000000} precision={2} />
                        </Form.Item>
                        <Form.Item
                          name="critical"
                          label={tr("严重低余额阈值（元）")}
                          rules={[{ required: true }]}
                        >
                          <InputNumber min={0} max={1000000} precision={2} />
                        </Form.Item>
                        <Form.Item
                          name="handoff_minutes"
                          label={tr("转人工超时（分钟）")}
                          rules={[{ required: true }]}
                        >
                          <InputNumber min={1} max={1440} precision={0} />
                        </Form.Item>
                        <Form.Item
                          name="service_enabled"
                          label={tr("转人工与持续故障提醒")}
                          valuePropName="checked"
                        >
                          <Switch />
                        </Form.Item>
                        <Button type="primary" htmlType="submit" loading={busy} disabled={!loaded}>
                          {tr("保存设置")}
                        </Button>
                      </Form>
                    </Card>
                  ),
                },
                { key: "leads", label: tr("离线留言提醒"), children: <LeadNotifications /> },
              ]
            : []),
          {
            key: "history",
            label: tr("投递历史"),
            children: (
              <Card extra={<Button onClick={history.reload}>{tr("刷新")}</Button>}>
                <ErrorNotice error={history.error} />
                <NoticeTable items={history.items} loading={history.loading} />
              </Card>
            ),
          },
        ]}
      />
    </section>
  );
}
