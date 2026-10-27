/** 文件作用说明：提供访客与客服共用的消息界面、幂等重试、附件、语音、引用与可见已读。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { Alert, App, Button, Input, Popconfirm, Select, Space, Spin, Tag, Tooltip } from "antd";
import { AudioOutlined, CloseOutlined, PaperClipOutlined, SendOutlined } from "@ant-design/icons";
import { ErrorNotice, EmptyData } from "@/components/page-state";
import { errorMessage } from "@/lib/api";
import type { ChatMessage, Resource } from "@/lib/types";
import { useChat, type ChatIdentity, type Outgoing } from "./use-chat";
import { AttachmentView } from "./attachment";
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
}: {
  id: string;
  identity: ChatIdentity;
  onStatus?: (status: string) => void;
  readOnly?: boolean;
}) {
  const chat = useChat(id, identity);
  const { message } = App.useApp();
  const [draft, setDraft] = useState("");
  const [audience, setAudience] = useState("public");
  const [reply, setReply] = useState<ChatMessage | null>(null);
  const [failed, setFailed] = useState<Outgoing[]>([]);
  const [sending, setSending] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [recording, setRecording] = useState(false);
  const [atBottom, setAtBottom] = useState(true);
  const scroller = useRef<HTMLDivElement | null>(null);
  const fileInput = useRef<HTMLInputElement | null>(null);
  const composing = useRef(false);
  const mounted = useRef(true);
  const lastRead = useRef("0");
  const recorder = useRef<MediaRecorder | null>(null);
  const stream = useRef<MediaStream | null>(null);
  const recordingTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const draftKey = `goodchat.draft.${identity.tenantId || "visitor"}.${identity.actorId}.${id}`;
  const current = chat.conversation;
  const [quickReplies, setQuickReplies] = useState<Resource[]>([]);
  const canSend =
    !!current &&
    !readOnly &&
    current?.status !== "closed" &&
    (identity.kind === "visitor" ||
      (current?.status === "active" && current.current_agent_id === identity.actorId));
  useEffect(() => {
    if (identity.kind !== "agent") return;
    const controller = new AbortController();
    chat
      .request<{ items: Resource[] }>("/tenant/quick-replies", { signal: controller.signal })
      .then((data) => setQuickReplies((data.items || []).filter((item) => item.enabled !== false)))
      .catch(() => setQuickReplies([]));
    return () => controller.abort();
  }, [chat.request, identity.kind]);
  useEffect(() => {
    mounted.current = true;
    try {
      setDraft(sessionStorage.getItem(draftKey) || "");
    } catch {
      setDraft("");
    }
    try {
      const pending = JSON.parse(sessionStorage.getItem(`${draftKey}.pending`) || "[]");
      setFailed(
        Array.isArray(pending)
          ? pending.filter(
              (item) =>
                item &&
                typeof item.client_message_id === "string" &&
                typeof item.content === "string",
            )
          : [],
      );
    } catch {
      setFailed([]);
    }
    setReply(null);
    lastRead.current = "0";
    return () => {
      mounted.current = false;
      if (recordingTimer.current) clearTimeout(recordingTimer.current);
      if (recorder.current) {
        recorder.current.onstop = null;
        if (recorder.current.state !== "inactive") recorder.current.stop();
      }
      stream.current?.getTracks().forEach((track) => track.stop());
    };
  }, [draftKey]);
  useEffect(() => {
    try {
      sessionStorage.setItem(`${draftKey}.pending`, JSON.stringify(failed));
    } catch {
      /* 失败消息仍保留在内存中供重试。 */
    }
  }, [failed, draftKey]);
  useEffect(() => {
    const confirmed = new Set(
      chat.messages
        .filter((item) => item.sender_id === identity.actorId)
        .map((item) => item.client_message_id),
    );
    setFailed((previous) =>
      previous.some((item) => confirmed.has(item.client_message_id))
        ? previous.filter((item) => !confirmed.has(item.client_message_id))
        : previous,
    );
  }, [chat.messages, identity.actorId]);
  useEffect(() => {
    try {
      sessionStorage.setItem(draftKey, draft);
    } catch {
      /* 存储被禁用时当前输入仍保存在内存。 */
    }
  }, [draft, draftKey]);
  useEffect(() => {
    if (current) onStatus?.(current.status);
  }, [current, onStatus]);
  useEffect(() => {
    if (atBottom)
      scroller.current?.scrollTo({ top: scroller.current.scrollHeight, behavior: "smooth" });
  }, [chat.messages.length, atBottom]);
  useEffect(() => {
    const latest = chat.messages.at(-1)?.seq || chat.messages.at(-1)?.event_seq;
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
  /** sendOutgoing 保留失败消息的原始编号，重试不会产生重复消息。 */
  async function sendOutgoing(body: Outgoing) {
    setSending(true);
    try {
      await chat.send(body);
      setFailed((previous) =>
        previous.filter((item) => item.client_message_id !== body.client_message_id),
      );
      chat.typing(false);
      setAtBottom(true);
    } catch (problem) {
      setFailed((previous) =>
        previous.some((item) => item.client_message_id === body.client_message_id)
          ? previous
          : [...previous, body],
      );
      message.error(errorMessage(problem));
    } finally {
      setSending(false);
    }
  }
  /** sendText 从当前输入构建消息，引用内部备注时不允许发给访客。 */
  function sendText() {
    if (!draft.trim() || sending || !canSend || composing.current) return;
    const body = {
      client_message_id: crypto.randomUUID(),
      content: draft,
      message_type: "text",
      audience,
      reply_to_id: reply?.id,
    };
    setDraft("");
    setReply(null);
    void sendOutgoing(body);
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
  /** toggleRecording 只在用户点击后请求麦克风，停止时释放设备并提交真实音频。 */
  async function toggleRecording() {
    if (recording) {
      recorder.current?.stop();
      return;
    }
    if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === "undefined") {
      message.warning("当前浏览器不支持录音，请上传音频文件。");
      return;
    }
    try {
      const media = await navigator.mediaDevices.getUserMedia({ audio: true });
      if (!mounted.current) {
        media.getTracks().forEach((track) => track.stop());
        return;
      }
      stream.current = media;
      const mime = ["audio/webm", "audio/mp4"].find((value) =>
        MediaRecorder.isTypeSupported(value),
      );
      const instance = new MediaRecorder(media, mime ? { mimeType: mime } : undefined);
      const chunks: BlobPart[] = [];
      recorder.current = instance;
      instance.ondataavailable = (event) => {
        if (event.data.size) chunks.push(event.data);
      };
      instance.onstop = () => {
        if (recordingTimer.current) clearTimeout(recordingTimer.current);
        media.getTracks().forEach((track) => track.stop());
        setRecording(false);
        const blob = new Blob(chunks, { type: instance.mimeType });
        if (blob.size)
          void uploadAndSend(
            new File(
              [blob],
              `语音-${Date.now()}.${instance.mimeType.includes("mp4") ? "m4a" : "webm"}`,
              { type: instance.mimeType },
            ),
            "audio",
          );
      };
      instance.start();
      setRecording(true);
      recordingTimer.current = setTimeout(() => instance.stop(), 60000);
    } catch (problem) {
      message.error(errorMessage(problem));
      stream.current?.getTracks().forEach((track) => track.stop());
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
    <div className="chat-main">
      <div
        style={{
          padding: "8px 20px",
          background: chat.connected ? "#f3f7ee" : "#fff8e9",
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
        ) : !chat.messages.length ? (
          <EmptyData text="还没有消息，可以从一句问候开始。" />
        ) : (
          chat.messages.map((item) => {
            const own = item.sender_id === identity.actorId;
            return (
              <div
                key={item.id}
                className={`message-row ${own ? "own" : ""} ${item.audience === "internal" ? "internal" : ""}`}
              >
                <div className="message-meta">
                  {item.audience === "internal" ? "内部备注 · " : ""}
                  {item.sender_name ||
                    ({ visitor: "访客", agent: "客服", system: "系统" } as Record<string, string>)[
                      item.sender_kind
                    ] ||
                    "消息"}{" "}
                  ·{" "}
                  {item.sender_kind === "system" && item.source && (
                    <>
                      {item.source === "welcome"
                        ? "欢迎引导"
                        : item.source === "keyword"
                          ? "规则回复"
                          : "系统消息"}{" "}
                      ·{" "}
                    </>
                  )}
                  {item.created_at
                    ? new Date(item.created_at).toLocaleTimeString("zh-CN", {
                        hour: "2-digit",
                        minute: "2-digit",
                      })
                    : ""}
                </div>
                <div className="message-bubble">
                  {item.withdrawn_at ? (
                    <span className="muted">消息已撤回</span>
                  ) : (
                    <>
                      {item.reply && (
                        <div
                          style={{
                            fontSize: 12,
                            borderLeft: "2px solid #a8b79d",
                            paddingLeft: 10,
                            marginBottom: 8,
                            color: "#6c7767",
                          }}
                        >
                          引用：{item.reply.withdrawn ? "消息已撤回" : item.reply.content}
                        </div>
                      )}
                      {item.content}
                      {item.attachment && (
                        <AttachmentView attachment={item.attachment} identity={identity} />
                      )}
                      {item.sender_kind === "system" && Array.isArray(item.options) && (
                        <Space wrap style={{ marginTop: 10, display: "flex" }}>
                          {item.options.map((option) => (
                            <Button
                              key={option.id}
                              size="small"
                              disabled={
                                !canSend ||
                                identity.kind !== "visitor" ||
                                sending ||
                                chat.messages.some(
                                  (message) =>
                                    message.client_message_id === option.client_message_id,
                                )
                              }
                              onClick={() =>
                                void sendOutgoing({
                                  client_message_id: option.client_message_id,
                                  content: option.value,
                                  message_type: "text",
                                  audience: "public",
                                })
                              }
                            >
                              {option.label}
                            </Button>
                          ))}
                        </Space>
                      )}
                    </>
                  )}
                </div>
                {!item.withdrawn_at && (
                  <Space size={10} style={{ fontSize: 11, marginTop: 4 }}>
                    {canSend && (
                      <a
                        onClick={() => {
                          setReply(item);
                          if (item.audience === "internal") setAudience("internal");
                        }}
                      >
                        引用
                      </a>
                    )}
                    {own && canSend && (
                      <Popconfirm title="撤回这条消息？" onConfirm={() => withdraw(item)}>
                        <a>撤回</a>
                      </Popconfirm>
                    )}
                    {own && BigInt(chat.readSeq) >= BigInt(item.seq || item.event_seq || 0) && (
                      <span className="muted">对方已读</span>
                    )}
                  </Space>
                )}
              </div>
            );
          })
        )}
        {failed.map((item) => (
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
        <div style={{ fontSize: 12, color: "#68776e", padding: "0 20px 8px" }}>对方正在输入…</div>
      )}
      <div className="chat-composer">
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
            {identity.kind === "agent" && quickReplies.length > 0 && (
              <Select<string>
                aria-label="选择快捷话术"
                value={undefined}
                placeholder="插入快捷话术"
                showSearch
                optionFilterProp="label"
                style={{ width: "100%", marginBottom: 10 }}
                options={quickReplies.map((item) => ({
                  label: String(item.title),
                  value: item.id,
                }))}
                onChange={(value) => {
                  const item = quickReplies.find((record) => record.id === value);
                  if (item) setDraft(String(item.content || ""));
                }}
              />
            )}
            <Input.TextArea
              aria-label={audience === "internal" ? "内部备注内容" : "消息内容"}
              autoSize={{ minRows: 2, maxRows: 5 }}
              maxLength={10000}
              value={draft}
              placeholder={
                audience === "internal"
                  ? "仅团队可见的内部备注"
                  : "输入消息，Enter 发送，Shift + Enter 换行"
              }
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
                    onClick={() => void toggleRecording()}
                  >
                    {recording ? "停止并发送" : ""}
                  </Button>
                </Tooltip>
                {identity.kind === "agent" && (
                  <Select
                    size="small"
                    aria-label="消息受众"
                    value={audience}
                    onChange={(value) => {
                      setAudience(value);
                      if (value === "public" && reply?.audience === "internal") setReply(null);
                    }}
                    options={[
                      { label: "回复访客", value: "public" },
                      { label: "内部备注", value: "internal" },
                    ]}
                    style={{ width: 110 }}
                  />
                )}
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
  );
}
