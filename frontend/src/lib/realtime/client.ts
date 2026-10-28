/** 文件作用说明：集中管理实时连接状态、超时、退避和安全诊断，连接事件仅提示业务层补拉持久数据。 */
export type ConnectionStatus =
  "idle" | "connecting" | "recovering" | "connected" | "reconnecting" | "offline" | "denied";
export type RealtimeEvent = {
  id?: string;
  event_type?: string;
  conversation_id?: string;
  payload?: Record<string, unknown>;
};
export type Diagnostic = {
  at: number;
  phase: string;
  detail: string;
  attempt: number;
  delayMs?: number;
  connectionId?: string;
  requestId?: string;
};
export type RealtimeFrame = { type: string; data?: Record<string, unknown> };
export type RealtimeListener = {
  onState?: (status: ConnectionStatus, diagnostics: Diagnostic[]) => void;
  onEvent?: (event: RealtimeEvent) => void;
  onFrame?: (frame: RealtimeFrame) => void;
  onRecover?: () => void | Promise<void>;
};
export interface SocketLike {
  readyState: number;
  onopen: ((event: Event) => void) | null;
  onmessage: ((event: MessageEvent) => void) | null;
  onclose: ((event: CloseEvent) => void) | null;
  onerror: ((event: Event) => void) | null;
  send: (data: string) => void;
  close: () => void;
}
type Options = {
  ticket: (signal: AbortSignal) => Promise<string>;
  socket: (url: string) => SocketLike;
  now?: () => number;
  random?: () => number;
  timer?: (callback: () => void, delay: number) => ReturnType<typeof setTimeout>;
  clearTimer?: (timer: ReturnType<typeof setTimeout>) => void;
};
/** RealtimeClient 每个身份只维护一条连接，任何旧连接回调都不能改变新连接状态。 */
export class RealtimeClient {
  status: ConnectionStatus = "idle";
  diagnostics: Diagnostic[] = [];
  private listeners = new Set<RealtimeListener>();
  private socket: SocketLike | null = null;
  private controller: AbortController | null = null;
  private retry: ReturnType<typeof setTimeout> | undefined;
  private generation = 0;
  private attempt = 0;
  private online = true;
  private stopped = false;
  private ready = false;
  private openedAt = 0;
  private lastReceived = 0;
  private lastSent = 0;
  private recovering = false;
  private recoveryPending = false;
  private connectionId = "";
  private requestId = "";
  private options: Options;
  private now: () => number;
  private random: () => number;
  /** constructor 注入浏览器接口以便用故障连接验证真实状态转换。 */
  constructor(options: Options) {
    this.options = options;
    this.now = options.now || Date.now;
    this.random = options.random || Math.random;
  }
  /** subscribe 注册业务监听并返回清理方法，不在诊断中保存业务正文。 */
  subscribe(listener: RealtimeListener) {
    this.listeners.add(listener);
    listener.onState?.(this.status, this.diagnostics);
    return () => this.listeners.delete(listener);
  }
  /** record 只记录有限的状态和预定义原因，禁止记录票据、网址或聊天正文。 */
  private record(phase: string, detail: string, delayMs?: number) {
    this.diagnostics = [
      ...this.diagnostics.slice(-39),
      {
        at: this.now(),
        phase,
        detail,
        attempt: this.attempt,
        ...(delayMs === undefined ? {} : { delayMs }),
        ...(this.connectionId ? { connectionId: this.connectionId } : {}),
        ...(this.requestId ? { requestId: this.requestId } : {}),
      },
    ];
    this.listeners.forEach((listener) => listener.onState?.(this.status, this.diagnostics));
  }
  /** transition 向所有订阅者发布统一连接状态。 */
  private transition(status: ConnectionStatus, detail: string) {
    this.status = status;
    this.record(status, detail);
  }
  /** start 在首次订阅后开始申请一次性票据。 */
  start() {
    this.stopped = false;
    void this.connect();
  }
  /** clearRetry 清理待重试任务，避免多事件同时建立连接。 */
  private clearRetry() {
    if (this.retry !== undefined) (this.options.clearTimer || clearTimeout)(this.retry);
    this.retry = undefined;
  }
  /** invalidate 中止旧票据请求并解除旧连接所有回调。 */
  private invalidate() {
    this.generation++;
    this.controller?.abort();
    this.controller = null;
    const previous = this.socket;
    this.socket = null;
    this.ready = false;
    this.recovering = false;
    this.recoveryPending = false;
    this.connectionId = "";
    this.requestId = "";
    if (previous) {
      previous.onopen = previous.onmessage = previous.onclose = previous.onerror = null;
      previous.close();
    }
  }
  /** connect 每次重连重新申请票据，错误只暴露状态码而不包含请求载荷。 */
  private async connect() {
    this.clearRetry();
    if (this.stopped || !this.online || this.status === "denied") return;
    this.invalidate();
    const generation = this.generation;
    this.controller = new AbortController();
    this.openedAt = this.now();
    this.transition(this.attempt ? "reconnecting" : "connecting", "request_ticket");
    try {
      const url = await this.options.ticket(this.controller.signal);
      if (generation !== this.generation || this.stopped) return;
      const socket = this.options.socket(url);
      this.socket = socket;
      socket.onopen = () => {
        if (generation !== this.generation) return;
        this.openedAt = this.lastReceived = this.now();
        this.transition("recovering", "await_ready");
      };
      socket.onmessage = (event) => {
        if (generation !== this.generation) return;
        let frame: RealtimeFrame;
        try {
          frame = JSON.parse(String(event.data));
        } catch {
          this.record("frame", "invalid_json");
          return;
        }
        if (!frame || typeof frame.type !== "string") return;
        this.lastReceived = this.now();
        if (frame.type === "ready") {
          const connectionId = frame.data?.connection_id;
          const requestId = frame.data?.request_id;
          this.connectionId =
            typeof connectionId === "string" && /^[a-f\d-]{36}$/i.test(connectionId)
              ? connectionId
              : "";
          this.requestId =
            typeof requestId === "string" && /^[a-f\d]{16,64}$/i.test(requestId) ? requestId : "";
          this.ready = true;
          this.lastSent = this.now();
          this.send({ type: "heartbeat" });
          void this.recover(generation);
        } else if (frame.type === "event" && frame.data) {
          this.listeners.forEach((listener) => listener.onEvent?.(frame.data as RealtimeEvent));
        }
        this.listeners.forEach((listener) => listener.onFrame?.(frame));
      };
      socket.onclose = (event) => {
        if (generation !== this.generation) return;
        if (event.code === 1008 || event.code === 4401 || event.code === 4403) {
          this.invalidate();
          this.transition("denied", "authorization_changed");
        } else this.schedule(`closed_${event.code}`);
      };
      socket.onerror = () => {
        if (generation === this.generation) this.schedule("socket_error");
      };
    } catch (error) {
      if (generation !== this.generation || this.stopped) return;
      const problem = error as { status?: number; code?: string; requestId?: string };
      if (problem.status === 401 || problem.status === 403) {
        this.invalidate();
        this.transition("denied", `ticket_${problem.status}`);
      } else this.schedule("ticket_failed");
    }
  }
  /** recover 等待各业务补拉完成后才显示已连接，补拉失败由时钟和网络恢复事件重试。 */
  private async recover(generation: number) {
    if (this.recovering) {
      this.recoveryPending = true;
      return;
    }
    this.recovering = true;
    this.transition("recovering", "sync_persistent_events");
    try {
      do {
        this.recoveryPending = false;
        await Promise.all([...this.listeners].map((listener) => listener.onRecover?.()));
      } while (this.recoveryPending && generation === this.generation);
      if (generation !== this.generation || this.stopped) return;
      this.attempt = 0;
      this.transition("connected", "ready_and_synced");
    } catch {
      if (generation === this.generation) this.record("recovering", "sync_retry_pending");
    } finally {
      if (generation === this.generation) this.recovering = false;
    }
  }
  /** schedule 使用有上限的指数退避和随机抖动，避免服务器恢复时集中重连。 */
  private schedule(reason: string) {
    this.invalidate();
    if (this.stopped) return;
    if (!this.online) {
      this.transition("offline", "network_offline");
      return;
    }
    this.clearRetry();
    const delay = Math.round(
      Math.min(30000, 1000 * 2 ** Math.min(this.attempt++, 5)) * (0.75 + this.random() * 0.5),
    );
    this.status = "reconnecting";
    this.record("reconnecting", reason, delay);
    this.retry = (this.options.timer || setTimeout)(() => {
      this.retry = undefined;
      void this.connect();
    }, delay);
  }
  /** tick 以真实时间判断半开和初始化超时，Worker暂停后恢复也不会依赖丢失的定时次数。 */
  tick() {
    if (this.stopped || !this.online || this.status === "denied") return;
    const now = this.now();
    if (this.controller !== null && !this.ready && now - this.openedAt > 30000) {
      this.schedule("ready_timeout");
      return;
    }
    if (!this.ready) return;
    if (now - this.lastReceived > 45000) {
      this.schedule("heartbeat_timeout");
      return;
    }
    if (now - this.lastSent >= 15000) {
      this.lastSent = now;
      this.send({ type: "heartbeat" });
    }
    if (this.status === "recovering" && !this.recovering) void this.recover(this.generation);
  }
  /** send 仅发送当前连接的短期控制帧，持久消息始终使用幂等HTTP接口。 */
  send(frame: Record<string, unknown>) {
    if (this.socket?.readyState !== 1) return false;
    try {
      this.socket.send(JSON.stringify(frame));
      return true;
    } catch {
      this.schedule("send_failed");
      return false;
    }
  }
  /** setOnline 网络失联立即标红，网络恢复立即重新申请票据。 */
  setOnline(online: boolean) {
    this.online = online;
    if (!online) {
      this.clearRetry();
      this.invalidate();
      this.transition("offline", "network_offline");
    } else this.resume();
  }
  /** resume 从后台或休眠恢复时立即核验过期连接并补拉，不改变客服上下班意图。 */
  resume() {
    if (this.stopped || !this.online || this.status === "denied") return;
    if (!this.ready || this.now() - this.lastReceived > 30000) {
      this.record("resume", "check_connection");
      void this.connect();
    } else {
      this.lastSent = this.now();
      this.send({ type: "heartbeat" });
      void this.recover(this.generation);
    }
  }
  /** reconnect 用户主动重试时解除身份失败状态，但不修改服务器的业务状态。 */
  reconnect() {
    this.attempt = 0;
    this.status = "idle";
    this.resume();
  }
  /** stop 最后订阅者离开时释放连接、请求和重试任务。 */
  stop() {
    this.stopped = true;
    this.clearRetry();
    this.invalidate();
    this.transition("idle", "released");
  }
}
