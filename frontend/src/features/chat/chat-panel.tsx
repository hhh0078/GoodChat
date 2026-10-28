/** 文件作用说明：提供访客与客服共用的消息界面、幂等重试、附件、语音、引用与可见已读。 */
"use client";
import { useEffect, useRef, useState, type CSSProperties } from "react";
import { Alert, App, Avatar, Button, Input, Popconfirm, Space, Spin, Tooltip } from "antd";
import { AudioOutlined, CloseOutlined, PaperClipOutlined, SendOutlined } from "@ant-design/icons";
import { ErrorNotice, EmptyData } from "@/components/page-state";
import { errorMessage } from "@/lib/api";
import type { ChatMessage } from "@/lib/types";
import { useChat, type ChatIdentity, type Outgoing } from "./use-chat";
import { MessageHistory } from "./message-history";
import { insertReplyText } from "@/lib/workspace-display";
import { acknowledgePending, readPending, stagePending } from "@/lib/pending-messages";
import { QuickReplies } from "./quick-replies";
import { useVoiceRecorder } from "./use-voice-recorder";
export const conversationStatus: Record<string, string> = {
  queued: "正在排队",
  assigned: "等待客服接单",
  active: "正在接待",
  closed: "咨询已结束",
};
/** ChatPanel 根据已认证参与者显示消息，发送与已读均以服务端响应为准。 */
export function ChatPanel({
  id,
  identity,
  onStatus,
  readOnly = false,
  canManageReplies = false,
}: {
  id: string;
  identity: ChatIdentity;
  onStatus?: (status: string) => void;
  readOnly?: boolean;
  canManageReplies?: boolean;
}) {
  const chat = useChat(id, identity);
  const { message } = App.useApp();
  const [draft, setDraft] = useState("");
  const audience = "public";
  const [pending, setPending] = useState<Outgoing[]>([]);
  const [flight, setFlight] = useState<{
    id: string;
    content: string;
    style: CSSProperties;
  } | null>(null);
  const sendingLock = useRef(false);
  const flightTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const composerNode = useRef<HTMLDivElement | null>(null);
  const [reply, setReply] = useState<ChatMessage | null>(null);
  const [failed, setFailed] = useState<Outgoing[]>([]);
  const [sending, setSending] = useState(false);
  const [storageWarning, setStorageWarning] = useState("");
  const [draftHydrated, setDraftHydrated] = useState(false);
  const [uploading, setUploading] = useState(false);
  const { recording, toggleRecording } = useVoiceRecorder((file) => uploadAndSend(file, "audio"));
  const [atBottom, setAtBottom] = useState(true);
  const scroller = useRef<HTMLDivElement | null>(null);
  const fileInput = useRef<HTMLInputElement | null>(null);
  const composing = useRef(false);
  const mounted = useRef(true);
  const lastRead = useRef("0");
  const draftKey = `goodchat.draft.${identity.tenantId || "visitor"}.${identity.actorId}.${id}`;
  const scope = useRef(draftKey);
  scope.current = draftKey;
  const current = chat.conversation;
  const visibleMessages = chat.messages.filter((item) => item.audience !== "internal");
  const confirmedIds = new Set(
    chat.messages
      .filter((item) => item.sender_id === identity.actorId)
      .map((item) => item.client_message_id),
  );
  const canSend =
    !!current &&
    !readOnly &&
    current?.status !== "closed" &&
    (identity.kind === "visitor" ||
      (current?.status === "active" && current.current_agent_id === identity.actorId));
  useEffect(() => {
    mounted.current = true;
    setPending([]);
    try {
      setDraft(sessionStorage.getItem(draftKey) || "");
    } catch {
      setDraft("");
      setStorageWarning("浏览器禁止草稿暂存，刷新可能丢失输入，请允许站点存储后发送。");
    }
    try {
      setFailed(readPending(sessionStorage, `${draftKey}.pending`));
    } catch (problem) {
      setFailed([]);
      setStorageWarning(errorMessage(problem));
    }
    setDraftHydrated(true);
    setReply(null);
    lastRead.current = "0";
    return () => {
      mounted.current = false;
      if (flightTimer.current) clearTimeout(flightTimer.current);
    };
  }, [draftKey]);
  useEffect(() => {
    const confirmed = new Set(
      chat.messages
        .filter((item) => item.sender_id === identity.actorId)
        .map((item) => item.client_message_id),
    );
    try {
      acknowledgePending(
        sessionStorage,
        `${draftKey}.pending`,
        [...confirmed].filter((value): value is string => typeof value === "string"),
      );
    } catch (problem) {
      setStorageWarning(errorMessage(problem));
    }
    setFailed((previous) =>
      previous.some((item) => confirmed.has(item.client_message_id))
        ? previous.filter((item) => !confirmed.has(item.client_message_id))
        : previous,
    );
  }, [chat.messages, identity.actorId, draftKey]);
  useEffect(() => {
    if (!draftHydrated) return;
    try {
      sessionStorage.setItem(draftKey, draft);
    } catch {
      setStorageWarning("浏览器无法暂存草稿，刷新可能丢失输入，请允许站点存储或释放空间后发送。");
    }
  }, [draft, draftKey, draftHydrated]);
  useEffect(() => {
    if (current) onStatus?.(current.status);
  }, [current, onStatus]);
  useEffect(() => {
    if (atBottom)
      scroller.current?.scrollTo({ top: scroller.current.scrollHeight, behavior: "smooth" });
  }, [chat.messages.length, pending.length, atBottom]);
  useEffect(() => {
    const node = scroller.current;
    if (!node || !atBottom || typeof ResizeObserver === "undefined") return;
    // 视口或资料栏变化后保持正在阅读的最新消息可见，向上查历史时不强制跳回。
    const observer = new ResizeObserver(() =>
      node.scrollTo({ top: node.scrollHeight, behavior: "auto" }),
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [atBottom]);
  useEffect(() => {
    const latest = visibleMessages.at(-1)?.seq || visibleMessages.at(-1)?.event_seq;
    if (
      !latest ||
      !atBottom ||
      document.visibilityState !== "visible" ||
      lastRead.current === latest
    )
      return;
    lastRead.current = latest;
    chat
      .request(`/conversations/${id}/read-cursor`, {
        method: "POST",
        bodyJSON: { last_read_seq: latest },
      })
      .catch(() => {
        lastRead.current = "0";
      });
  }, [chat.messages, atBottom, id, chat.request]);
  /** sendOutgoing 发送前同步保存原会话完整消息，确认后仅清理原编号，离开会话不丢失在途请求。 */
  async function sendOutgoing(body: Outgoing, onQueued?: () => void) {
    if (sendingLock.current) return;
    const operationScope = draftKey;
    const storageKey = `${operationScope}.pending`;
    /** isCurrent 拦截切换会话后的异步界面更新，存储清理仍使用原会话键。 */
    const isCurrent = () => mounted.current && scope.current === operationScope;
    try {
      stagePending(sessionStorage, storageKey, body);
    } catch (problem) {
      setStorageWarning(errorMessage(problem));
      message.error(errorMessage(problem));
      return;
    }
    sendingLock.current = true;
    setSending(true);
    setAtBottom(true);
    setPending((previous) => [...previous, body]);
    onQueued?.();
    try {
      await chat.send(body);
      try {
        acknowledgePending(sessionStorage, storageKey, [body.client_message_id]);
      } catch (problem) {
        if (isCurrent()) setStorageWarning(errorMessage(problem));
      }
      if (!isCurrent()) return;
      setFailed((previous) =>
        previous.filter((item) => item.client_message_id !== body.client_message_id),
      );
      chat.typing(false);
      setAtBottom(true);
    } catch (problem) {
      if (!isCurrent()) return;
      setFailed((previous) =>
        previous.some((item) => item.client_message_id === body.client_message_id)
          ? previous
          : [...previous, body],
      );
      message.error(errorMessage(problem));
    } finally {
      sendingLock.current = false;
      if (isCurrent()) {
        setSending(false);
        setPending((previous) =>
          previous.filter((item) => item.client_message_id !== body.client_message_id),
        );
      }
    }
  }
  /** animateText 以输入位置到消息区的短动画表达发送过程，不提前标记服务端成功。 */
  function animateText(content: string, id: string) {
    if (identity.kind !== "agent" || window.matchMedia("(prefers-reduced-motion: reduce)").matches)
      return;
    const from = composerNode.current?.getBoundingClientRect();
    const to = scroller.current?.getBoundingClientRect();
    if (!from || !to) return;
    if (flightTimer.current) clearTimeout(flightTimer.current);
    setFlight({
      id,
      content,
      style: {
        left: from.left + Math.min(180, from.width / 3),
        top: from.top + 24,
        "--flight-x": `${to.right - Math.min(260, to.width / 2) - from.left - Math.min(180, from.width / 3)}px`,
        "--flight-y": `${to.bottom - 64 - from.top - 24}px`,
      } as CSSProperties,
    });
    flightTimer.current = setTimeout(() => setFlight(null), 340);
  }
  /** sendQuickReply 双击或发送按钮直接发送独立文字，仍沿用当前会话权限和幂等编号。 */
  function sendQuickReply(content: string) {
    if (!content.trim() || sendingLock.current || !canSend) return;
    const body = {
      client_message_id: crypto.randomUUID(),
      content,
      message_type: "text",
      audience: "public",
    };
    void sendOutgoing(body, () => {
      animateText(content, body.client_message_id);
    });
  }
  /** sendText 从当前输入构建消息，引用内部备注时不允许发给访客。 */
  function sendText() {
    if (
      !draft.trim() ||
      sendingLock.current ||
      uploading ||
      recording ||
      !canSend ||
      composing.current
    )
      return;
    const body = {
      client_message_id: crypto.randomUUID(),
      content: draft,
      message_type: "text",
      audience,
      reply_to_id: reply?.id,
    };
    void sendOutgoing(body, () => {
      setDraft("");
      setReply(null);
      animateText(body.content, body.client_message_id);
    });
  }
  /** uploadAndSend 上传通过后才发送引用附件的消息。 */
  async function uploadAndSend(file: File, type?: string) {
    if (file.size > 20 * 1024 * 1024) {
      message.error("文件不能超过 20MB。");
      return;
    }
    setUploading(true);
    try {
      const form = new FormData();
      form.append("file", file);
      const attachment = await chat.request<{ id: string; content_type: string }>("/attachments", {
        method: "POST",
        body: form,
      });
      await sendOutgoing({
        client_message_id: crypto.randomUUID(),
        content: "",
        message_type:
          type ||
          (attachment.content_type.startsWith("image/")
            ? "image"
            : attachment.content_type.startsWith("audio/")
              ? "audio"
              : "file"),
        attachment_id: attachment.id,
        audience,
      });
    } catch (problem) {
      message.error(errorMessage(problem));
    } finally {
      setUploading(false);
    }
  }
  /** withdraw 撤回本人消息并重新同步，时间窗口由服务端检查。 */
  async function withdraw(item: ChatMessage) {
    try {
      await chat.withdraw(item.id);
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  return (
    <div className={`chat-main chat-thread ${identity.kind === "agent" ? "agent-thread" : ""}`}>
      <div
        className={`chat-connection ${chat.connected ? "connected" : "reconnecting"}`}
        style={{
          padding: "8px 20px",
          fontSize: 12,
          display: "flex",
          justifyContent: "space-between",
        }}
      >
        <span>
          {chat.connected ? "实时连接已建立" : "正在恢复实时连接 · 消息仍可通过安全请求发送"}
        </span>
        <span>{current ? conversationStatus[current.status] || current.status : "读取会话中"}</span>
      </div>
      <div style={{ padding: "0 14px" }}>
        {storageWarning && (
          <Alert
            type="warning"
            showIcon
            title={storageWarning}
            closable
            onClose={() => setStorageWarning("")}
          />
        )}
        <ErrorNotice error={chat.error} retry={chat.reload} />
      </div>
      <div
        className="chat-messages"
        ref={scroller}
        onScroll={() => {
          const node = scroller.current;
          if (node) setAtBottom(node.scrollHeight - node.scrollTop - node.clientHeight < 90);
        }}
      >
        {chat.hasOlder && (
          <div style={{ textAlign: "center", marginBottom: 16 }}>
            <Button
              size="small"
              onClick={() => {
                setAtBottom(false);
                void chat.loadOlder().catch((problem) => message.error(errorMessage(problem)));
              }}
            >
              加载更早消息
            </Button>
          </div>
        )}
        {chat.loading ? (
          <Spin />
        ) : !visibleMessages.length ? (
          <EmptyData text="还没有消息，可以从一句问候开始。" />
        ) : (
          <MessageHistory
            messages={visibleMessages}
            identity={identity}
            canSend={canSend}
            sending={sending}
            readSeq={chat.readSeq}
            onReply={setReply}
            onWithdraw={withdraw}
            onSend={sendOutgoing}
          />
        )}
        {pending
          .filter((item) => !confirmedIds.has(item.client_message_id))
          .map((item) => (
            <div
              className="message-row own message-pending"
              key={`pending-${item.client_message_id}`}
            >
              <div className="message-bubble">{item.content || "附件消息"}</div>
              <span className="message-delivery">
                <Spin size="small" /> 发送中
              </span>
            </div>
          ))}
        {failed
          .filter(
            (item) =>
              item.audience !== "internal" &&
              !confirmedIds.has(item.client_message_id) &&
              !pending.some((outgoing) => outgoing.client_message_id === item.client_message_id),
          )
          .map((item) => (
            <div className="message-row own" key={item.client_message_id}>
              <div className="message-bubble">{item.content || "附件消息"}</div>
              <Button
                danger
                type="link"
                size="small"
                loading={sending}
                onClick={() => void sendOutgoing(item)}
              >
                发送未确认，点击重试
              </Button>
            </div>
          ))}
      </div>
      {chat.typingUntil > Date.now() && (
        <div style={{ fontSize: 12, color: "var(--muted)", padding: "0 20px 8px" }}>
          对方正在输入…
        </div>
      )}
      {flight && (
        <div
          key={flight.id}
          className="message-send-flight"
          style={flight.style}
          aria-hidden="true"
        >
          {flight.content.slice(0, 80)}
        </div>
      )}
      <div
        className={`chat-composer ${identity.kind === "agent" && canSend ? "has-quick-replies" : ""}`}
      >
        {identity.kind === "agent" && canSend && identity.tenantId && (
          <QuickReplies
            tenantId={identity.tenantId}
            manages={canManageReplies}
            disabled={sending || uploading || recording}
            onInsert={(content) => setDraft((previous) => insertReplyText(previous, content))}
            onSend={sendQuickReply}
          />
        )}
        <div className="composer-input-area" ref={composerNode}>
          {current?.status === "closed" ? (
            <Alert type="info" showIcon title="本次咨询已结束，历史记录会保留。" />
          ) : !canSend ? (
            <Alert type="info" showIcon title="领取并正式接待此咨询后可以回复。" />
          ) : (
            <>
              {reply && (
                <div
                  style={{
                    fontSize: 12,
                    marginBottom: 8,
                    display: "flex",
                    justifyContent: "space-between",
                  }}
                >
                  <span>引用：{reply.content.slice(0, 100) || "附件消息"}</span>
                  <Button
                    size="small"
                    type="text"
                    aria-label="取消引用"
                    icon={<CloseOutlined />}
                    onClick={() => setReply(null)}
                  />
                </div>
              )}
              <Input.TextArea
                aria-label="消息内容"
                autoSize={{ minRows: 2, maxRows: 4 }}
                maxLength={10000}
                value={draft}
                placeholder="输入消息，Enter 发送，Shift + Enter 换行"
                onChange={(event) => {
                  setDraft(event.target.value);
                  chat.typing(!!event.target.value);
                }}
                onCompositionStart={() => {
                  composing.current = true;
                }}
                onCompositionEnd={() => {
                  composing.current = false;
                }}
                onKeyDown={(event) => {
                  if (
                    event.key === "Enter" &&
                    !event.shiftKey &&
                    !event.nativeEvent.isComposing &&
                    !composing.current
                  ) {
                    event.preventDefault();
                    sendText();
                  }
                }}
              />
              <div className="composer-bottom">
                <Space>
                  <input
                    hidden
                    type="file"
                    ref={fileInput}
                    onChange={(event) => {
                      const file = event.target.files?.[0];
                      event.target.value = "";
                      if (file) void uploadAndSend(file);
                    }}
                  />
                  <Tooltip title="图片或文件，最大 20MB">
                    <Button
                      size="small"
                      icon={<PaperClipOutlined />}
                      aria-label="上传图片或文件"
                      disabled={sending || recording}
                      loading={uploading}
                      onClick={() => fileInput.current?.click()}
                    />
                  </Tooltip>
                  <Tooltip title="最长 60 秒，点击停止后发送">
                    <Button
                      size="small"
                      danger={recording}
                      icon={<AudioOutlined />}
                      aria-label={recording ? "停止并发送录音" : "开始录音"}
                      disabled={sending || uploading}
                      onClick={() => void toggleRecording()}
                    >
                      {recording ? "停止并发送" : ""}
                    </Button>
                  </Tooltip>
                </Space>
                <Button
                  type="primary"
                  icon={<SendOutlined />}
                  loading={sending}
                  disabled={!draft.trim() || uploading || recording}
                  onClick={sendText}
                >
                  发送
                </Button>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
