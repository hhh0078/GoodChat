/** 文件作用说明：提供访客与客服共用的消息界面、幂等重试、附件、语音、引用与可见已读。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { Alert, App, Button, Input, Popconfirm, Popover, Space, Spin } from "antd";
import {
  CloseOutlined,
  FileOutlined,
  PictureOutlined,
  PlusOutlined,
  SendOutlined,
  VideoCameraOutlined,
} from "@ant-design/icons";
import { ErrorNotice, EmptyData } from "@/components/page-state";
import { errorMessage } from "@/lib/api";
import type { ChatMessage } from "@/lib/types";
import { useChat, type ChatIdentity, type Outgoing } from "./use-chat";
import { MessageHistory } from "./message-history";
import { animateMessageFlight } from "@/lib/message-flight";
import type { QuickReply } from "@/lib/quick-replies";
import { acknowledgePending, readPending, stagePending } from "@/lib/pending-messages";
import { QuickReplies } from "./quick-replies";
import { useVisitorReminders } from "./use-visitor-reminders";

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
  onRead,
  readOnly = false,
  canManageReplies = false,
  notificationSound,
}: {
  id: string;
  identity: ChatIdentity;
  onStatus?: (status: string) => void;
  onRead?: () => void;
  readOnly?: boolean;
  canManageReplies?: boolean;
  notificationSound?: string;
}) {
  const chat = useChat(id, identity);
  useVisitorReminders(chat.messages, {
    enabled: identity.kind === "visitor",
    loading: chat.loading || !chat.conversation,
    conversationId: id,
    sound: notificationSound,
  });
  const { message } = App.useApp();
  const [draft, setDraft] = useState("");
  const audience = "public";
  const [pending, setPending] = useState<Outgoing[]>([]);
  const sendingLock = useRef(false);
  const flightCleanup = useRef<() => void>(() => {});
  const [attachmentsOpen, setAttachmentsOpen] = useState(false);
  const composerNode = useRef<HTMLDivElement | null>(null);
  const [reply, setReply] = useState<ChatMessage | null>(null);
  const [failed, setFailed] = useState<Outgoing[]>([]);
  const [sending, setSending] = useState(false);
  const [storageWarning, setStorageWarning] = useState("");
  const [draftHydrated, setDraftHydrated] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [atBottom, setAtBottom] = useState(true);
  const [pageVisible, setPageVisible] = useState(false);
  const [readAttempt, setReadAttempt] = useState(0);
  useEffect(() => {
    /** updateVisibility 返回前台时重新尝试可见已读，不把后台消息误标已读。 */
    function updateVisibility() {
      setPageVisible(document.visibilityState === "visible");
    }
    updateVisibility();
    document.addEventListener("visibilitychange", updateVisibility);
    return () => document.removeEventListener("visibilitychange", updateVisibility);
  }, []);
  const scroller = useRef<HTMLDivElement | null>(null);
  const fileInput = useRef<HTMLInputElement | null>(null);
  const composing = useRef(false);
  const mounted = useRef(true);
  const lastRead = useRef("");
  const draftKey = `goodchat.draft.${identity.tenantId || "visitor"}.${identity.actorId}.${id}`;
  const scope = useRef(draftKey);
  scope.current = draftKey;
  const current = chat.conversation;
  const readContext = `${current?.current_assignment_id || ""}:${current?.current_agent_id || ""}`;
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
    lastRead.current = "";
    return () => {
      mounted.current = false;
      flightCleanup.current();
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
    if (atBottom && !chat.loading)
      scroller.current?.scrollTo({ top: scroller.current.scrollHeight, behavior: "smooth" });
  }, [chat.messages.length, pending.length, atBottom, chat.loading]);
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
    const latest =
      visibleMessages.at(-1)?.seq || visibleMessages.at(-1)?.event_seq || (current ? "0" : "");
    const readKey = `${readContext}:${latest}`;
    if (!latest || chat.loading || !atBottom || !pageVisible || lastRead.current === readKey)
      return;
    const controller = new AbortController();
    let retry: ReturnType<typeof setTimeout> | undefined;
    let frame = 0;
    let visibleFrames = 0;
    /** reportVisibleRead 等待真实布局连续两帧到底，避免平滑滚动尚未展示末条消息就清除未读。 */
    function reportVisibleRead() {
      const node = scroller.current;
      if (controller.signal.aborted || document.visibilityState !== "visible") return;
      const visible =
        node &&
        node.clientHeight > 0 &&
        node.getClientRects().length > 0 &&
        node.scrollHeight - node.scrollTop - node.clientHeight <= 2;
      visibleFrames = visible ? visibleFrames + 1 : 0;
      if (visibleFrames < 2) {
        frame = requestAnimationFrame(reportVisibleRead);
        return;
      }
      lastRead.current = readKey;
      chat
        .request(`/conversations/${id}/read-cursor`, {
          method: "POST",
          signal: controller.signal,
          bodyJSON: { last_read_seq: latest },
        })
        .then(() => {
          if (!controller.signal.aborted) onRead?.();
        })
        .catch(() => {
          if (controller.signal.aborted) return;
          if (lastRead.current === readKey) lastRead.current = "";
          retry = setTimeout(() => setReadAttempt((value) => value + 1), 4000);
        });
    }
    frame = requestAnimationFrame(reportVisibleRead);
    return () => {
      controller.abort();
      cancelAnimationFrame(frame);
      if (retry) clearTimeout(retry);
      if (lastRead.current === readKey) lastRead.current = "";
    };
  }, [
    chat.messages,
    chat.loading,
    atBottom,
    pageVisible,
    id,
    chat.request,
    onRead,
    readAttempt,
    readContext,
  ]);
  /** sendOutgoing 发送前同步保存原会话完整消息，确认后仅清理原编号，离开会话不丢失在途请求。 */
  async function sendOutgoing(body: Outgoing, onQueued?: () => void): Promise<boolean> {
    if (sendingLock.current) return false;
    const operationScope = draftKey;
    const storageKey = `${operationScope}.pending`;
    /** isCurrent 拦截切换会话后的异步界面更新，存储清理仍使用原会话键。 */
    const isCurrent = () => mounted.current && scope.current === operationScope;
    try {
      stagePending(sessionStorage, storageKey, body);
    } catch (problem) {
      setStorageWarning(errorMessage(problem));
      message.error(errorMessage(problem));
      return false;
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
      if (!isCurrent()) return true;
      setFailed((previous) =>
        previous.filter((item) => item.client_message_id !== body.client_message_id),
      );
      chat.typing(false);
      setAtBottom(true);
      return true;
    } catch (problem) {
      if (!isCurrent()) return false;
      setFailed((previous) =>
        previous.some((item) => item.client_message_id === body.client_message_id)
          ? previous
          : [...previous, body],
      );
      message.error(errorMessage(problem));
      return false;
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
  /** animateText 使用真实输入框与对应气泡布局，让客服和访客共享平滑发送效果。 */
  function animateText(content: string, clientId: string) {
    const root = scroller.current?.closest<HTMLElement>(".chat-thread");
    const input = composerNode.current?.querySelector<HTMLElement>("textarea");
    if (!root || !input) return;
    flightCleanup.current();
    flightCleanup.current = animateMessageFlight(root, input, clientId, content);
  }
  /** sendQuickReply 发送服务端模板引用，成功结果供快捷区刷新真实使用次数，保留未发送草稿。 */
  async function sendQuickReply(item: QuickReply): Promise<boolean> {
    if (sendingLock.current || !canSend) return false;
    const body: Outgoing = {
      client_message_id: crypto.randomUUID(),
      quick_reply_id: item.id,
      quick_reply_version: String(item.version),
      content: item.content,
      message_type: item.message_type,
      audience: "public",
      ...(item.attachment_id ? { attachment_id: item.attachment_id } : {}),
    };
    return sendOutgoing(body, () => {
      if (item.content) animateText(item.content, body.client_message_id);
    });
  }
  /** chooseAttachment 仅改变系统文件选择器范围，具体文件内容与大小仍由服务端核验。 */
  function chooseAttachment(kind: "image" | "file" | "video") {
    const node = fileInput.current;
    if (!node) return;
    node.accept = kind === "image" ? "image/*" : kind === "video" ? "video/*" : "";
    setAttachmentsOpen(false);
    node.click();
  }
  /** sendText 从当前输入构建消息，引用内部备注时不允许发给访客。 */
  function sendText() {
    if (!draft.trim() || sendingLock.current || uploading || !canSend || composing.current) return;
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
  /** removeFailed 由用户明确移除本机待确认记录，不撤回或删除服务端已经保存的消息。 */
  function removeFailed(clientId: string) {
    try {
      acknowledgePending(sessionStorage, `${draftKey}.pending`, [clientId]);
      setFailed((previous) => previous.filter((item) => item.client_message_id !== clientId));
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
          if (node) setAtBottom(node.scrollHeight - node.scrollTop - node.clientHeight <= 2);
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
              data-client-message-id={item.client_message_id}
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
              <Popconfirm
                title="移除本机重试记录？"
                description="请先刷新核对。仅移除本机记录，服务端已保存的消息会保留。"
                onConfirm={() => removeFailed(item.client_message_id)}
              >
                <Button size="small" type="text">
                  移除记录
                </Button>
              </Popconfirm>
            </div>
          ))}
      </div>
      {chat.typingUntil > Date.now() && (
        <div style={{ fontSize: 12, color: "var(--muted)", padding: "0 20px 8px" }}>
          对方正在输入…
        </div>
      )}
      <div
        className={`chat-composer ${identity.kind === "agent" && canSend ? "has-quick-replies" : ""}`}
      >
        {identity.kind === "agent" && canSend && identity.tenantId && (
          <QuickReplies
            tenantId={identity.tenantId}
            manages={canManageReplies}
            disabled={sending || uploading}
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
              <div
                className={`message-capsule ${identity.kind === "agent" ? "agent-capsule" : "visitor-capsule"}`}
              >
                <Popover
                  trigger="click"
                  placement="topLeft"
                  open={attachmentsOpen}
                  onOpenChange={setAttachmentsOpen}
                  content={
                    <div className="message-attachment-menu">
                      <Button
                        type="text"
                        icon={<PictureOutlined />}
                        onClick={() => chooseAttachment("image")}
                      >
                        图片
                      </Button>
                      <Button
                        type="text"
                        icon={<FileOutlined />}
                        onClick={() => chooseAttachment("file")}
                      >
                        文件
                      </Button>
                      <Button
                        type="text"
                        icon={<VideoCameraOutlined />}
                        onClick={() => chooseAttachment("video")}
                      >
                        视频
                      </Button>
                    </div>
                  }
                >
                  <Button
                    type="text"
                    shape="circle"
                    icon={<PlusOutlined />}
                    aria-label="添加附件"
                    loading={uploading}
                    disabled={sending || uploading}
                  />
                </Popover>
                <Input.TextArea
                  aria-label="消息内容"
                  autoSize={{ minRows: 1, maxRows: 4 }}
                  maxLength={10000}
                  value={draft}
                  placeholder="输入消息…"
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
                <Button
                  type="primary"
                  shape={identity.kind === "agent" ? "round" : "circle"}
                  icon={<SendOutlined />}
                  aria-label="发送消息"
                  loading={sending}
                  disabled={!draft.trim() || uploading}
                  onClick={sendText}
                >
                  {identity.kind === "agent" ? "发送" : null}
                </Button>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
