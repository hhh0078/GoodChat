/** 文件作用说明：提供桌面密码与邮箱验证码登录，不存储密码。 */
import { useEffect, useState } from "react";
import { Alert, Button, Form, Input } from "antd";
import { CustomerServiceOutlined } from "@ant-design/icons";
import { api, errorMessage } from "@/lib/api";

/** Login 使用现有密码及邮箱验证码接口，不保存密码、不内置万能码。 */
export function Login({
  server,
  busy: parentBusy,
  onLogin,
  onChangeServer,
}: {
  server: string;
  busy: boolean;
  onLogin: () => Promise<void>;
  onChangeServer: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [mode, setMode] = useState<"password" | "code">("password");
  const [seconds, setSeconds] = useState(0);
  const [form] = Form.useForm();
  useEffect(() => {
    if (!seconds) return;
    const timer = setTimeout(() => setSeconds(seconds - 1), 1000);
    return () => clearTimeout(timer);
  }, [seconds]);
  /** submit 等服务器登录确认后重新读取有效身份。 */
  async function submit(values: Record<string, string>) {
    setBusy(true);
    setError("");
    try {
      await api(mode === "password" ? "/auth/login" : "/auth/login-code", {
        method: "POST",
        bodyJSON: values,
      });
      await onLogin();
    } catch (problem) {
      setError(errorMessage(problem));
    } finally {
      setBusy(false);
    }
  }
  /** sendCode 仅在明确操作时发送真实登录验证码。 */
  async function sendCode() {
    try {
      const { email } = await form.validateFields(["email"]);
      setBusy(true);
      await api("/auth/send-code", {
        method: "POST",
        bodyJSON: { email, purpose: "login" },
      });
      setSeconds(60);
      setError("");
    } catch (problem) {
      if (problem instanceof Error) setError(errorMessage(problem));
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="desktop-entry">
      <section className="desktop-connect-card">
        <div className="desktop-entry-icon">
          <CustomerServiceOutlined />
        </div>
        <span className="desktop-eyebrow">欢迎回到工作台</span>
        <h1>登录 GoodChat</h1>
        <p className="desktop-description desktop-login-server" title={server}>
          {server}
        </p>
        {error && <Alert type="error" showIcon title={error} />}
        <Form
          form={form}
          layout="vertical"
          onFinish={submit}
          requiredMark={false}
        >
          <Form.Item
            label="邮箱"
            name="email"
            rules={[
              { required: true, type: "email", message: "请输入有效邮箱" },
            ]}
          >
            <Input size="large" autoComplete="username" autoFocus />
          </Form.Item>
          {mode === "password" ? (
            <Form.Item
              label="密码"
              name="password"
              rules={[{ required: true, message: "请输入密码" }]}
            >
              <Input.Password size="large" autoComplete="current-password" />
            </Form.Item>
          ) : (
            <Form.Item
              label="邮箱验证码"
              name="code"
              rules={[
                {
                  required: true,
                  pattern: /^\d{6}$/,
                  message: "请输入六位验证码",
                },
              ]}
            >
              <Input
                size="large"
                inputMode="numeric"
                autoComplete="one-time-code"
                suffix={
                  <Button
                    type="link"
                    size="small"
                    disabled={!!seconds || busy}
                    onClick={() => void sendCode()}
                  >
                    {seconds ? `${seconds} 秒后重发` : "获取验证码"}
                  </Button>
                }
              />
            </Form.Item>
          )}
          <Button
            type="primary"
            size="large"
            block
            htmlType="submit"
            loading={busy || parentBusy}
          >
            登录工作台
          </Button>
        </Form>
        <div className="desktop-login-links">
          <Button
            type="link"
            onClick={() => {
              setMode(mode === "password" ? "code" : "password");
              setError("");
            }}
          >
            {mode === "password" ? "使用验证码登录" : "使用密码登录"}
          </Button>
          <Button type="link" onClick={onChangeServer}>
            切换服务器
          </Button>
        </div>
      </section>
    </main>
  );
}
