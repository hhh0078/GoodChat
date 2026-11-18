/** 文件作用说明：配置个人机器人、授权知识话术、时段预算及计费调试，管理员另设默认接待策略。 */
"use client";
import { ErrorNotice } from "@/components/page-state";
import { useSession } from "@/features/auth/session";
import { languageNames } from "@/i18n/preferences";
import { useTranslator } from "@/i18n/use-translator";
import { api, errorMessage } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import {
  Alert,
  Avatar,
  App,
  Button,
  Card,
  Checkbox,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Spin,
  Switch,
} from "antd";
import { useLocale } from "next-intl";
import { useEffect, useState } from "react";
import { aiErrorMessage } from "./errors";
import { AIPlanPreview } from "./preview";
import { aiLanguages, type AIBot, type AIContent, type AIJob, type AIModel } from "./types";

/** AIBotSettingsPage 加载本成员设置并在语言切换后重新读取资源目录。 */
export function AIBotSettingsPage() {
  const tr = useTranslator();
  const { message } = App.useApp();
  const { tenantId, current } = useSession();
  const locale = useLocale();
  const [form] = Form.useForm();
  const knowledge = useResource<AIContent>("/tenant/ai/knowledge");
  const scripts = useResource<AIContent>("/tenant/ai/scripts");
  const models = useResource<AIModel>("/tenant/ai/models");
  const [bot, setBot] = useState<AIBot | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const [question, setQuestion] = useState("");
  const [jobID, setJobID] = useState("");
  const [job, setJob] = useState<AIJob | null>(null);
  const member = current.memberships.find((m) => m.tenant_id === tenantId);
  const manages = member?.role === "owner" || member?.role === "admin";
  useEffect(() => {
    const controller = new AbortController();
    setError(null);
    api<AIBot>("/me/ai/bot", { tenantId, signal: controller.signal })
      .then((value) => {
        if (!controller.signal.aborted) {
          setBot(value);
          form.setFieldsValue({
            ...value,
            model_id: value.model_id || "",
            settings: {
              ...value.settings,
              daily_budget_micro: value.settings.daily_budget_micro / 1000000,
            },
          });
        }
      })
      .catch((e) => {
        if (!controller.signal.aborted) setError(e);
      });
    return () => controller.abort();
  }, [tenantId, locale, revision, form]);
  useEffect(() => {
    if (!jobID) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    /** poll 跟随持久任务直到真实完成，离开页面取消轮询。 */
    async function poll() {
      try {
        const value = await api<AIJob>(`/tenant/ai/jobs/${jobID}`, {
          tenantId,
          signal: controller.signal,
        });
        if (controller.signal.aborted) return;
        setJob(value);
        if (["queued", "running"].includes(value.status)) timer = setTimeout(poll, 1500);
      } catch (e) {
        if (!controller.signal.aborted) setError(e);
      }
    }
    void poll();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [tenantId, jobID]);
  /** save 固定资源编号及个人版本，服务端再次校验所有权。 */
  async function save() {
    try {
      const values = await form.validateFields();
      setBusy(true);
      const next = await api<AIBot>("/me/ai/bot", {
        method: "PUT",
        tenantId,
        bodyJSON: {
          ...values,
          version: bot?.version || 0,
          settings: {
            ...values.settings,
            daily_budget_micro: Math.round(values.settings.daily_budget_micro * 1000000),
          },
        },
      });
      setBot(next);
      message.success(tr("已保存"));
    } catch (e) {
      if (!(e && typeof e === "object" && "errorFields" in e)) message.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  /** test 使用已保存的机器人配置创建计费调试任务。 */
  async function test() {
    if (!question.trim()) return;
    setBusy(true);
    setJob(null);
    try {
      const result = await api<{ id: string }>("/me/ai/test", {
        method: "POST",
        tenantId,
        bodyJSON: { text: question },
      });
      setJobID(result.id);
    } catch (e) {
      message.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  if (!bot)
    return (
      <>
        <ErrorNotice error={error} retry={() => setRevision((v) => v + 1)} />
        {!error && <Spin />}
      </>
    );
  const weekdays = [
    tr("周日"),
    tr("周一"),
    tr("周二"),
    tr("周三"),
    tr("周四"),
    tr("周五"),
    tr("周六"),
  ];
  return (
    <div className="ai-bot-grid">
      <Card title={tr("我的机器人")}>
        <ErrorNotice error={error} retry={() => setRevision((v) => v + 1)} />
        <ErrorNotice
          error={knowledge.error || scripts.error || models.error}
          retry={() => {
            knowledge.reload();
            scripts.reload();
            models.reload();
          }}
        />
        <Form form={form} layout="vertical" disabled={busy}>
          <div className="ai-form-grid">
            <Form.Item
              name="name"
              label={tr("机器人名称")}
              rules={[{ required: true, whitespace: true }]}
            >
              <Input maxLength={60} />
            </Form.Item>
            <Form.Item name="mode" label={tr("工作模式")}>
              <Select
                options={[
                  { value: "off", label: tr("关闭") },
                  { value: "assistant", label: tr("辅助回复") },
                  { value: "auto", label: tr("自动回复") },
                ]}
              />
            </Form.Item>
          </div>
          <Form.Item name="avatar_ref" label={tr("头像")}>
            <Select
              options={[
                {
                  value: "goodchat",
                  label: (
                    <Space>
                      <Avatar size={24} src="/logo/goodchat-mark.svg" />
                      GoodChat
                    </Space>
                  ),
                },
                ...Array.from({ length: 50 }, (_, i) => {
                  const value = `default-${String(i + 1).padStart(2, "0")}`;
                  return {
                    value,
                    label: (
                      <Space>
                        <Avatar size={24} src={`/images/avatars/${value}.png`} />
                        {i + 1}
                      </Space>
                    ),
                  };
                }),
              ]}
            />
          </Form.Item>
          <Form.Item name="model_id" label={tr("AI模型")}>
            <Select
              options={[
                { value: "", label: tr("平台默认模型") },
                ...models.items.map((m) => ({
                  value: m.id,
                  label: m.name + (m.supports_images ? " · " + tr("图片") : ""),
                })),
              ]}
            />
          </Form.Item>
          <Form.Item name="prompt" label={tr("服务角色与回答要求")}>
            <Input.TextArea rows={3} maxLength={1200} />
          </Form.Item>
          <Form.Item name="language" label={tr("回复语言")}>
            <Select
              options={[
                { value: "auto", label: tr("跟随访客语言") },
                ...aiLanguages.map((value) => ({ value, label: languageNames[value] })),
              ]}
            />
          </Form.Item>
          <Form.Item
            name="knowledge_ids"
            label={tr("可用知识库")}
            extra={tr("AI先查看描述，再按需读取正文。未勾选的知识库不可读取。")}
          >
            <Select
              mode="multiple"
              options={knowledge.items
                .filter((k) => k.enabled)
                .map((k) => ({ value: k.id, label: k.title }))}
            />
          </Form.Item>
          <Form.Item name="script_ids" label={tr("可用AI话术")}>
            <Select
              mode="multiple"
              options={scripts.items
                .filter((s) => s.enabled)
                .map((s) => ({ value: s.id, label: s.title }))}
            />
          </Form.Item>
          <div className="ai-form-grid">
            <Form.Item
              name={["settings", "start"]}
              label={tr("接待开始时间")}
              rules={[{ required: true }]}
            >
              <Input type="time" />
            </Form.Item>
            <Form.Item
              name={["settings", "end"]}
              label={tr("接待结束时间")}
              rules={[{ required: true }]}
            >
              <Input type="time" />
            </Form.Item>
          </div>
          <Form.Item
            name={["settings", "weekdays"]}
            label={tr("接待日期")}
            rules={[{ required: true }]}
          >
            <Checkbox.Group options={weekdays.map((label, value) => ({ label, value }))} />
          </Form.Item>
          <p className="ai-help">
            {tr("接待时段和每日额度使用租户业务时区，个人显示时区不影响执行。")}
          </p>
          <div className="ai-form-grid">
            <Form.Item
              name={["settings", "daily_budget_micro"]}
              label={tr("每日预算（元）")}
              extra={tr("0 表示不限制每日金额，默认不设上限。")}
              rules={[{ required: true }]}
            >
              <InputNumber min={0} max={1000000} precision={6} />
            </Form.Item>
            <Form.Item
              name={["settings", "max_output_tokens"]}
              label={tr("单次最大输出Token")}
              rules={[{ required: true }]}
            >
              <InputNumber min={128} max={4096} />
            </Form.Item>
            <Form.Item
              name={["settings", "max_turns"]}
              label={tr("最多连续自动消息数")}
              rules={[{ required: true }]}
            >
              <InputNumber min={1} max={50} />
            </Form.Item>
            <Form.Item
              name={["settings", "offline"]}
              label={tr("人工离线时继续接待")}
              valuePropName="checked"
            >
              <Switch />
            </Form.Item>
          </div>
          <Button type="primary" loading={busy} onClick={() => void save()}>
            {tr("保存机器人")}
          </Button>
        </Form>
      </Card>
      <div>
        <Card title={tr("调试机器人")}>
          <Alert
            type="info"
            showIcon
            title={tr("调试使用已保存的配置，会产生AI费用。辅助回复即使未发送也会产生费用。")}
          />
          <Input.TextArea
            aria-label={tr("调试问题")}
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            rows={4}
            maxLength={1200}
          />
          <Button
            onClick={() => void test()}
            disabled={
              !bot.id || !question.trim() || (!!job && ["queued", "running"].includes(job.status))
            }
            loading={busy}
          >
            {tr("开始调试")}
          </Button>
          {job && (
            <div className="ai-test-result">
              <p>
                {["queued", "running"].includes(job.status)
                  ? tr("生成中")
                  : job.status === "succeeded"
                    ? tr("已完成")
                    : job.status === "uncertain"
                      ? tr("用量待核对")
                      : tr("生成失败")}
              </p>
              {job.error_code && (
                <Alert type="warning" showIcon title={aiErrorMessage(job.error_code, tr)} />
              )}
              {job.result && <AIPlanPreview plan={job.result} />}
              <p>
                {tr("输入 / 输出Token：{p0} / {p1}", {
                  p0: job.input_tokens,
                  p1: job.output_tokens,
                })}
              </p>
            </div>
          )}
        </Card>
        {manages && <AIDefaultReception key={bot.version} />}
      </div>
    </div>
  );
}

/** AIDefaultReception 仅租户管理员可选择默认机器人和AI优先接待策略。 */
function AIDefaultReception() {
  const tr = useTranslator();
  const { tenantId } = useSession();
  const { message } = App.useApp();
  const bots = useResource<AIBot>("/tenant/ai/bots");
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const locale = useLocale();
  useEffect(() => {
    const controller = new AbortController();
    api<{ default_bot_id: string | null; reception_mode: string }>("/tenant/ai/settings", {
      tenantId,
      signal: controller.signal,
    })
      .then((value) => {
        if (!controller.signal.aborted)
          form.setFieldsValue({ ...value, default_bot_id: value.default_bot_id || "" });
      })
      .catch((e) => {
        if (!controller.signal.aborted) setError(e);
      });
    return () => controller.abort();
  }, [tenantId, locale, form]);
  /** save 更新租户入口的默认AI接待策略。 */
  async function save() {
    setBusy(true);
    try {
      await api("/tenant/ai/settings", {
        method: "PUT",
        tenantId,
        bodyJSON: await form.validateFields(),
      });
      message.success(tr("已保存"));
    } catch (e) {
      message.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Card title={tr("默认接待策略")}>
      <ErrorNotice error={error || bots.error} />
      <Form
        form={form}
        layout="vertical"
        initialValues={{ default_bot_id: "", reception_mode: "human_first" }}
      >
        <Form.Item name="reception_mode" label={tr("接待方式")}>
          <Select
            options={[
              { value: "human_first", label: tr("人工优先") },
              { value: "ai_first", label: tr("AI优先") },
            ]}
          />
        </Form.Item>
        <Form.Item name="default_bot_id" label={tr("默认接待机器人")}>
          <Select
            options={[
              { value: "", label: tr("不使用默认机器人") },
              ...bots.items
                .filter((b) => b.mode === "auto")
                .map((b) => ({ value: b.id, label: b.name })),
            ]}
          />
        </Form.Item>
        <Button loading={busy} onClick={() => void save()}>
          {tr("保存")}
        </Button>
      </Form>
    </Card>
  );
}
