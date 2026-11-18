/** 文件作用说明：提供平台邮件模板版本、沙箱预览、本人测试发信及脱敏投递监控。 */
"use client";
import { useTranslator } from "@/i18n/use-translator";
import { useFormats } from "@/i18n/use-formats";
import { useLocale } from "next-intl";
import { useRouter, usePathname, useSearchParams } from "next/navigation";
import { useSession } from "@/features/auth/session";
import { ErrorNotice } from "@/components/page-state";
import { api, errorMessage } from "@/lib/api";
import {
  Alert,
  App,
  Button,
  Card,
  Form,
  Input,
  Popconfirm,
  Select,
  Space,
  Statistic,
  Table,
  Tabs,
} from "antd";
import { useEffect, useState } from "react";
import { SMTPSettings } from "./smtp-settings";
import { kinds, NoticeTable, type Notice } from "./shared";
type Template = {
  kind: string;
  locale: string;
  subject: string;
  body: string;
  button: string;
  version: number;
  history: { version: number; created_at: string }[];
};
type Monitor = {
  items: Notice[];
  authentication: ({ purpose: string } & Notice)[];
  stats: Record<string, number>;
  smtp_configured: boolean;
};
const languages = [
  { value: "zh-CN", label: "简体中文" },
  { value: "en", label: "English" },
  { value: "zh-TW", label: "繁體中文" },
  { value: "ja", label: "日本語" },
  { value: "ko", label: "한국어" },
  { value: "es", label: "Español" },
  { value: "pt-BR", label: "Português (Brasil)" },
  { value: "fr", label: "Français" },
  { value: "de", label: "Deutsch" },
];
/** PlatformMail 按所选模板语言重载版本，试发仅发当前管理员本人。 */
export function PlatformMail() {
  const tr = useTranslator();
  const locale = useLocale();
  const query = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const selectedView = query.get("mail_view") || "smtp";
  const view = ["smtp", "templates", "monitor", "test", "announcement"].includes(selectedView)
    ? selectedView
    : "smtp";
  /** changeView 将邮件子页签写入URL，避免账号初始化或语言刷新复位。 */
  function changeView(key: string) {
    const next = new URLSearchParams(query.toString());
    next.set("tab", "mail");
    next.set("mail_view", key);
    router.replace(pathname + "?" + next.toString(), { scroll: false });
  }

  const { currentPreferences, dateTimeFormatter } = useFormats();
  const { current } = useSession();
  const { message } = App.useApp();
  const [lang, setLang] = useState(locale);
  const [selected, setSelected] = useState("code_register");
  const [templates, setTemplates] = useState<Template[]>([]);
  const [monitor, setMonitor] = useState<Monitor | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const [monitorTick, setMonitorTick] = useState(0);
  const [html, setHTML] = useState("");
  const [form] = Form.useForm();
  const [test] = Form.useForm();
  const [announcement] = Form.useForm();
  const chosen = templates.find((t) => t.kind === selected);
  useEffect(() => {
    const c = new AbortController();
    setTemplates([]);
    form.resetFields();
    setHTML("");
    api<{ items: Template[] }>("/platform/mail-templates?locale=" + encodeURIComponent(lang), {
      signal: c.signal,
    })
      .then((v) => {
        if (!c.signal.aborted) setTemplates(v.items);
      })
      .catch((e) => {
        if (!c.signal.aborted) setError(e);
      });
    return () => c.abort();
  }, [lang, revision]);
  useEffect(() => {
    if (chosen) {
      form.setFieldsValue(chosen);
      setHTML("");
    }
  }, [chosen, form]);
  useEffect(() => {
    const c = new AbortController();
    api<Monitor>("/platform/notifications", { signal: c.signal })
      .then((v) => {
        if (!c.signal.aborted) setMonitor(v);
      })
      .catch((e) => {
        if (!c.signal.aborted) setError(e);
      });
    return () => c.abort();
  }, [revision, locale, monitorTick]);
  useEffect(() => {
    if (!monitor?.items.some((n) => n.status === "running" || n.status === "queued")) return;
    const timer = setInterval(() => setMonitorTick((v) => v + 1), 5000);
    return () => clearInterval(timer);
  }, [monitor]);
  /** action 执行受控平台操作并刷新真实状态，不把已排队提示成邮件已送达。 */
  async function action(path: string, body: unknown) {
    setBusy(true);
    setError(null);
    try {
      await api(path, { method: "POST", bodyJSON: body });
      message.success(tr("操作已受理，请查看投递或版本记录"));
      setRevision((v) => v + 1);
    } catch (e) {
      setError(e);
      message.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  /** preview 使用服务端安全渲染结果在无脚本沙箱中展示，预览不会发送邮件。 */
  async function preview() {
    setBusy(true);
    try {
      const mail = await api<{ HTML: string }>("/platform/mail-templates/preview", {
        method: "POST",
        bodyJSON: {
          ...form.getFieldsValue(),
          kind: selected,
          locale: lang,
          timezone: currentPreferences().timezone,
        },
      });
      setHTML(mail.HTML);
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section>
      <ErrorNotice error={error} />
      <Alert
        type={monitor?.smtp_configured ? "info" : "warning"}
        showIcon
        title={
          monitor?.smtp_configured
            ? tr("发信服务已配置，受理与真实收件分别核验")
            : tr("发信服务配置不完整")
        }
      />
      <Tabs
        activeKey={view}
        onChange={changeView}
        items={[
          {
            key: "smtp",
            label: tr("发信服务配置"),
            children: (
              <Card>
                <SMTPSettings />
              </Card>
            ),
          },
          {
            key: "templates",
            label: tr("九语言邮件模板"),
            children: (
              <Card>
                <Space wrap>
                  <Select
                    aria-label={tr("模板语言")}
                    value={lang}
                    onChange={setLang}
                    options={languages}
                    style={{ minWidth: 170 }}
                  />
                  <Select
                    aria-label={tr("邮件场景")}
                    value={selected}
                    onChange={setSelected}
                    options={Object.entries(kinds).map(([value, label]) => ({
                      value,
                      label: tr(label),
                    }))}
                    style={{ minWidth: 230 }}
                  />
                </Space>
                <p>{tr("模板只支持受限变量，按钮地址、验证码有效期和计费规则由服务端控制。")}</p>
                <Form
                  form={form}
                  layout="vertical"
                  onFinish={(v) =>
                    action("/platform/mail-templates", {
                      ...v,
                      kind: selected,
                      locale: lang,
                      base_version: chosen?.version || 0,
                    })
                  }
                >
                  <Form.Item name="subject" label={tr("邮件主题")} rules={[{ required: true }]}>
                    <Input maxLength={120} />
                  </Form.Item>
                  <Form.Item name="body" label={tr("邮件正文")} rules={[{ required: true }]}>
                    <Input.TextArea rows={9} maxLength={6000} />
                  </Form.Item>
                  <Form.Item name="button" label={tr("按钮文字")}>
                    <Input maxLength={100} />
                  </Form.Item>
                  <Space wrap>
                    <Button onClick={preview} loading={busy} disabled={!chosen}>
                      {tr("预览")}
                    </Button>
                    <Button type="primary" htmlType="submit" loading={busy} disabled={!chosen}>
                      {tr("发布新版本")}
                    </Button>
                  </Space>
                </Form>
                <p>
                  {tr("当前模板版本")}：{chosen?.version || tr("内置版本")}
                </p>
                {html && (
                  <iframe
                    title={tr("邮件预览")}
                    srcDoc={html}
                    sandbox=""
                    style={{
                      width: "100%",
                      height: 460,
                      border: "1px solid #dce3ed",
                      borderRadius: 8,
                    }}
                  />
                )}
                <Table
                  rowKey="version"
                  size="small"
                  dataSource={chosen?.history || []}
                  pagination={{ pageSize: 5 }}
                  scroll={{ x: 450 }}
                  columns={[
                    { title: tr("版本"), dataIndex: "version" },
                    {
                      title: tr("发布时间"),
                      dataIndex: "created_at",
                      render: (v) =>
                        dateTimeFormatter({ dateStyle: "medium", timeStyle: "short" }).format(
                          new Date(v),
                        ),
                    },
                    {
                      title: tr("操作"),
                      render: (_, v) => (
                        <Popconfirm
                          title={tr("将此历史版本发布为新版本？")}
                          onConfirm={() =>
                            action("/platform/mail-templates", {
                              kind: selected,
                              locale: lang,
                              base_version: chosen?.version || 0,
                              restore_version: v.version,
                            })
                          }
                        >
                          <Button disabled={busy || v.version === chosen?.version}>
                            {tr("回滚")}
                          </Button>
                        </Popconfirm>
                      ),
                    },
                  ]}
                />
              </Card>
            ),
          },
          {
            key: "monitor",
            label: tr("投递监控"),
            children: (
              <Card extra={<Button onClick={() => setRevision((v) => v + 1)}>{tr("刷新")}</Button>}>
                <Space wrap>
                  {["queued", "accepted", "failed", "unknown", "overdue"].map((key) => (
                    <Statistic
                      key={key}
                      title={tr(
                        {
                          queued: "排队中",
                          accepted: "邮件服务已受理",
                          failed: "失败",
                          unknown: "结果不确定",
                          overdue: "等待过久",
                        }[key] || key,
                      )}
                      value={monitor?.stats[key] || 0}
                    />
                  ))}
                </Space>
                <NoticeTable items={monitor?.items || []} platform />
                <Table<Notice>
                  rowKey="id"
                  dataSource={
                    monitor?.items.filter(
                      (n) =>
                        n.status === "failed" &&
                        ["temporary", "transport", "configuration"].includes(n.failure_kind),
                    ) || []
                  }
                  size="small"
                  pagination={{ pageSize: 5 }}
                  columns={[
                    { title: tr("收件人"), dataIndex: "recipient" },
                    {
                      title: tr("操作"),
                      render: (_, n) => (
                        <Button
                          loading={busy}
                          onClick={() => action("/platform/notifications/" + n.id + "/retry", {})}
                        >
                          {tr("重试")}
                        </Button>
                      ),
                    },
                  ]}
                />
                <h3>{tr("认证邮件记录")}</h3>
                <NoticeTable
                  items={(monitor?.authentication || []).map((n) => ({
                    ...n,
                    kind: "code_" + n.purpose,
                  }))}
                  platform
                />
              </Card>
            ),
          },
          {
            key: "test",
            label: tr("本人测试邮件"),
            children: (
              <Card>
                <p>
                  {tr("测试只发当前管理员已验证邮箱，不支持指定其他收件人。")} {current.user.email}
                </p>
                <Form
                  form={test}
                  layout="vertical"
                  initialValues={{ locale, timezone: currentPreferences().timezone }}
                  onFinish={(v) => action("/platform/mail-test", v)}
                >
                  <Form.Item name="locale" label={tr("语言")} rules={[{ required: true }]}>
                    <Select options={languages} />
                  </Form.Item>
                  <Form.Item name="timezone" label={tr("时区")} rules={[{ required: true }]}>
                    <Input placeholder="Asia/Shanghai" />
                  </Form.Item>
                  <Button type="primary" htmlType="submit" loading={busy}>
                    {tr("发送本人测试邮件")}
                  </Button>
                </Form>
              </Card>
            ),
          },
          {
            key: "announcement",
            label: tr("订阅公告"),
            children: (
              <Card>
                <Alert
                  type="info"
                  title={tr("仅向主动订阅产品更新的账号发送，不向访客或未订阅用户群发。")}
                />
                <Form
                  form={announcement}
                  layout="vertical"
                  onFinish={(v) => action("/platform/mail-announcement", v)}
                >
                  <Form.Item
                    name="key"
                    label={tr("唯一发布编号")}
                    rules={[{ required: true, min: 8, max: 100 }]}
                  >
                    <Input />
                  </Form.Item>
                  <p>{tr("更新摘要")}</p>
                  {languages.map((language) => (
                    <Form.Item
                      key={language.value}
                      name={["translations", language.value]}
                      label={language.label}
                      rules={[{ required: true, max: 200 }]}
                    >
                      <Input.TextArea rows={2} maxLength={200} />
                    </Form.Item>
                  ))}
                  <Button type="primary" htmlType="submit" loading={busy}>
                    {tr("发布订阅公告")}
                  </Button>
                </Form>
              </Card>
            ),
          },
        ]}
      />
    </section>
  );
}
