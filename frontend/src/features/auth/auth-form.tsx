/** 文件作用说明：实现真实邮箱注册、验证码与密码登录、密码重置和受邀激活流程。 */
"use client";
import { Brand } from "@/components/brand";
import Link from "@/i18n/link";
import { usePreferences } from "@/i18n/locale-provider";
import { availableTimezones } from "@/i18n/preferences";
import { localizeStatic } from "@/i18n/runtime";
import { useTranslator } from "@/i18n/use-translator";
import { api, errorMessage } from "@/lib/api";
import { safeReturnPath } from "@/lib/urls";
import {
  ArrowLeftOutlined,
  ArrowRightOutlined,
  LinkOutlined,
  MailOutlined,
  SkinOutlined,
  TeamOutlined,
} from "@ant-design/icons";
import { Alert, App, Button, Checkbox, Form, Input, Segmented, Select, Space } from "antd";
import Image from "next/image";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
type Mode = "login" | "register" | "reset" | "invitation";
type Values = {
  email: string;
  password?: string;
  code?: string;
  tenant_name?: string;
  timezone?: string;
  display_name?: string;
  agreement?: boolean;
  confirm_password?: string;
};
const labels = {
  login: ["欢迎回来", "登录你的工作空间，继续接住每一次咨询。"],
  register: ["创建你的工作空间", "验证邮箱后，你将成为新租户的所有者与管理员。"],
  reset: ["重置登录密码", "通过邮箱验证后设置新密码。"],
  invitation: ["加入团队", "使用受邀邮箱验证，加入邀请对应的工作空间。"],
};
/** AuthForm 根据页面模式提交身份接口，并准确展示发信与错误状态。 */
export function AuthForm({ mode }: { mode: Mode }) {
  const tr = useTranslator();
  const preferences = usePreferences();
  const [form] = Form.useForm<Values>();
  useEffect(() => {
    if (mode === "register" && !form.isFieldTouched("timezone"))
      form.setFieldsValue({ timezone: preferences.timezone });
  }, [form, mode, preferences.timezone]);
  const [loginMethod, setLoginMethod] = useState("password");
  const [pending, setPending] = useState(false);
  const [sending, setSending] = useState(false);
  const [cooldown, setCooldown] = useState(0);
  const [error, setError] = useState("");
  const [delivery, setDelivery] = useState("");
  const { message } = App.useApp();
  const router = useRouter();
  const query = useSearchParams();
  const needsCode = mode !== "login" || loginMethod === "code";
  const needsPassword = mode !== "login" || loginMethod === "password";
  const invitationToken = query.get("token");
  useEffect(() => {
    if (!cooldown) return;
    const timer = window.setTimeout(() => setCooldown((value) => value - 1), 1000);
    return () => window.clearTimeout(timer);
  }, [cooldown]);
  /** sendCode 校验邮箱后申请对应用途验证码，不在前端生成或展示验证码。 */
  async function sendCode() {
    setError("");
    setDelivery("");
    try {
      await form.validateFields(["email"]);
      setSending(true);
      const email = form.getFieldValue("email").trim();
      const purpose =
        mode === "reset"
          ? "reset_password"
          : mode === "invitation"
            ? "invitation"
            : mode === "register"
              ? "register"
              : "login";
      const result = await api<{
        delivery_status: string;
        retry_after: number;
        expires_in: number;
      }>("/auth/send-code", { method: "POST", bodyJSON: { email, purpose } });
      setCooldown(result.retry_after || 60);
      setDelivery(
        result.delivery_status === "accepted"
          ? tr("邮件服务器已受理，请查看收件箱或垃圾邮件目录。验证码 5 分钟内有效。")
          : tr("发送请求已处理，请核对邮件状态后获取验证码。"),
      );
    } catch (problem) {
      if (!(problem && typeof problem === "object" && "errorFields" in problem))
        setError(errorMessage(problem));
    } finally {
      setSending(false);
    }
  }
  /** submit 根据用户选择完成身份操作，只有接口成功才进入控制台。 */
  async function submit(values: Values) {
    setPending(true);
    setError("");
    const email = values.email.trim();
    try {
      if (mode === "reset") {
        await api("/auth/reset-password", {
          method: "POST",
          bodyJSON: { email, code: values.code, password: values.password },
        });
        message.success(tr("密码已更新，请重新登录。"));
        router.replace("/login");
        return;
      }
      if (mode === "invitation") {
        if (!invitationToken)
          throw new Error(tr("邀请链接缺少凭证，请向团队管理员重新获取邀请邮件。"));
        await api("/auth/accept-invitation", {
          method: "POST",
          bodyJSON: {
            token: invitationToken,
            code: values.code,
            password: values.password,
            display_name: values.display_name,
          },
        });
      } else if (mode === "register") {
        await api("/auth/register", {
          method: "POST",
          bodyJSON: {
            email,
            code: values.code,
            password: values.password,
            tenant_name: values.tenant_name,
            timezone: values.timezone,
            display_name: values.display_name,
          },
        });
      } else {
        await api(loginMethod === "code" ? "/auth/login-code" : "/auth/login", {
          method: "POST",
          bodyJSON: { email, password: values.password, code: values.code },
        });
      }
      window.dispatchEvent(new Event("goodchat:authenticated"));
      router.replace(safeReturnPath(query.get("next")));
      router.refresh();
    } catch (problem) {
      setError(errorMessage(problem));
    } finally {
      setPending(false);
    }
  }
  return (
    <div className={`auth-wrap auth-refresh auth-mode-${mode}`}>
      <aside className="auth-intro">
        <Image
          className="auth-scene"
          src="/images/cobalt-communication-rings.png"
          alt=""
          width={1513}
          height={1039}
          sizes="45vw"
        />
        <Brand />
        <div className="auth-intro-content">
          <div className="public-overline">GOODCHAT · CONNECT</div>
          <h1>
            {tr("连接每一位客户。 ")}
            <br />
            {tr("即刻回应每次期待。 ")}
          </h1>
          <p className="muted">{tr("连接每一位客户，也连接你和团队。")}</p>
          <div className="auth-conversation-preview" aria-label={tr("客户沟通界面示意")}>
            <div className="auth-preview-heading">
              <span className="auth-preview-avatar">{tr("顾")}</span>
              <div>
                <strong>{tr("你的品牌 \u00B7 客户服务")}</strong>
                <small>
                  <i />
                  {tr("在线，随时为你解答 ")}
                </small>
              </div>
              <span className="auth-preview-more">···</span>
            </div>
            <div className="auth-preview-message">
              <span>
                {tr("你好，很高兴遇见你。 ")}
                <br />
                {tr("有什么可以帮到你？ ")}
              </span>
            </div>
            <div className="auth-preview-message own">
              <span>{tr("想了解一下你们的产品。")}</span>
            </div>
            <div className="auth-preview-input">
              <span>{tr("从一句你好开始\u2026")}</span>
              <ArrowRightOutlined />
            </div>
            <small className="auth-preview-caption">{tr("客户沟通界面示意")}</small>
          </div>
          <div className="auth-capabilities">
            <div>
              <LinkOutlined />
              <span>
                <strong>{tr("多链接接入")}</strong>
                <small>{tr("独立入口连接不同业务，让咨询有序进入团队。")}</small>
              </span>
            </div>
            <div>
              <TeamOutlined />
              <span>
                <strong>{tr("灵活分配")}</strong>
                <small>{tr("按团队规则分配咨询，让合适的人提供服务。")}</small>
              </span>
            </div>
            <div>
              <SkinOutlined />
              <span>
                <strong>{tr("品牌装修")}</strong>
                <small>{tr("用组件、代码或 AI 创作自己的聊天页面。")}</small>
              </span>
            </div>
          </div>
        </div>
        <p className="auth-free-note">
          <span>{tr("2 个免费客服坐席")}</span>
          <i />
          {tr("基础服务长期免费 ")}
        </p>
      </aside>
      <main className="auth-form-side">
        <div className="auth-form">
          <div className="auth-form-brand">
            <Brand />
          </div>
          <h1>{localizeStatic(labels, tr)[mode][0]}</h1>
          <p className="auth-subtitle">{localizeStatic(labels, tr)[mode][1]}</p>
          {mode === "login" && (
            <Segmented
              block
              value={loginMethod}
              onChange={setLoginMethod}
              options={[
                { label: tr("密码登录"), value: "password" },
                { label: tr("验证码登录"), value: "code" },
              ]}
              style={{ marginBottom: 24 }}
            />
          )}
          <Space
            orientation="vertical"
            style={{ width: "100%", marginBottom: error || delivery ? 20 : 0 }}
          >
            {error && <Alert type="error" title={error} showIcon />}
            {delivery && <Alert type="info" title={delivery} showIcon />}
            {mode === "invitation" && !invitationToken && (
              <Alert type="warning" title={tr("邀请链接不完整，请从邀请邮件重新打开。")} showIcon />
            )}
          </Space>
          <Form
            form={form}
            layout="vertical"
            onFinish={submit}
            requiredMark={false}
            autoComplete="on"
          >
            {mode === "register" && (
              <Form.Item
                name="tenant_name"
                label={tr("工作空间名称")}
                rules={[
                  {
                    required: true,
                    whitespace: true,
                    max: 100,
                    message: tr("请填写 100 字以内的名称"),
                  },
                ]}
              >
                <Input placeholder={tr("公司、品牌或团队名称")} />
              </Form.Item>
            )}
            {mode === "register" && (
              <Form.Item
                name="timezone"
                label={tr("企业营业与统计时区")}
                extra={tr("仅用于企业营业时间和统计，成员可单独设置个人时区。")}
                rules={[{ required: true }]}
              >
                <Select
                  showSearch
                  options={availableTimezones(preferences.timezone).map((timezone) => ({
                    value: timezone,
                    label: timezone.replaceAll("_", " "),
                  }))}
                />
              </Form.Item>
            )}
            {["register", "invitation"].includes(mode) && (
              <Form.Item
                name="display_name"
                label={tr("你的称呼")}
                rules={[
                  { required: true, whitespace: true, max: 50, message: tr("请填写你的称呼") },
                ]}
              >
                <Input placeholder={tr("用于团队内显示")} autoComplete="name" />
              </Form.Item>
            )}
            <Form.Item
              name="email"
              label={mode === "invitation" ? tr("受邀邮箱") : tr("邮箱")}
              rules={[
                { required: true, message: tr("请填写邮箱") },
                { type: "email", message: tr("请填写有效邮箱") },
              ]}
            >
              <Input
                prefix={<MailOutlined className="muted" />}
                type="email"
                autoComplete="email"
                placeholder="name@company.com"
                onChange={() => setDelivery("")}
              />
            </Form.Item>
            {needsCode && (
              <Form.Item label={tr("邮箱验证码")} required>
                <div className="code-row">
                  <Form.Item
                    name="code"
                    noStyle
                    rules={[
                      { required: true, message: tr("请填写验证码") },
                      { pattern: /^\d{6}$/, message: tr("请输入 6 位数字验证码") },
                    ]}
                  >
                    <Input
                      autoComplete="one-time-code"
                      inputMode="numeric"
                      maxLength={6}
                      placeholder={tr("6 位验证码")}
                    />
                  </Form.Item>
                  <Button onClick={sendCode} loading={sending} disabled={cooldown > 0}>
                    {cooldown > 0
                      ? tr("{p0} 秒后重发", { p0: String(cooldown) })
                      : tr("获取验证码")}
                  </Button>
                </div>
              </Form.Item>
            )}
            {needsPassword && (
              <Form.Item
                name="password"
                label={
                  mode === "login"
                    ? tr("密码")
                    : mode === "invitation"
                      ? tr("新账号密码")
                      : tr("设置密码")
                }
                rules={[
                  { required: mode !== "invitation", message: tr("请填写密码") },
                  // 登录只校验输入边界；账号密码的创建强度由注册、重置和服务端分别约束。
                  ...(mode === "login"
                    ? [{ max: 128, message: tr("密码最多 128 位") }]
                    : [{ min: 8, max: 128, message: tr("密码需为 8 至 128 位") }]),
                ]}
                extra={
                  mode === "invitation"
                    ? tr("新邮箱需要设置密码；已有账号密码保持不变。")
                    : undefined
                }
              >
                <Input.Password
                  autoComplete={mode === "login" ? "current-password" : "new-password"}
                  placeholder={mode === "login" ? tr("输入密码") : tr("至少 8 位，支持密码管理器")}
                />
              </Form.Item>
            )}
            {["register", "reset"].includes(mode) && (
              <Form.Item
                name="confirm_password"
                label={tr("确认密码")}
                dependencies={["password"]}
                rules={[
                  { required: true, message: tr("请再次输入密码") },
                  ({ getFieldValue }) => ({
                    validator(_, value) {
                      return !value || getFieldValue("password") === value
                        ? Promise.resolve()
                        : Promise.reject(new Error(tr("两次密码不一致")));
                    },
                  }),
                ]}
              >
                <Input.Password autoComplete="new-password" placeholder={tr("再次输入密码")} />
              </Form.Item>
            )}
            {mode === "register" && (
              <Form.Item
                name="agreement"
                valuePropName="checked"
                rules={[
                  {
                    validator: (_, value) =>
                      value
                        ? Promise.resolve()
                        : Promise.reject(new Error(tr("请先阅读并同意服务条款和隐私政策"))),
                  },
                ]}
              >
                <Checkbox>
                  {tr("我已阅读并同意")}{" "}
                  <Link href="/terms" target="_blank">
                    {tr("服务条款 ")}
                  </Link>{" "}
                  {tr("和")}{" "}
                  <Link href="/privacy" target="_blank">
                    {tr("隐私政策 ")}
                  </Link>
                </Checkbox>
              </Form.Item>
            )}
            <Button
              type="primary"
              htmlType="submit"
              block
              size="large"
              loading={pending}
              disabled={mode === "invitation" && !invitationToken}
              icon={<ArrowRightOutlined />}
              iconPlacement="end"
            >
              {mode === "register"
                ? tr("创建工作空间")
                : mode === "reset"
                  ? tr("更新密码")
                  : mode === "invitation"
                    ? tr("验证并加入团队")
                    : tr("登录工作空间")}
            </Button>
          </Form>
          <div className="auth-links">
            {mode === "login" ? (
              <>
                <Link href="/register">{tr("免费注册")}</Link>
                <Link href="/reset-password">{tr("忘记密码")}</Link>
              </>
            ) : (
              <>
                <Link href="/login">{tr("返回登录")}</Link>
                <Link href="/">{tr("返回官网")}</Link>
              </>
            )}
          </div>
          {mode === "login" && (
            <div className="auth-home-link">
              <Link href="/">
                <ArrowLeftOutlined />
                {tr(" 返回官网 ")}
              </Link>
            </div>
          )}
        </div>
      </main>
    </div>
  );
}
