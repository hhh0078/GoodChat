/** 文件作用说明：实现客服上下班、咨询列表、人工接单、转接与结束咨询的工作台。 */
"use client";
import { EmptyData, ErrorNotice, PageTitle } from "@/components/page-state";
import { useSession } from "@/features/auth/session";
import { useFormats } from "@/i18n/use-formats";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import { canTransferTo, isTransferRecipient } from "@/lib/assignment-state";
import { useRealtime } from "@/lib/realtime/use-realtime";
import type { Resource } from "@/lib/types";
import { useResource } from "@/lib/use-resource";
import { channelTags } from "@/lib/workspace-display";
import {
  ArrowLeftOutlined,
  CustomerServiceOutlined,
  SwapOutlined,
  UserOutlined,
} from "@ant-design/icons";
import {
  Alert,
  App,
  Avatar,
  Badge,
  Button,
  Drawer,
  Grid,
  Popconfirm,
  Popover,
  Select,
  Space,
  Spin,
  Tabs,
  Tag,
  Tooltip,
} from "antd";
import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { ChatPanel } from "./chat-panel";
import { ConversationFilters } from "./conversation-filters";
import "./conversation-list.css";
import { CustomerContext } from "./customer-context";
import { ReminderVolume } from "./reminder-volume";
import { useConversationInbox } from "./use-conversation-inbox";
import { useWorkspaceReminders } from "./workspace-reminders";
type AgentStatus = {
  available: boolean;
  busy: boolean;
  current_load: number;
  capacity: number;
};
/** AgentWorkspace 以真实身份接待访客，当前分配权限始终交由后端核验。 */
export function AgentWorkspace() {
  const { currentPreferences, numberFormatter } = useFormats();

  const tr = useTranslator();
  const { tenantId, current } = useSession();
  const membership = current.memberships.find((item) => item.tenant_id === tenantId);
  const { message } = App.useApp();
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
  const reminders = useWorkspaceReminders();
  const conversations = useConversationInbox({
    tenantId,
    actorId: membership?.id || "",
    enabled: !!membership?.can_serve,
  });
  const { active, selected, status, period, select: setSelected } = conversations;
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
      },
      onRecover: async () => {
        const runtime = await api<AgentStatus>("/agent/status", { tenantId });
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
    setError(null);
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
      message.success(tr("接待状态已更新"));
    } catch (problem) {
      setError(problem);
      conversations.reload();
    } finally {
      setBusy(false);
    }
  }
  const controls = (
    <Space className="agent-controls" size={14}>
      <Tooltip
        title={tr("当前接待 {p0} / {p1}", {
          p0: String(runtime?.current_load ?? "—"),
          p1: String(runtime?.capacity ?? "—"),
        })}
      >
        <Popover
          trigger="click"
          title={tr("实时连接诊断")}
          content={
            <div className="realtime-diagnostics">
              <p>
                {tr("连接状态：")}
                {realtime.status}
              </p>
              <pre>{JSON.stringify(realtime.diagnostics, null, 2)}</pre>
              <Button size="small" onClick={realtime.reconnect}>
                {tr("重新连接 ")}
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
              text={realtime.connected ? tr("实时在线") : tr("恢复连接中")}
            />
          </button>
        </Popover>
      </Tooltip>
      <Select
        aria-label={tr("接待状态")}
        popupMatchSelectWidth={100}
        className={`agent-availability ${!runtime?.available ? "is-offline" : runtime.busy ? "is-busy" : "is-ready"}`}
        variant="outlined"
        value={!runtime?.available ? "offline" : runtime.busy ? "busy" : "ready"}
        disabled={!runtime || busy}
        onChange={(value) =>
          void updateStatus({ available: value !== "offline", busy: value === "busy" })
        }
        options={[
          { value: "ready", label: tr("正常") },
          { value: "busy", label: tr("示忙") },
          { value: "offline", label: tr("离线") },
        ]}
      />
      <ReminderVolume reminders={reminders} />
    </Space>
  );
  if (!membership?.can_serve)
    return (
      <>
        <PageTitle title={tr("客服工作台")} description={tr("此账号尚未启用接待权限。")} />
        <Alert
          type="info"
          showIcon
          title={tr("此账号尚未启用接待权限")}
          description={tr(
            "请由租户管理员在团队成员中开启\u201C可接待\u201D，并将你加入客服业务组。",
          )}
        />
      </>
    );
  return (
    <>
      <h1 className="sr-only">{tr("客服工作台")}</h1>
      {headerTarget && createPortal(controls, headerTarget)}
      <div className="agent-mobile-controls">{controls}</div>
      <ErrorNotice error={error || conversations.error} retry={conversations.reload} />
      <div
        className={`chat-layout agent-layout ${selected ? "has-active" : ""} ${active && screens.xl ? "with-customer" : ""}`}
      >
        <div
          className="conversation-list"
          ref={conversations.listNode}
          aria-label={tr("会话列表")}
          onScroll={conversations.checkEnd}
        >
          <div className="conversation-head">
            <ConversationFilters
              period={period}
              timezone={conversations.timezone}
              onPeriod={conversations.changePeriod}
              onSearch={conversations.search}
            />
            <Tabs
              className="inbox-tabs"
              tabBarGutter={13}
              activeKey={status}
              onChange={conversations.changeStatus}
              items={[
                { key: "", label: tr("全部") },
                { key: "active", label: tr("接待中") },
                { key: "assigned", label: tr("待接") },
                { key: "queued", label: tr("排队") },
                { key: "closed", label: tr("结束") },
              ]}
            />
          </div>
          {!conversations.loading && !conversations.items.length ? (
            <EmptyData text={tr("暂时没有咨询")} />
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
                  {(item.contact?.display_name || item.contact_name || tr("访")).slice(0, 1)}
                </Avatar>
                <div className="conversation-summary">
                  <div className="conversation-row-title">
                    <div className="conversation-name-channels">
                      <strong>
                        {item.contact?.display_name || item.contact_name || tr("访客")}
                      </strong>
                      {channelTags(item.source_snapshot).map((channel) => (
                        <Tag key={channel}>{channel}</Tag>
                      ))}
                    </div>
                    <div className="conversation-row-meta">
                      <time>
                        {item.last_message_at || item.created_at
                          ? new Date(
                              String(item.last_message_at || item.created_at),
                            ).toLocaleTimeString(currentPreferences().locale, {
                              timeZone: currentPreferences().timezone,
                              ...{
                                hour: "2-digit",
                                minute: "2-digit",
                              },
                            })
                          : ""}
                      </time>
                      <Badge
                        className="conversation-unread"
                        count={item.unread_count || 0}
                        overflowCount={99}
                        size="small"
                        title={tr("未读{p0}条", { p0: String(item.unread_count || 0) })}
                      />
                    </div>
                  </div>
                  <p className="conversation-status" data-status={item.status}>
                    <span className="visitor-presence">
                      {item.contact_online ? tr("在线") : tr("离线")}
                    </span>{" "}
                    {item.contact?.phone ? ` · ${item.contact.phone}` : ""}
                  </p>
                </div>
              </button>
            ))
          )}
          <div className="conversation-list-footer" ref={conversations.listEnd}>
            {conversations.loading || conversations.loadingMore ? (
              <Spin size="small" />
            ) : conversations.error && conversations.items.length ? (
              <Button type="link" size="small" onClick={conversations.reload}>
                {tr("加载失败，点击重试 ")}
              </Button>
            ) : conversations.hasMore ? (
              <Button type="text" size="small" onClick={conversations.loadMore}>
                {tr("加载更多会话 ")}
              </Button>
            ) : conversations.items.length ? (
              <span>{tr("已显示全部匹配会话")}</span>
            ) : null}
          </div>
        </div>
        <div className="chat-main">
          {!active ? (
            <div className="agent-welcome">
              <div className="agent-welcome-mark" aria-hidden="true">
                <CustomerServiceOutlined />
                <span />
              </div>
              <span className="agent-welcome-eyebrow">{tr("你的接待空间")}</span>
              <h2>{tr("准备好，回应每一份期待。")}</h2>
              <p>
                {tr("选择左侧咨询，在这里开始对话。 ")}
                <br />
                {tr("客户资料与访问记录会随会话呈现。 ")}
              </p>
              <div className="agent-load-summary">
                <span>{tr("当前接待")}</span>
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
                    aria-label={tr("返回咨询列表")}
                    icon={<ArrowLeftOutlined />}
                    onClick={() => setSelected("")}
                  />
                  <Avatar
                    className="chat-contact-avatar"
                    size={42}
                    src={active.contact?.avatar_url || undefined}
                  >
                    {(active.contact?.display_name || tr("访")).slice(0, 1)}
                  </Avatar>
                  <div>
                    <div className="chat-name-channels">
                      <strong>{active.contact?.display_name || tr("访客")}</strong>
                      {channelTags(active.source_snapshot).map((channel) => (
                        <Tag key={channel}>{channel}</Tag>
                      ))}
                    </div>
                    <div className="form-hint">
                      {active.contact?.phone || tr("未留电话")} · {active.id.slice(0, 8)}
                    </div>
                  </div>
                </Space>
                <Space wrap className="chat-assignment-actions">
                  {!screens.xl && (
                    <Button
                      icon={<UserOutlined />}
                      aria-label={tr("打开客户资料")}
                      onClick={() => setProfileOpen(true)}
                    >
                      {tr("客户资料 ")}
                    </Button>
                  )}
                  {receiving && transfer && (
                    <>
                      <Tag color="gold">{tr("收到转接邀请")}</Tag>
                      <Button
                        type="primary"
                        loading={busy}
                        onClick={() =>
                          void assignmentAction("accept", { transfer_request_id: transfer.id })
                        }
                      >
                        {tr("接受转接 ")}
                      </Button>
                      <Button
                        loading={busy}
                        onClick={() =>
                          void assignmentAction("reject", { transfer_request_id: transfer.id })
                        }
                      >
                        {tr("拒绝转接 ")}
                      </Button>
                    </>
                  )}
                  {transfer && active.current_agent_id === membership.id && (
                    <Tag color="gold">{tr("等待目标客服确认")}</Tag>
                  )}
                  {active.status === "queued" &&
                    !active.context?.ai_receiving &&
                    active.reception_mode === "manual" && (
                      <Button
                        type="primary"
                        loading={busy}
                        onClick={() => void assignmentAction("claim")}
                      >
                        {tr("领取咨询 ")}
                      </Button>
                    )}
                  {["assigned", "pending"].includes(active.status) &&
                    active.current_agent_id === membership.id && (
                      <>
                        <Button type="primary" onClick={() => void assignmentAction("accept")}>
                          {tr("确认接单 ")}
                        </Button>
                        <Popconfirm
                          title={tr("拒绝接单并返回队列？")}
                          onConfirm={() => assignmentAction("reject")}
                        >
                          <Button>{tr("拒绝")}</Button>
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
                        {tr("转接 ")}
                      </Button>
                      <Popconfirm
                        title={tr("结束本次咨询？")}
                        onConfirm={() => assignmentAction("close")}
                      >
                        <Button>{tr("结束咨询")}</Button>
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
                onRead={conversations.reload}
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
          title={tr("客户资料")}
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
        title={tr("转接咨询")}
        open={transferOpen}
        onClose={() => setTransferOpen(false)}
        width={420}
      >
        <p>{tr("转回队列会重新应用分配规则；指定客服需要对方满足接待条件。")}</p>
        <ErrorNotice error={colleagues.error} retry={colleagues.reload} />
        <Select
          loading={colleagues.loading}
          value={target}
          onChange={setTarget}
          style={{ width: "100%", marginBottom: 20 }}
          placeholder={tr("选择目标")}
          options={[
            { label: tr("返回原队列"), value: "queue" },
            ...colleagues.items
              .filter((item) => item.id !== membership.id)
              .map((item) => ({
                label: `${String(item.display_name || item.id)}${!item.online ? tr(" \u00B7 离线") : item.busy ? tr(" \u00B7 示忙") : !item.available ? tr(" \u00B7 未上班") : ""}`,
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
          {tr("发起转接 ")}
        </Button>
      </Drawer>
    </>
  );
}
