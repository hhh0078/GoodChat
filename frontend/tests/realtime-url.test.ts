/** 文件作用说明：验证统一WSS端点与旧票据协议，防止混合内容和意外凭据跳转。 */
import assert from "node:assert/strict";
import test from "node:test";
import { realtimeSocketURL } from "../src/lib/realtime/url.ts";

test("专用WSS与网页域名分离，旧相对票据仍可连接", () => {
  assert.equal(
    realtimeSocketURL("wss://socket.example/ws/v1?ticket=one", "https://web.example"),
    "wss://socket.example/ws/v1?ticket=one",
  );
  assert.equal(
    realtimeSocketURL("/ws/v1?ticket=one", "https://web.example"),
    "wss://web.example/ws/v1?ticket=one",
  );
  assert.equal(
    realtimeSocketURL("/ws/v1?ticket=one", "http://localhost:13000"),
    "ws://localhost:13000/ws/v1?ticket=one",
  );
});
test("拒绝明文降级、伪造HTTP来源、凭据和非法票据路径", () => {
  for (const value of [
    "ws://socket.example/ws/v1?ticket=one",
    "https://other.example/ws/v1?ticket=one",
    "wss://user:pass@socket.example/ws/v1?ticket=one",
    "wss://socket.example/admin?ticket=one",
    "wss://socket.example/ws/v1",
    "wss://socket.example/ws/v1?ticket=one&ticket=two",
    "wss://socket.example/ws/v1?ticket=one#leak",
    "javascript:alert(1)",
  ])
    assert.equal(realtimeSocketURL(value, "https://web.example"), null, value);
});
