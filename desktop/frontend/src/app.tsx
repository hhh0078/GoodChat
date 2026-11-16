/** 文件作用说明：提供桌面服务器选择、账号登录、工作空间切换、发送队列与复用客服工作台。 */
import { AgentWorkspace } from "@/features/chat/workspace";
import { localizeStatic } from "@/i18n/runtime";
import { useTranslator } from "@/i18n/use-translator";
import { api, ApiError, errorMessage } from "@/lib/api";
import type { CurrentUser } from "@/lib/types";
import {
  CloudServerOutlined,
  CustomerServiceOutlined,
  LogoutOutlined,
  ReloadOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import {
  Alert,
  App as AntApp,
  Button,
  Drawer,
  Empty,
  Form,
  Input,
  Select,
  Space,
  Spin,
  Tag,
  Typography,
} from "antd";
import { useEffect, useState } from "react";
import { switchDraftServer } from "./drafts";
import { Login } from "./login";
import {
  invoke,
  message,
  on,
  type Pending,
  type Startup,
  type State,
} from "./native";
import { WorkspaceRemindersProvider } from "./reminders";
import { DesktopSessionProvider } from "./session";
import { UpdateRequired, type Update } from "./update-required";
const labels: Record<string, string> = {
  idle: "尚未连接",
  connecting: "连接中",
  recovering: "正在恢复",
  connected: "消息已同步",
  reconnecting: "正在重连",
  denied: "身份失效",
  offline: "网络断开",
};
/** DesktopApp 将服务器确认置于登录之前，每次启动都保留用户选择权。 */
export default function DesktopApp() {
  const tr = useTranslator();
  const [serverForm] = Form.useForm();
  const [update, setUpdate] = useState<Update | null>(null);
  const [startup, setStartup] = useState<Startup | null>(null);
  const [server, setServer] = useState("");
  const [choosing, setChoosing] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [current, setCurrent] = useState<CurrentUser | null>(null);
  const [tenant, setTenant] = useState("");
  const [ready, setReady] = useState(false);
  const [state, setState] = useState<State>({
    status: "idle",
    detail: "",
    pending: 0,
    failed: 0,
  });
  const [pending, setPending] = useState<Pending[]>([]);
  const [queueOpen, setQueueOpen] = useState(false);
  const { modal, message: toast } = AntApp.useApp();
  useEffect(() => {
    void invoke<Startup>("Startup")
      .then((value) => {
        setStartup(value);
        setServer(value.server);
      })
      .catch((problem) => setError(message(problem)));
    return on("desktop:select-server", requestServer);
  }, []);
  useEffect(() => {
    const cleanups = [
      on<State>("desktop:state", setState),
      on<string>("desktop:notification-error", (value) => setError(value)),
    ];
    return () => cleanups.forEach((cleanup) => cleanup());
  }, []);
  useEffect(() => {
    if (!current || !tenant) {
      setReady(false);
      return;
    }
    let cancelled = false;
    setReady(false);
    void invoke("SetIdentity", tenant)
      .then(async () => {
        if (cancelled) return;
        setState(await invoke<State>("Status"));
        setReady(true);
      })
      .catch((problem) => {
        if (!cancelled) setError(message(problem));
      });
    return () => {
      cancelled = true;
    };
  }, [current?.user.id, tenant]);
  useEffect(() => {
    if (!queueOpen) return;
    let cancelled = false;
    /** refresh 只读取当前服务器和身份的队列，不把正文写入日志。 */
    async function refresh() {
      try {
        const items = await invoke<Pending[]>("Pending");
        if (!cancelled) setPending(items);
      } catch (problem) {
        if (!cancelled) setError(message(problem));
      }
    }
    void refresh();
    const dispose = on("desktop:state", () => {
      void refresh();
    });
    return () => {
      cancelled = true;
      dispose();
    };
  }, [queueOpen]);
  /** loadSession 重新从服务端读取有效成员关系，不信任旧缓存中的租户。 */
  async function loadSession() {
    const user = await api<CurrentUser>("/auth/me");
    if (!user?.user?.id || !Array.isArray(user.memberships))
      throw new Error(
        tr("此地址未返回有效的GoodChat账号信息，请检查服务器地址。"),
      );
    setCurrent(user);
    setTenant((previous) =>
      user.memberships.some((item) => item.tenant_id === previous)
        ? previous
        : user.active_tenant_id || user.memberships[0]?.tenant_id || "",
    );
  }
  /** connect 先让原生模块切换服务器，再尝试恢复该服务器自己的安全登录。 */
  async function connect(values: { server: string }) {
    setBusy(true);
    setUpdate(null);
    setError("");
    setCurrent(null);
    setReady(false);
    try {
      await invoke("SelectServer", values.server);
      const selected = await invoke<Startup>("Startup");
      switchDraftServer(sessionStorage, selected.server);
      document.documentElement.dataset.server = selected.server;
      setServer(selected.server);
      setTenant("");
      setChoosing(false);
      setState({ status: "idle", detail: "", pending: 0, failed: 0 });
      setPending([]);
      setQueueOpen(false);
      const checked = await invoke<Update>("CheckUpdate");
      setUpdate(checked);
      if (checked.required) return;
      try {
        await loadSession();
      } catch (problem) {
        if (!(problem instanceof ApiError && problem.status === 401))
          throw problem;
      }
    } catch (problem) {
      setError(errorMessage(problem));
    } finally {
      setBusy(false);
    }
  }
  /** requestServer 允许查看配置，真正切换前明确提示待发送消息仍属于原服务器。 */
  function requestServer() {
    setChoosing(true);
    setError("");
  }
  /** confirmServer 让切换动作可见，防止用户在有未确认消息时误以为它们会迁移。 */
  function confirmServer(values: { server: string }) {
    if (
      current &&
      values.server.replace(/\/$/, "") !== server &&
      (state.pending || state.failed)
    ) {
      modal.confirm({
        title: tr("切换服务器"),
        content: tr(
          "未确认消息将保留在原服务器的账号分区。切换后不会转发到新服务器，重新连接原服务器并登录后才能继续处理。",
        ),
        okText: tr("切换"),
        cancelText: tr("返回"),
        onOk: () => connect(values),
      });
    } else void connect(values);
  }
  /** logout 等待服务端撤销当前设备会话，失败时保留可重试错误。 */
  async function logout() {
    setBusy(true);
    try {
      await api("/auth/logout", { method: "POST", bodyJSON: {} });
      setCurrent(null);
      setTenant("");
      setReady(false);
      setState({ status: "idle", detail: "", pending: 0, failed: 0 });
      sessionStorage.clear();
    } catch (problem) {
      setError(errorMessage(problem));
    } finally {
      setBusy(false);
    }
  }
  const member = current?.memberships.find((item) => item.tenant_id === tenant);
  return (
    <div className="desktop-root">
      <header className="desktop-header">
        <div className="desktop-brand">
          <CustomerServiceOutlined />
          <strong>GoodChat</strong>
          <span>{tr("客服工作台")}</span>
        </div>
        <div className="desktop-header-center">
          {!choosing && (
            <>
              <span
                className={`desktop-dot ${state.status === "connected" ? "connected" : ""}`}
              />
              <span>
                {localizeStatic(labels, tr)[state.status] || state.status}
              </span>
              <span className="desktop-server" title={server}>
                {server}
              </span>
            </>
          )}
        </div>
        <Space size={8}>
          {!!current && (
            <Button type="text" onClick={() => setQueueOpen(true)}>
              {tr("发送队列 ")}
              {state.pending + state.failed || ""}
            </Button>
          )}
          <Button
            type="text"
            icon={<SettingOutlined />}
            onClick={requestServer}
          >
            {tr("服务器 ")}
          </Button>
          {!!current && (
            <Button
              type="text"
              icon={<LogoutOutlined />}
              loading={busy}
              onClick={() => void logout()}
            >
              {tr("退出登录 ")}
            </Button>
          )}
        </Space>
      </header>
      {error && (
        <Alert
          className="desktop-error"
          type="error"
          showIcon
          closable
          onClose={() => setError("")}
          title={error}
        />
      )}
      {choosing ? (
        <main className="desktop-entry">
          <section className="desktop-connect-card">
            <div className="desktop-entry-icon">
              <CloudServerOutlined />
            </div>
            <span className="desktop-eyebrow">{tr("连接你的工作空间")}</span>
            <h1>{tr("选择服务器")}</h1>
            <p className="desktop-description">
              {tr("使用默认服务器，或填写你所在企业的 GoodChat 服务器地址。 ")}
            </p>
            {!startup && !error ? (
              <Spin description={tr("正在读取配置")} />
            ) : (
              <Form
                form={serverForm}
                key={server}
                layout="vertical"
                initialValues={{ server }}
                onFinish={confirmServer}
                requiredMark={false}
              >
                <Form.Item
                  label={tr("服务器地址")}
                  name="server"
                  rules={[
                    { required: true, message: tr("请填写服务器地址") },
                    {
                      validator: async (_, value: string) => {
                        let url: URL;
                        try {
                          url = new URL(value.trim());
                        } catch {
                          throw new Error(
                            tr("请输入完整地址，例如 https://goodchat.58it.cn"),
                          );
                        }
                        if (
                          url.username ||
                          url.password ||
                          url.search ||
                          url.hash ||
                          !["", "/"].includes(url.pathname)
                        )
                          throw new Error(
                            tr(
                              "只填写服务器根地址，不包含账号、页面路径或查询参数",
                            ),
                          );
                        if (
                          url.protocol !== "https:" &&
                          !(
                            url.protocol === "http:" &&
                            ["localhost", "127.0.0.1", "[::1]"].includes(
                              url.hostname,
                            )
                          )
                        )
                          throw new Error(
                            tr("请使用 HTTPS；HTTP 仅供本机调试"),
                          );
                      },
                    },
                  ]}
                >
                  <Input
                    size="large"
                    placeholder="https://goodchat.58it.cn"
                    autoFocus
                    autoComplete="url"
                    spellCheck={false}
                  />
                </Form.Item>
                <div className="desktop-server-help">
                  <span>{tr("下次启动会记住你的选择")}</span>
                  <Button
                    type="link"
                    onClick={() =>
                      serverForm.setFieldsValue({
                        server: startup?.default_server || "",
                      })
                    }
                  >
                    {tr("恢复安装包默认值 ")}
                  </Button>
                </div>
                <Button
                  type="primary"
                  htmlType="submit"
                  size="large"
                  block
                  loading={busy}
                >
                  {tr("连接服务器 ")}
                </Button>
                {!!current && (
                  <Button
                    className="desktop-return"
                    block
                    type="text"
                    onClick={() => setChoosing(false)}
                  >
                    {tr("返回当前工作台 ")}
                  </Button>
                )}
              </Form>
            )}
            <div className="desktop-entry-meta">
              <span>Mac · Windows</span>
              <span>
                {tr("版本 ")}
                {startup?.version || "—"}
              </span>
            </div>
          </section>
          <p className="desktop-entry-note">
            {tr(
              "关闭窗口后，GoodChat 将留在系统托盘或菜单栏。彻底退出后停止接收。 ",
            )}
          </p>
        </main>
      ) : update?.required ? (
        <UpdateRequired
          update={update}
          busy={busy}
          error={error}
          onServer={requestServer}
          onCheck={() => void connect({ server })}
        />
      ) : !current ? (
        <Login
          server={server}
          busy={busy}
          onLogin={loadSession}
          onChangeServer={requestServer}
        />
      ) : !current.memberships.length ? (
        <Empty
          description={tr("当前账号没有可用工作空间，请联系管理员邀请加入。")}
        />
      ) : (
        <>
          <div className="desktop-toolbar">
            <Space>
              <Select
                aria-label={tr("工作空间")}
                value={tenant}
                onChange={setTenant}
                options={current.memberships.map((item) => ({
                  value: item.tenant_id,
                  label: item.tenant_name,
                }))}
              />
              <span className="muted">
                {current.user.display_name || current.user.email}
              </span>
            </Space>
            <Space>
              <span className="desktop-connection-detail">{state.detail}</span>
              <Button
                size="small"
                icon={<ReloadOutlined />}
                onClick={() => void invoke("Reconnect")}
              >
                {tr("重新连接 ")}
              </Button>
              <div id="workspace-header-actions" />
            </Space>
          </div>
          {ready && member ? (
            <DesktopSessionProvider
              value={{
                current,
                tenantId: tenant,
                setTenantId: setTenant,
                reload: loadSession,
              }}
            >
              <WorkspaceRemindersProvider
                tenantId={tenant}
                memberId={member.id}
                canServe={member.can_serve}
              >
                <main
                  className="desktop-workspace"
                  key={`${server}:${current.user.id}:${tenant}`}
                >
                  <AgentWorkspace />
                </main>
              </WorkspaceRemindersProvider>
            </DesktopSessionProvider>
          ) : (
            <div className="console-loading">
              <Spin description={tr("正在核验工作空间并启动消息模块")} />
            </div>
          )}
        </>
      )}
      <Drawer
        title={tr("本机发送队列")}
        open={queueOpen}
        onClose={() => setQueueOpen(false)}
        size={460}
      >
        <Alert
          type="info"
          title={tr(
            "只有服务器确认保存后才会移出队列。网络失败会自动重试；服务器拒绝的消息需核对后手动重试。",
          )}
        />
        {!pending.length ? (
          <Empty description={tr("没有未确认消息")} />
        ) : (
          pending.map((item) => (
            <article className="desktop-pending" key={item.id}>
              <Tag color={item.state === "failed" ? "error" : "processing"}>
                {item.state === "failed" ? tr("需要处理") : tr("待服务器确认")}
              </Tag>
              <p>
                {item.body.content ||
                  `[${item.body.message_type || tr("附件")}]`}
              </p>
              <Typography.Text type="secondary">
                {item.error || tr("保留原消息编号，连接恢复后自动核对。")}
              </Typography.Text>
              {item.state === "failed" && (
                <Button
                  onClick={() =>
                    void invoke("Retry", item.id)
                      .then(() => toast.success(tr("已加入原消息重试队列")))
                      .catch((problem) => toast.error(message(problem)))
                  }
                >
                  {tr("重试原消息 ")}
                </Button>
              )}
            </article>
          ))
        )}
      </Drawer>
    </div>
  );
}
