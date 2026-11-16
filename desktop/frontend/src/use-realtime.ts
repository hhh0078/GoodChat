/** 文件作用说明：把现有客服实时订阅接到原生消息模块，界面不再创建浏览器WebSocket。 */
import { useEffect, useRef, useState } from "react";
import type {
  ConnectionStatus,
  RealtimeListener,
  RealtimeEvent,
  RealtimeFrame,
} from "@/lib/realtime/client";
import { invoke, on, type State } from "./native";
export type RealtimeIdentity = {
  tenantId?: string;
  token?: string;
  actorId: string;
  kind: "agent" | "visitor";
};

/** useRealtime 订阅原生接收结果，页面完成自身数据恢复后才显示已连接。 */
export function useRealtime(
  identity: RealtimeIdentity,
  options: Omit<RealtimeListener, "onState"> & { enabled?: boolean } = {},
) {
  const callbacks = useRef(options);
  callbacks.current = options;
  const [status, setStatus] = useState<ConnectionStatus>("connecting");
  const [detail, setDetail] = useState("");
  useEffect(() => {
    if (options.enabled === false || !identity.actorId) return;
    let disposed = false;
    let epoch = 0;
    let scope = "";
    /** recover 等待调用组件补拉完成，避免连接握手成功就提前标绿。 */
    async function recover(state: State) {
      if (disposed) return;
      scope = state.scope || "";
      const attempt = ++epoch;
      setDetail(state.detail);
      if (state.status !== "connected") {
        setStatus(state.status as ConnectionStatus);
        return;
      }
      setStatus("recovering");
      try {
        await callbacks.current.onRecover?.();
        if (!disposed && attempt === epoch) setStatus("connected");
      } catch {
        if (!disposed && attempt === epoch) setStatus("recovering");
      }
    }
    void invoke<State>("Status")
      .then(recover)
      .catch(() => setStatus("offline"));
    const cleanups = [
      on<State>("desktop:state", (state) => {
        void recover(state);
      }),
      on<{ scope: string; event: RealtimeEvent }>("desktop:event", (value) => {
        if (value.scope === scope) callbacks.current.onEvent?.(value.event);
      }),
      on<RealtimeFrame & { scope: string }>("desktop:frame", (frame) => {
        if (frame.scope === scope) callbacks.current.onFrame?.(frame);
      }),
      on<{ scope: string }>("desktop:sync", (value) => {
        if (value.scope === scope)
          void Promise.resolve()
            .then(() => callbacks.current.onRecover?.())
            .catch(() => {});
      }),
      on<{ scope: string }>("desktop:sent", (value) => {
        if (value.scope === scope)
          void Promise.resolve()
            .then(() => callbacks.current.onRecover?.())
            .catch(() => {});
      }),
    ];
    return () => {
      disposed = true;
      epoch++;
      cleanups.forEach((cleanup) => cleanup());
    };
  }, [identity.tenantId, identity.actorId, options.enabled]);
  return {
    connected: status === "connected",
    status,
    diagnostics: [
      { at: Date.now(), phase: status, detail, attempt: 0, delayMs: undefined },
    ],
    /** send 仅转发输入状态帧，正文继续经过HTTP持久发送队列。 */
    send(frame: Record<string, unknown>) {
      void invoke(
        "SendFrame",
        String(frame.conversation_id || ""),
        !!frame.is_typing,
      );
      return true;
    },
    /** reconnect 请求原生层重新连接，不依赖页面定时器。 */
    reconnect() {
      void invoke("Reconnect");
    },
  };
}
