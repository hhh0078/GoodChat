/** 文件作用说明：模拟票据、半开连接和补拉并发故障，验证实时状态恢复及进行中唤醒不会丢失。 */
import assert from "node:assert/strict";
import test from "node:test";
import { RealtimeClient, type SocketLike } from "../src/lib/realtime/client.ts";
import { createSyncPump } from "../src/lib/realtime/sync-pump.ts";
/** FakeSocket 控制连接生命周期回调，模拟浏览器连接故障。 */
class FakeSocket implements SocketLike {
  readyState = 0;
  onopen: SocketLike["onopen"] = null;
  onmessage: SocketLike["onmessage"] = null;
  onclose: SocketLike["onclose"] = null;
  onerror: SocketLike["onerror"] = null;
  sent: string[] = [];
  /** open 模拟握手完成，尚未收到ready。 */
  open() {
    this.readyState = 1;
    this.onopen?.({} as Event);
  }
  /** frame 模拟服务器控制帧。 */
  frame(type: string, data?: Record<string, unknown>) {
    this.onmessage?.({ data: JSON.stringify({ type, data }) } as MessageEvent);
  }
  /** send 收集发送帧。 */
  send(data: string) {
    this.sent.push(data);
  }
  /** close 释放连接。 */
  close() {
    this.readyState = 3;
  }
}
/** settle 等待异步票据和补拉完成。 */
async function settle() {
  await new Promise<void>((resolve) => setImmediate(resolve));
}
/** deferred 控制请求完成时机以注入并发唤醒。 */
function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

test("握手和ready不能提前标绿，持久数据恢复完成才成功", async () => {
  const sockets: FakeSocket[] = [];
  const recovery = deferred();
  const client = new RealtimeClient({
    ticket: async () => "safe",
    socket: () => {
      const socket = new FakeSocket();
      sockets.push(socket);
      return socket;
    },
  });
  client.subscribe({ onRecover: () => recovery.promise });
  client.start();
  await settle();
  sockets[0].open();
  assert.equal(client.status, "recovering");
  sockets[0].frame("ready", {
    connection_id: "bfc2f55f-a336-4f25-8321-c6bcbec90100",
    request_id: "0123456789abcdef01234567",
    ticket: "must-not-appear",
  });
  assert.equal(client.status, "recovering");
  assert.deepEqual(JSON.parse(sockets[0].sent[0]), { type: "heartbeat" });
  recovery.resolve();
  await settle();
  assert.equal(client.status, "connected");
  assert.equal(client.diagnostics.at(-1)?.connectionId, "bfc2f55f-a336-4f25-8321-c6bcbec90100");
  assert.equal(client.diagnostics.at(-1)?.requestId, "0123456789abcdef01234567");
  assert.equal(JSON.stringify(client.diagnostics).includes("must-not-appear"), false);
  client.stop();
});

test("半开超时换新票据，旧回调不能覆盖新连接，身份失效停止重连", async () => {
  let now = 0;
  let tickets = 0;
  const sockets: FakeSocket[] = [];
  const timers: (() => void)[] = [];
  const client = new RealtimeClient({
    ticket: async () => {
      tickets++;
      return "ticket-private";
    },
    socket: () => {
      const socket = new FakeSocket();
      sockets.push(socket);
      return socket;
    },
    now: () => now,
    random: () => 0.5,
    timer: (callback, delay) => {
      assert.equal(delay, 1000);
      timers.push(callback);
      return 1 as unknown as ReturnType<typeof setTimeout>;
    },
    clearTimer: () => {},
  });
  client.start();
  await settle();
  sockets[0].open();
  sockets[0].frame("ready");
  await settle();
  const oldClose = sockets[0].onclose;
  now = 46000;
  client.tick();
  assert.equal(client.status, "reconnecting");
  assert.equal(sockets[0].readyState, 3);
  assert.equal(timers.length, 1);
  timers.shift()!();
  await settle();
  assert.equal(tickets, 2);
  sockets[1].open();
  sockets[1].frame("ready");
  await settle();
  oldClose?.({ code: 1006 } as CloseEvent);
  assert.equal(client.status, "connected");
  sockets[1].onclose?.({ code: 1008 } as CloseEvent);
  assert.equal(client.status, "denied");
  client.resume();
  client.tick();
  await settle();
  assert.equal(tickets, 2);
  assert.ok(client.diagnostics.every((item) => !JSON.stringify(item).includes("ticket-private")));
  client.stop();
});

test("断网立即离线，长时间后台恢复立即连接并补拉", async () => {
  let now = 1;
  let recoveries = 0;
  const sockets: FakeSocket[] = [];
  const client = new RealtimeClient({
    ticket: async () => "safe",
    socket: () => {
      const socket = new FakeSocket();
      sockets.push(socket);
      return socket;
    },
    now: () => now,
  });
  client.subscribe({
    onRecover: async () => {
      recoveries++;
    },
  });
  client.start();
  await settle();
  sockets[0].open();
  sockets[0].frame("ready");
  await settle();
  client.setOnline(false);
  assert.equal(client.status, "offline");
  client.setOnline(true);
  await settle();
  sockets[1].open();
  sockets[1].frame("ready");
  await settle();
  now += 3600000;
  client.resume();
  await settle();
  assert.equal(sockets.length, 3);
  assert.equal(client.status, "connecting");
  sockets[2].open();
  sockets[2].frame("ready");
  await settle();
  assert.equal(recoveries, 3);
  assert.equal(client.status, "connected");
  client.stop();
});

test("补拉中到达的提示会再次扫描，不并发也不丢最后唤醒", async () => {
  const first = deferred();
  let calls = 0;
  let concurrent = 0;
  let maximum = 0;
  const persisted = ["first"];
  const displayed: string[] = [];
  const pump = createSyncPump(async () => {
    calls++;
    concurrent++;
    maximum = Math.max(maximum, concurrent);
    const snapshot = persisted.slice(displayed.length);
    if (calls === 1) await first.promise;
    displayed.push(...snapshot);
    concurrent--;
  });
  const original = pump.trigger();
  persisted.push("arrived-during-fetch");
  assert.equal(original, pump.trigger());
  first.resolve();
  await original;
  assert.equal(maximum, 1);
  assert.equal(calls, 2);
  assert.deepEqual(displayed, persisted);
  pump.stop();
});

test("补拉失败不会锁死后续轮询，下一次触发恢复数据", async () => {
  let calls = 0;
  const pump = createSyncPump(async () => {
    if (++calls === 1) throw new Error("网络中断");
  });
  await assert.rejects(pump.trigger());
  await pump.trigger();
  assert.equal(calls, 2);
  pump.stop();
});

test("503恢复失败保持恢复中，下次检查成功后才标绿", async () => {
  let attempts = 0;
  const socket = new FakeSocket();
  const client = new RealtimeClient({ ticket: async () => "safe", socket: () => socket });
  client.subscribe({
    onRecover: async () => {
      if (++attempts === 1) throw new Error("503");
    },
  });
  client.start();
  await settle();
  socket.open();
  socket.frame("ready");
  await settle();
  assert.equal(client.status, "recovering");
  assert.equal(attempts, 1);
  client.tick();
  await settle();
  assert.equal(client.status, "connected");
  assert.equal(attempts, 2);
  client.stop();
});
