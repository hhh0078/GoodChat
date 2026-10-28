/** 文件作用说明：为客服列表、单会话和访客复用同一身份连接，统一Worker降级、页面生命周期和实时状态。 */
"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import {
  RealtimeClient,
  type ConnectionStatus,
  type Diagnostic,
  type RealtimeListener,
} from "./client";
export type RealtimeIdentity = {
  tenantId?: string;
  token?: string;
  actorId: string;
  kind: "agent" | "visitor";
};
type Shared = { client: RealtimeClient; refs: number; dispose: () => void };
const connections = new Map<string, Shared>();
/** acquire 为相同身份复用连接，凭证仅作内存隔离键，不进入日志或浏览器持久存储。 */
function acquire(identity: RealtimeIdentity) {
  const key = JSON.stringify([identity.kind, identity.tenantId, identity.actorId, identity.token]);
  const existing = connections.get(key);
  if (existing) {
    existing.refs++;
    return existing;
  }
  const client = new RealtimeClient({
    /** ticket 只经请求头传身份，每次连接使用新的同源短期票据。 */
    ticket: async (signal) => {
      const result = await api<{ url: string }>("/realtime/ticket", {
        method: "POST",
        tenantId: identity.tenantId,
        headers: identity.token ? { Authorization: `Bearer ${identity.token}` } : undefined,
        signal,
        bodyJSON: {},
      });
      const url = new URL(result.url, location.origin);
      if (url.origin !== location.origin) throw new Error("实时连接来源无效");
      url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
      return url.href;
    },
    socket: (url) => new WebSocket(url),
  });
  let worker: Worker | undefined;
  let fallback: ReturnType<typeof setInterval> | undefined;
  /** useFallback 在Worker不可用或加载失败时退回普通时钟，服务端Ping仍负责网络探活。 */
  function useFallback() {
    worker?.terminate();
    worker = undefined;
    fallback ||= setInterval(() => client.tick(), 5000);
  }
  try {
    worker = new Worker(new URL("./scheduler.worker.ts", import.meta.url), {
      type: "module",
      name: "goodchat-realtime-clock",
    });
    worker.onmessage = () => client.tick();
    worker.onerror = useFallback;
  } catch {
    useFallback();
  }
  /** resume 仅在前台恢复时立即检查并补拉，普通切到后台不主动下班。 */
  function resume() {
    if (document.visibilityState === "visible") client.resume();
  }
  /** network 将系统网络事件作为重试提示，最终在线以服务器探活为准。 */
  function network() {
    client.setOnline(navigator.onLine);
  }
  window.addEventListener("online", network);
  window.addEventListener("offline", network);
  window.addEventListener("pageshow", resume);
  window.addEventListener("focus", resume);
  document.addEventListener("visibilitychange", resume);
  document.addEventListener("resume", resume);
  const shared: Shared = {
    client,
    refs: 1,
    /** dispose 最后订阅者离开时移除所有定时器和监听。 */
    dispose: () => {
      shared.refs--;
      if (shared.refs) return;
      connections.delete(key);
      client.stop();
      worker?.terminate();
      if (fallback) clearInterval(fallback);
      window.removeEventListener("online", network);
      window.removeEventListener("offline", network);
      window.removeEventListener("pageshow", resume);
      window.removeEventListener("focus", resume);
      document.removeEventListener("visibilitychange", resume);
      document.removeEventListener("resume", resume);
    },
  };
  connections.set(key, shared);
  if (navigator.onLine) client.start();
  else client.setOnline(false);
  return shared;
}
/** useRealtime 订阅持久事件提示与短期帧，补拉回调完成后统一展示连接成功。 */
export function useRealtime(
  identity: RealtimeIdentity,
  options: Omit<RealtimeListener, "onState"> & { enabled?: boolean } = {},
) {
  const callbacks = useRef(options);
  callbacks.current = options;
  const shared = useRef<Shared | null>(null);
  const [status, setStatus] = useState<ConnectionStatus>("idle");
  const [diagnostics, setDiagnostics] = useState<Diagnostic[]>([]);
  const { tenantId, token, actorId, kind } = identity;
  const enabled = options.enabled !== false && !!actorId;
  useEffect(() => {
    if (!enabled) {
      setStatus("idle");
      return;
    }
    const current = acquire({ tenantId, token, actorId, kind });
    shared.current = current;
    const unsubscribe = current.client.subscribe({
      onState: (next, records) => {
        setStatus(next);
        setDiagnostics(records);
      },
      onEvent: (event) => callbacks.current.onEvent?.(event),
      onFrame: (frame) => callbacks.current.onFrame?.(frame),
      onRecover: () => callbacks.current.onRecover?.(),
    });
    return () => {
      unsubscribe();
      current.dispose();
      if (shared.current === current) shared.current = null;
    };
  }, [tenantId, token, actorId, kind, enabled]);
  const send = useCallback(
    (frame: Record<string, unknown>) => shared.current?.client.send(frame) || false,
    [],
  );
  const reconnect = useCallback(() => shared.current?.client.reconnect(), []);
  return { connected: status === "connected", status, diagnostics, send, reconnect };
}
