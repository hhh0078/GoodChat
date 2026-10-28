/** 文件作用说明：实现客服上下班、咨询列表、人工接单、转接与结束咨询的工作台。 */
"use client";
import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import {
  Alert,
  App,
  Badge,
  Button,
  Avatar,
  Grid,
  Tabs,
  Tooltip,
  Drawer,
  Input,
  Popconfirm,
  Popover,
  Select,
  Space,
  Tag,
} from "antd";
import {
  ArrowLeftOutlined,
  CustomerServiceOutlined,
  ReloadOutlined,
  SwapOutlined,
  UserOutlined,
} from "@ant-design/icons";
import { canTransferTo, isTransferRecipient } from "@/lib/assignment-state";
import { channelTags } from "@/lib/workspace-display";
import { useRealtime } from "@/lib/realtime/use-realtime";
import { api } from "@/lib/api";
import { useSession } from "@/features/auth/session";
import { useResource } from "@/lib/use-resource";
import type { Conversation, Resource } from "@/lib/types";
import { ErrorNotice, EmptyData, PageTitle } from "@/components/page-state";
import { useReminders } from "./reminders";
import { ReminderVolume } from "./reminder-volume";
import { ChatPanel, conversationStatus } from "./chat-panel";
import { CustomerContext } from "./customer-context";
type AgentStatus = { available: boolean; busy: boolean; current_load: number; capacity: number };
/** AgentWorkspace 以真实身份接待访客，当前分配权限始终交由后端核验。 */
export function AgentWorkspace() {
  const { tenantId, current } = useSession();
  const membership = current.memberships.find((item) => item.tenant_id === tenantId);
  const { message } = App.useApp();
  const [selected, setSelected] = useState("");
  const [status, setStatus] = useState("");
  const [query, setQuery] = useState("");
  const [runtime, setRuntime] = useState<AgentStatus | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [transferOpen, setTransferOpen] = useState(false);
  const [target, setTarget] = useState<string | undefined>();
  const [profileOpen, setProfileOpen] = useState(false);
  const [headerTarget, setHeaderTarget] = useState<HTMLElement | null>(null);
  const screens = Grid.useBreakpoint();
  useEffect(() => {
    setHeaderTarget(document.getElementById("workspace-header-actions"));
  }, []);
  const reminders = useReminders(tenantId);
  const conversations = useResource<Conversation>(
    membership?.can_serve ? `/conversations?${new URLSearchParams({ status, query })}` : "",
  );
  const active = conversations.items.find((item) => item.id === selected);
  const colleagues = useResource<Resource>(
    selected ? `/agent/colleagues?conversation_id=${encodeURIComponent(selected)}` : "",
  );
  const transfer = active?.pending_transfer;
  const receiving = isTransferRecipient(transfer, membership?.id || "");
  useEffect(() => {
    setSelected("");
    if (!membership?.can_serve) return;
    api<AgentStatus>("/agent/status", { tenantId }).then(setRuntime).catch(setError);
  }, [tenantId, membership?.can_serve]);
  const realtime = useRealtime(
    { tenantId, actorId: membership?.id || "", kind: "agent" },
    {
      enabled: !!membership?.can_serve,
      onEvent: (event) => {
        conversations.reload();
        reminders.receive(event);
      },
      onRecover: async () => {
        const [, runtime] = await Promise.all([
          api(`/conversations?${new URLSearchParams({ status, query })}`, { tenantId }),
          api<AgentStatus>("/agent/status", { tenantId }),
        ]);
        setRuntime(runtime);
        conversations.reload();
      },
    },
  );
  useEffect(() => {
    if (!membership?.can_serve) return;
    const controller = new AbortController();
    const polling = setInterval(() => {
      conversations.reload();
      api<AgentStatus>("/agent/status", { tenantId, signal: controller.signal })
        .then(setRuntime)
        .catch(() => {});
    }, 10000);
    return () => {
      controller.abort();
      clearInterval(polling);
    };
  }, [tenantId, membership?.can_serve, conversations.reload]);
  /** updateStatus 由客服明确选择上下班和示忙，不因打开页面自动上班。 */
  async function updateStatus(values: Partial<AgentStatus>) {
    setBusy(true);
    try {
      setRuntime(
        await api<AgentStatus>("/agent/status", { method: "PATCH", tenantId, bodyJSON: values }),
      );
      conversations.reload();
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  /** assignmentAction 带当前版本提交分配动作，冲突时让用户重新读取。 */
  async function assignmentAction(action: string, extra: Record<string, unknown> = {}) {
    if (!active) return;
    setBusy(true);
    setError(null);
    try {
      await api(`/conversations/${active.id}/assignments/${action}`, {
        method: "POST",
        tenantId,
        bodyJSON: { expected_version: active.version, ...extra },
        idempotent: true,
      });
      conversations.reload();
      setTransferOpen(false);
      message.success("接待状态已更新");
    } catch (problem) {
      setError(problem);
      conversations.reload();
    } finally {
      setBusy(false);
    }
  }
  const controls = (
    <Space className="agent-controls" size={14}>
      <Tooltip title={`当前接待 ${runtime?.current_load ?? "—"} / ${runtime?.capacity ?? "—"}`}>
        <Popover
          trigger="click"
          title="实时连接诊断"
          content={
            <div className="realtime-diagnostics">
              <p>连接状态：{realtime.status}</p>
              <pre>{JSON.stringify(realtime.diagnostics, null, 2)}</pre>
              <Button size="small" onClick={realtime.reconnect}>
                重新连接
              </Button>
            </div>
          }
        >
          <button
            className={`connection-status-button ${realtime.connected ? "is-connected" : "is-disconnected"}`}
            type="button"
          >
            <Badge
              status={realtime.connected ? "success" : "error"}
              text={realtime.connected ? "实时在线" : "恢复连接中"}
            />
          </button>
        </Popover>
      </Tooltip>
      <Select
        aria-label="接待状态"
        className={`agent-availability ${!runtime?.available ? "is-offline" : runtime.busy ? "is-busy" : "is-ready"}`}
        variant="outlined"
        value={!runtime?.available ? "offline" : runtime.busy ? "busy" : "ready"}
        disabled={!runtime || busy}
        onChange={(value) =>
          void updateStatus({ available: value !== "offline", busy: value === "busy" })
        }
        options={[
          { value: "ready", label: "上班 · 可接待" },
          { value: "busy", label: "上班 · 示忙" },
          { value: "offline", label: "下班" },
        ]}
      />
      <ReminderVolume reminders={reminders} />
    </Space>
  );
  if (!membership?.can_serve)
    return (
      <>
        <PageTitle title="客服工作台" description="此账号尚未启用接待权限。" />
        <Alert
          type="info"
          showIcon
          title="请由租户管理员在团队成员中开启“可接待”，并将你加入客服业务组。"
        />
      </>
    );
  return (
    <>
      <h1 className="sr-only">客服工作台</h1>
      {headerTarget && createPortal(controls, headerTarget)}
      <div className="agent-mobile-controls">{controls}</div>
      <ErrorNotice error={error || conversations.error} retry={conversations.reload} />
      <div
        className={`chat-layout agent-layout ${selected ? "has-active" : ""} ${active && screens.xl ? "with-customer" : ""}`}
      >
        <div className="conversation-list">
          <div className="conversation-head">
            <Space style={{ width: "100%", justifyContent: "space-between", marginBottom: 12 }}>
              <div className="inbox-title">
                <strong>咨询</strong>
                <span aria-label={`当前列表${conversations.items.length}条`}>
                  {conversations.items.length}
                </span>
              </div>
              <Button
                size="small"
                icon={<ReloadOutlined />}
                aria-label="刷新咨询列表"
                onClick={conversations.reload}
              />
            </Space>
            <Input.Search
              allowClear
              aria-label="搜索客户"
              placeholder="搜索客户姓名或手机号"
              onSearch={setQuery}
            />
            <Tabs
              className="inbox-tabs"
              tabBarGutter={13}
              activeKey={status}
              onChange={(value) => {
                setStatus(value);
                setSelected("");
              }}
              items={[
                { key: "", label: "全部" },
                { key: "active", label: "接待中" },
                { key: "assigned", label: "待接" },
                { key: "queued", label: "排队" },
                { key: "closed", label: "结束" },
              ]}
            />
          </div>
          {!conversations.loading && !conversations.items.length ? (
            <EmptyData text="暂时没有咨询" />
          ) : (
            conversations.items.map((item) => (
              <button
                key={item.id}
                className={`conversation-row ${item.contact_online ? "is-online" : "is-offline"} ${item.id === selected ? "active" : ""}`}
                aria-pressed={item.id === selected}
                onClick={() => setSelected(item.id)}
              >
                <Avatar
                  className="conversation-avatar"
                  size={40}
                  src={item.contact?.avatar_url || undefined}
                >
                  {(item.contact?.display_name || item.contact_name || "访").slice(0, 1)}
                </Avatar>
                <div className="conversation-summary">
                  <div className="conversation-row-title">
                    <div className="conversation-name-channels">
                      <strong>{item.contact?.display_name || item.contact_name || "访客"}</strong>
                      {channelTags(item.source_snapshot).map((channel) => (
                        <Tag key={channel}>{channel}</Tag>
                      ))}
                    </div>
                    <time>
                      {item.updated_at
                        ? new Date(item.updated_at).toLocaleTimeString("zh-CN", {
                            hour: "2-digit",
                            minute: "2-digit",
                          })
                        : ""}
                    </time>
                  </div>
                  <p className="conversation-status" data-status={item.status}>
                    <span className="visitor-presence">
                      {item.contact_online ? "在线" : "离线"}
                    </span>{" "}
                    · {conversationStatus[item.status] || item.status}
                    {item.agent_name ? ` · ${item.agent_name}` : ""}
                  </p>
                  <p>{item.contact?.phone || `咨询 ${item.id.slice(0, 8)}`}</p>
                </div>
              </button>
            ))
          )}
        </div>
        <div className="chat-main">
          {!active ? (
            <div className="agent-welcome">
              <div className="agent-welcome-mark" aria-hidden="true">
                <CustomerServiceOutlined />
                <span />
              </div>
              <span className="agent-welcome-eyebrow">你的接待空间</span>
              <h2>准备好，回应每一份期待。</h2>
              <p>
                选择左侧咨询，在这里开始对话。
                <br />
                客户资料与访问记录会随会话呈现。
              </p>
              <div className="agent-load-summary">
                <span>当前接待</span>
                <strong>{runtime?.current_load ?? "—"}</strong>
                <span>/ {runtime?.capacity ?? "—"}</span>
              </div>
            </div>
          ) : (
            <>
              <div className="chat-head">
                <Space>
                  <Button
                    className="mobile-menu"
                    aria-label="返回咨询列表"
                    icon={<ArrowLeftOutlined />}
                    onClick={() => setSelected("")}
                  />
                  <Avatar
                    className="chat-contact-avatar"
                    size={42}
                    src={active.contact?.avatar_url || undefined}
                  >
                    {(active.contact?.display_name || "访").slice(0, 1)}
                  </Avatar>
                  <div>
                    <div className="chat-name-channels">
                      <strong>{active.contact?.display_name || "访客"}</strong>
                      {channelTags(active.source_snapshot).map((channel) => (
                        <Tag key={channel}>{channel}</Tag>
                      ))}
                    </div>
                    <div className="form-hint">
                      {active.contact?.phone || "未留电话"} · {active.id.slice(0, 8)}
                    </div>
                  </div>
                </Space>
                <Space wrap className="chat-assignment-actions">
                  {!screens.xl && (
                    <Button
                      icon={<UserOutlined />}
                      aria-label="打开客户资料"
                      onClick={() => setProfileOpen(true)}
                    >
                      客户资料
                    </Button>
                  )}
                  {receiving && transfer && (
                    <>
                      <Tag color="gold">收到转接邀请</Tag>
                      <Button
                        type="primary"
                        loading={busy}
                        onClick={() =>
                          void assignmentAction("accept", { transfer_request_id: transfer.id })
                        }
                      >
                        接受转接
                      </Button>
                      <Button
                        loading={busy}
                        onClick={() =>
                          void assignmentAction("reject", { transfer_request_id: transfer.id })
                        }
                      >
                        拒绝转接
                      </Button>
                    </>
                  )}
                  {transfer && active.current_agent_id === membership.id && (
                    <Tag color="gold">等待目标客服确认</Tag>
                  )}
                  {active.status === "queued" && (
                    <Button
                      type="primary"
                      loading={busy}
                      onClick={() => void assignmentAction("claim")}
                    >
                      领取咨询
                    </Button>
                  )}
                  {["assigned", "pending"].includes(active.status) &&
                    active.current_agent_id === membership.id && (
                      <>
                        <Button type="primary" onClick={() => void assignmentAction("accept")}>
                          确认接单
                        </Button>
                        <Popconfirm
                          title="拒绝接单并返回队列？"
                          onConfirm={() => assignmentAction("reject")}
                        >
                          <Button>拒绝</Button>
                        </Popconfirm>
                      </>
                    )}
                  {active.current_agent_id === membership.id && active.status === "active" && (
                    <>
                      <Button
                        disabled={!!transfer}
                        icon={<SwapOutlined />}
                        onClick={() => {
                          setTarget(undefined);
                          setTransferOpen(true);
                        }}
                      >
                        转接
                      </Button>
                      <Popconfirm
                        title="结束本次咨询？"
                        onConfirm={() => assignmentAction("close")}
                      >
                        <Button>结束咨询</Button>
                      </Popconfirm>
                    </>
                  )}
                </Space>
              </div>
              <ChatPanel
                key={`${tenantId}.${active.id}`}
                id={active.id}
                identity={{ tenantId, actorId: membership.id, kind: "agent" }}
                canManageReplies={["owner", "admin"].includes(membership.role)}
              />
            </>
          )}
        </div>
        {active && screens.xl && (
          <aside className="agent-customer-sidebar">
            <CustomerContext
              key={`${tenantId}.${active.id}.${active.status}.${active.current_agent_id}.${membership.role}`}
              conversation={active}
              membership={membership}
            />
          </aside>
        )}
      </div>
      {!screens.xl && (
        <Drawer
          title="客户资料"
          open={profileOpen && !!active}
          onClose={() => setProfileOpen(false)}
          width={340}
        >
          {active && profileOpen && (
            <CustomerContext
              key={`${tenantId}.${active.id}.${active.status}.${active.current_agent_id}.${membership.role}`}
              conversation={active}
              membership={membership}
            />
          )}
        </Drawer>
      )}
      <Drawer
        title="转接咨询"
        open={transferOpen}
        onClose={() => setTransferOpen(false)}
        width={420}
      >
        <p>转回队列会重新应用分配规则；指定客服需要对方满足接待条件。</p>
        <ErrorNotice error={colleagues.error} retry={colleagues.reload} />
        <Select
          loading={colleagues.loading}
          value={target}
          onChange={setTarget}
          style={{ width: "100%", marginBottom: 20 }}
          placeholder="选择目标"
          options={[
            { label: "返回原队列", value: "queue" },
            ...colleagues.items
              .filter((item) => item.id !== membership.id)
              .map((item) => ({
                label: `${String(item.display_name || item.id)}${!item.online ? " · 离线" : item.busy ? " · 示忙" : !item.available ? " · 未上班" : ""}`,
                value: item.id,
                disabled: !canTransferTo(item),
              })),
          ]}
        />
        <Button
          type="primary"
          loading={busy}
          disabled={!target}
          onClick={() =>
            void assignmentAction(
              "transfer",
              target === "queue" ? { target: "queue" } : { agent_id: target },
            )
          }
        >
          发起转接
        </Button>
      </Drawer>
    </>
  );
}
