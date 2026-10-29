/** 文件作用说明：通过持久游标与实时票据同步聊天，断线恢复时保留服务端顺序和幂等消息编号。 */
"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { ChatMessage, Conversation } from "@/lib/types";
import { useRealtime } from "@/lib/realtime/use-realtime";
import { createSyncPump } from "@/lib/realtime/sync-pump";
import { acknowledgePending, readPending, stagePending } from "@/lib/pending-messages";

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
  quick_reply_id?: string;
  quick_reply_version?: string;
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
  const [synced, setSynced] = useState(false);
  const [typingUntil, setTypingUntil] = useState(0);
  const [hasOlder, setHasOlder] = useState(false);
  const [readSeq, setReadSeq] = useState("0");
  const [revision, setRevision] = useState(0);
  const [unconfirmed, setUnconfirmed] = useState<Outgoing[]>([]);
  const [pendingError, setPendingError] = useState<unknown>(null);
  const synchronize = useRef<() => Promise<void>>(() => Promise.resolve());
  const cursor = useRef("");
  const olderPosition = useRef("");
  const snapshotEpoch = useRef(0);
  const historyScope = useRef("");
  const knownWithdrawals = useRef(new Map<string, string>());
  const typingSent = useRef(0);
  const scope = `${identity.kind}:${identity.tenantId || ""}:${identity.actorId}:${id}`;
  const activeScope = useRef(scope);
  const withdrawalScope = useRef(scope);
  activeScope.current = scope;
  const token = identity.token,
    tenantId = identity.tenantId;
  const pendingKey = `goodchat.draft.${tenantId || "visitor"}.${identity.actorId}.${id}.pending`;
  useEffect(() => {
    try {
      acknowledgePending(
        sessionStorage,
        pendingKey,
        messages
          .filter((item) => item.sender_id === identity.actorId && item.conversation_id === id)
          .map((item) => item.client_message_id)
          .filter((value): value is string => !!value),
      );
      setUnconfirmed(readPending(sessionStorage, pendingKey));
      setPendingError(null);
    } catch (problem) {
      setPendingError(problem);
    }
  }, [pendingKey, messages, identity.actorId, id]);
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
  /** mergeIncoming 保留本会话已确认的撤回编号，原消息尚未加载时也不能被迟到的分页恢复。 */
  const mergeIncoming = useCallback((previous: ChatMessage[], incoming: ChatMessage[]) => {
    for (const item of incoming)
      if (item.withdrawn_at) knownWithdrawals.current.set(item.id, item.withdrawn_at);
    return mergeMessages(previous, incoming, knownWithdrawals.current);
  }, []);
  const live = useRealtime(identity, {
    enabled: !!id,
    onEvent: (event) => {
      if (event.conversation_id === id) void synchronize.current().catch(() => {});
    },
    onRecover: () => synchronize.current(),
    onFrame: (frame) => {
      const data = frame.data;
      if (
        frame.type === "typing" &&
        data?.conversation_id === id &&
        data.participant_id !== identity.actorId
      )
        setTypingUntil(data.is_typing ? new Date(String(data.expires_at)).getTime() : 0);
    },
  });
  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    let initialized = false;
    let detailDirty = false;
    const seen = new Set<string>();
    const controller = new AbortController();
    snapshotEpoch.current++;
    if (withdrawalScope.current !== scope) {
      knownWithdrawals.current.clear();
      withdrawalScope.current = scope;
    }
    historyScope.current = "";
    olderPosition.current = "";
    cursor.current = "";
    setMessages([]);
    setConversation(null);
    setLoading(true);
    setSynced(false);
    setError(null);
    setReadSeq("0");
    /** snapshot 获取授权快照，游标失效时立即重建，再继续补齐期间发生的事件。 */
    async function snapshot() {
      const epoch = ++snapshotEpoch.current;
      const history = await request<{
        items: ChatMessage[];
        next_cursor: string;
        has_more: boolean;
        sync_cursor: string;
        history_scope: string;
      }>(`/conversations/${id}/messages?limit=50`, { signal: controller.signal });
      // 先取得消息水位再读取资料，避免旧资料对应的变更已经落在恢复游标之前。
      const detail = await request<Conversation>(`/conversations/${id}`, {
        signal: controller.signal,
      });
      if (cancelled || epoch !== snapshotEpoch.current) return;
      if (!history.sync_cursor) throw new Error("消息快照缺少恢复游标，请刷新后重试。");
      cursor.current = history.sync_cursor;
      historyScope.current = history.history_scope;
      olderPosition.current = history.next_cursor;
      setConversation(detail);
      setMessages(mergeIncoming([], history.items || []));
      setHasOlder(history.has_more);
      initialized = true;
      detailDirty = false;
    }
    /** scan 扫描服务端签名游标，遇到失效立即快照并继续，不能吞掉进行中收到的唤醒。 */
    async function scan() {
      try {
        if (!initialized) await snapshot();
        let more = true;
        let resets = 0;
        while (more && !cancelled) {
          let batch: { items: ChatEvent[]; next_cursor: string; has_more: boolean };
          try {
            batch = await request(
              `/conversations/${id}/events?after_cursor=${encodeURIComponent(cursor.current)}`,
              { signal: controller.signal },
            );
          } catch (problem) {
            if (
              problem instanceof ApiError &&
              problem.code === "resync_required" &&
              resets++ === 0
            ) {
              cursor.current = "";
              snapshotEpoch.current++;
              olderPosition.current = "";
              seen.clear();
              // 可见范围变更时先清除旧资料，重建失败也不能继续展示已无权的历史。
              setMessages([]);
              setConversation(null);
              setSynced(false);
              await snapshot();
              continue;
            }
            throw problem;
          }
          if (cancelled) return;
          for (const event of batch.items || []) {
            if (seen.has(event.id)) continue;
            seen.add(event.id);
            if (seen.size > 1000) seen.delete(seen.values().next().value!);
            if (event.event_type === "message.created")
              setMessages((previous) => mergeIncoming(previous, [event.payload as ChatMessage]));
            else if (event.event_type === "message.withdrawn") {
              const withdrawnAt = new Date().toISOString();
              knownWithdrawals.current.set(String(event.payload.id), withdrawnAt);
              setMessages((previous) =>
                withdrawMessages(previous, String(event.payload.id), withdrawnAt),
              );
            } else if (
              event.event_type === "read.updated" &&
              event.payload.participant_id !== identity.actorId &&
              event.payload.participant_kind === (identity.kind === "agent" ? "visitor" : "agent")
            )
              setReadSeq(String(event.payload.last_read_seq || "0"));
            else detailDirty = true;
          }
          cursor.current = batch.next_cursor;
          more = batch.has_more;
        }
        if (detailDirty && !cancelled) {
          const detail = await request<Conversation>(`/conversations/${id}`, {
            signal: controller.signal,
          });
          if (!cancelled) {
            setConversation(detail);
            detailDirty = false;
          }
        }
        if (!cancelled) {
          setError(null);
          setLoading(false);
          setSynced(true);
        }
      } catch (problem) {
        if (!cancelled) {
          if (problem instanceof ApiError && [401, 403, 404].includes(problem.status)) {
            snapshotEpoch.current++;
            olderPosition.current = "";
            setMessages([]);
            setConversation(null);
            initialized = false;
          }
          setError(problem);
          setLoading(false);
          setSynced(false);
        }
        throw problem;
      }
    }
    const pump = createSyncPump(scan);
    synchronize.current = pump.trigger;
    void pump.trigger().catch(() => {});
    const poll = setInterval(() => {
      void pump.trigger().catch(() => {});
    }, 4000);
    const lease = setInterval(
      () => setTypingUntil((previous) => (previous > Date.now() ? previous : 0)),
      1000,
    );
    return () => {
      cancelled = true;
      snapshotEpoch.current++;
      pump.stop();
      controller.abort();
      clearInterval(poll);
      clearInterval(lease);
      if (synchronize.current === pump.trigger) synchronize.current = () => Promise.resolve();
    };
  }, [id, request, identity.actorId, scope, mergeIncoming, revision]);
  /** send 在统一可信层暂存完整消息，覆盖原生及装修访客；只有服务端确认后才清理原身份会话的暂存。 */
  async function send(body: Outgoing) {
    const epoch = snapshotEpoch.current;
    try {
      stagePending(sessionStorage, pendingKey, body);
      if (activeScope.current === scope) {
        setUnconfirmed(readPending(sessionStorage, pendingKey));
        setPendingError(null);
      }
    } catch (problem) {
      if (activeScope.current === scope) setPendingError(problem);
      throw problem;
    }
    const item = await request<ChatMessage>(`/conversations/${id}/messages`, {
      method: "POST",
      bodyJSON: body,
    });
    try {
      acknowledgePending(sessionStorage, pendingKey, [body.client_message_id]);
      if (activeScope.current === scope) setUnconfirmed(readPending(sessionStorage, pendingKey));
    } catch (problem) {
      if (activeScope.current === scope) setPendingError(problem);
    }
    if (activeScope.current === scope && epoch === snapshotEpoch.current)
      setMessages((previous) => mergeIncoming(previous, [item]));
    return item;
  }
  /** withdraw 仅在服务端确认作者和时间窗后更新消息及引用，失败时保留原内容。 */
  async function withdraw(messageId: string) {
    await request(`/conversations/${id}/messages/${messageId}/withdraw`, {
      method: "POST",
      bodyJSON: {},
    });
    if (activeScope.current === scope) {
      const withdrawnAt = new Date().toISOString();
      knownWithdrawals.current.set(messageId, withdrawnAt);
      setMessages((previous) => withdrawMessages(previous, messageId, withdrawnAt));
    }
  }
  /** loadOlder 使用消息历史游标追加旧记录，不覆盖实时新增消息。 */
  async function loadOlder() {
    const before = olderPosition.current;
    if (!before) return;
    const epoch = snapshotEpoch.current;
    const visibleScope = historyScope.current;
    const data = await request<{
      items: ChatMessage[];
      next_cursor: string;
      has_more: boolean;
      history_scope: string;
    }>(`/conversations/${id}/messages?before_seq=${encodeURIComponent(before)}&limit=50`);
    if (
      activeScope.current !== scope ||
      epoch !== snapshotEpoch.current ||
      before !== olderPosition.current
    )
      return;
    if (data.history_scope !== visibleScope) {
      await synchronize.current();
      return;
    }
    olderPosition.current = data.next_cursor;
    setMessages((previous) => mergeIncoming(previous, data.items || []));
    setHasOlder(data.has_more);
  }
  /** typing 对输入状态限流，短期状态通过服务端租约自动过期。 */
  function typing(active: boolean) {
    if (active && Date.now() - typingSent.current < 1200) return;
    typingSent.current = Date.now();
    live.send({ type: "typing", conversation_id: id, is_typing: active });
  }
  return {
    messages,
    unconfirmed,
    pendingError,
    conversation,
    error,
    loading,
    connected: live.connected && synced,
    connectionStatus: live.status,
    diagnostics: live.diagnostics,
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
