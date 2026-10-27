/** 文件作用说明：通过持久游标与实时票据同步聊天，断线恢复时保留服务端顺序和幂等消息编号。 */
"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { ChatMessage, Conversation } from "@/lib/types";

export type ChatIdentity = {
  tenantId?: string;
  token?: string;
  actorId: string;
  kind: "agent" | "visitor";
};
export type Outgoing = {
  client_message_id: string;
  content: string;
  message_type: string;
  audience: string;
  attachment_id?: string;
  reply_to_id?: string;
};
type ChatEvent = {
  id: string;
  seq: string;
  event_type: string;
  conversation_id: string;
  payload: Record<string, unknown>;
};
import { mergeMessages, withdrawMessages } from "@/lib/chat-state";
/** useChat 绑定单个身份与会话，票据不写入浏览器存储，失败消息保留原编号重试。 */
export function useChat(id: string, identity: ChatIdentity) {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [conversation, setConversation] = useState<Conversation | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [connected, setConnected] = useState(false);
  const [typingUntil, setTypingUntil] = useState(0);
  const [olderCursor, setOlderCursor] = useState("");
  const [hasOlder, setHasOlder] = useState(false);
  const [readSeq, setReadSeq] = useState("0");
  const [revision, setRevision] = useState(0);
  const socket = useRef<WebSocket | null>(null);
  const cursor = useRef("");
  const typingSent = useRef(0);
  const token = identity.token,
    tenantId = identity.tenantId;
  /** request 仅通过请求头附加独立访客凭证，不把凭证放进网址。 */
  const request = useCallback(
    <T>(path: string, options: Parameters<typeof api>[1] = {}) =>
      api<T>(path, {
        ...options,
        tenantId,
        headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...options.headers },
      }),
    [tenantId, token],
  );
  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    let polling = false;
    let reconnect: ReturnType<typeof setTimeout> | undefined;
    const controller = new AbortController();
    cursor.current = "";
    setMessages([]);
    setConversation(null);
    setLoading(true);
    setError(null);
    setReadSeq("0");
    /** snapshot 读取授权快照与最近历史，不根据可见序号推断全局连续性。 */
    async function snapshot() {
      const [detail, history] = await Promise.all([
        request<Conversation>(`/conversations/${id}`, { signal: controller.signal }),
        request<{ items: ChatMessage[]; next_cursor: string; has_more: boolean }>(
          `/conversations/${id}/messages?limit=50`,
          { signal: controller.signal },
        ),
      ]);
      if (cancelled) return;
      setConversation(detail);
      setMessages(history.items || []);
      setOlderCursor(history.next_cursor);
      setHasOlder(history.has_more);
    }
    /** sync 扫描持久事件并只使用服务端签名游标推进恢复位置。 */
    async function sync() {
      if (cancelled || polling) return;
      polling = true;
      try {
        let more = true;
        while (more && !cancelled) {
          const batch = await request<{
            items: ChatEvent[];
            next_cursor: string;
            has_more: boolean;
          }>(`/conversations/${id}/events?after_cursor=${encodeURIComponent(cursor.current)}`, {
            signal: controller.signal,
          });
          if (cancelled) return;
          let changed = false;
          for (const event of batch.items || []) {
            if (event.event_type === "message.created")
              setMessages((previous) => mergeMessages(previous, [event.payload as ChatMessage]));
            else if (event.event_type === "message.withdrawn")
              setMessages((previous) =>
                withdrawMessages(previous, String(event.payload.id), new Date().toISOString()),
              );
            else if (
              event.event_type === "read.updated" &&
              event.payload.participant_id !== identity.actorId
            )
              setReadSeq(String(event.payload.last_read_seq || "0"));
            else changed = true;
          }
          cursor.current = batch.next_cursor;
          more = batch.has_more;
          if (changed) {
            const detail = await request<Conversation>(`/conversations/${id}`, {
              signal: controller.signal,
            });
            if (!cancelled) setConversation(detail);
          }
        }
        if (!cancelled) setError(null);
      } catch (problem) {
        if (!cancelled) {
          if (problem instanceof ApiError && problem.code === "resync_required") {
            cursor.current = "";
            await snapshot().catch(setError);
          } else setError(problem);
        }
      } finally {
        polling = false;
      }
    }
    /** connect 重新申请一次性票据，在断开后恢复持久事件。 */
    async function connect() {
      try {
        const ticket = await request<{ url: string }>("/realtime/ticket", {
          method: "POST",
          bodyJSON: { conversation_id: id },
        });
        if (cancelled) return;
        const url = new URL(ticket.url, location.origin);
        if (url.origin !== location.origin) throw new Error("实时连接来源无效");
        url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
        const ws = new WebSocket(url);
        socket.current = ws;
        ws.onopen = () => {
          if (cancelled) {
            ws.close();
            return;
          }
          setConnected(true);
          void sync();
        };
        ws.onmessage = (event) => {
          try {
            const data = JSON.parse(String(event.data));
            if (data.type === "event") void sync();
            if (
              data.type === "typing" &&
              data.data?.conversation_id === id &&
              data.data?.participant_id !== identity.actorId
            )
              setTypingUntil(data.data.is_typing ? new Date(data.data.expires_at).getTime() : 0);
          } catch {
            /* 非法实时帧不推进持久游标。 */
          }
        };
        ws.onclose = () => {
          if (!cancelled) {
            setConnected(false);
            reconnect = setTimeout(() => void connect(), 3000);
          }
        };
        ws.onerror = () => ws.close();
      } catch (problem) {
        if (!cancelled) {
          setConnected(false);
          setError(problem);
          reconnect = setTimeout(() => void connect(), 5000);
        }
      }
    }
    void snapshot()
      .then(() => {
        if (!cancelled) {
          setLoading(false);
          void sync();
          void connect();
        }
      })
      .catch((problem) => {
        if (!cancelled) {
          setError(problem);
          setLoading(false);
        }
      });
    const poll = setInterval(() => void sync(), 4000);
    const heartbeat = setInterval(() => {
      if (socket.current?.readyState === WebSocket.OPEN)
        socket.current.send(JSON.stringify({ type: "heartbeat" }));
    }, 15000);
    const lease = setInterval(
      () => setTypingUntil((previous) => (previous > Date.now() ? previous : 0)),
      1000,
    );
    return () => {
      cancelled = true;
      controller.abort();
      clearInterval(poll);
      clearInterval(heartbeat);
      clearInterval(lease);
      if (reconnect) clearTimeout(reconnect);
      socket.current?.close();
      socket.current = null;
    };
  }, [id, request, identity.actorId, revision]);
  /** send 提交消息，只有服务端确认写入后才加入成功消息列表。 */
  async function send(body: Outgoing) {
    const item = await request<ChatMessage>(`/conversations/${id}/messages`, {
      method: "POST",
      bodyJSON: body,
    });
    setMessages((previous) => mergeMessages(previous, [item]));
    return item;
  }
  /** withdraw 仅在服务端确认作者和时间窗后更新消息及引用，失败时保留原内容。 */
  async function withdraw(messageId: string) {
    await request(`/conversations/${id}/messages/${messageId}/withdraw`, {
      method: "POST",
      bodyJSON: {},
    });
    setMessages((previous) => withdrawMessages(previous, messageId, new Date().toISOString()));
  }
  /** loadOlder 使用消息历史游标追加旧记录，不覆盖实时新增消息。 */
  async function loadOlder() {
    const data = await request<{ items: ChatMessage[]; next_cursor: string; has_more: boolean }>(
      `/conversations/${id}/messages?before_seq=${encodeURIComponent(olderCursor)}&limit=50`,
    );
    setMessages((previous) => mergeMessages(previous, data.items || []));
    setOlderCursor(data.next_cursor);
    setHasOlder(data.has_more);
  }
  /** typing 对输入状态限流，短期状态通过服务端租约自动过期。 */
  function typing(active: boolean) {
    if (active && Date.now() - typingSent.current < 1200) return;
    typingSent.current = Date.now();
    if (socket.current?.readyState === WebSocket.OPEN)
      socket.current.send(
        JSON.stringify({ type: "typing", conversation_id: id, is_typing: active }),
      );
  }
  return {
    messages,
    conversation,
    error,
    loading,
    connected,
    typingUntil,
    hasOlder,
    readSeq,
    request,
    send,
    withdraw,
    loadOlder,
    typing,
    reload: () => setRevision((value) => value + 1),
  };
}
