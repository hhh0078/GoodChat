/** 文件作用说明：隔离验证生产网关的偏好请求头、安全来源头及装修凭据边界，不启动监听端口。 */
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { timingSafeEqual } from "node:crypto";
import { isIP } from "node:net";
import test from "node:test";

const source = readFileSync(new URL("./gateway.mjs", import.meta.url), "utf8").replace(/^import .+;$/gm, "");

/** forward 在隔离环境执行实际网关代码，只返回目标请求头，不创建服务器或读取真实密钥。 */
function forward(headers, destination = 18080, edgeSecret = "") {
  const server = { on() {}, listen() {} };
  return runInNewContext(source + "\nheaders(input, destination)", {
    http: { createServer: () => server },
    timingSafeEqual,
    isIP,
    Buffer,
    URL,
    input: { headers, socket: { remoteAddress: "127.0.0.1" } },
    destination,
    process: {
      env: { SUPPORT_GATEWAY_SECRET: "unit-test-gateway-secret", SUPPORT_EDGE_SECRET: edgeSecret, SUPPORT_CODE_ORIGIN: "https://goodchat.invalid" },
      on() {},
    },
  });
}

test("语言时区字段可达后端，伪造身份和来源转发头继续被清除", () => {
  const result = forward({
    "x-goodchat-locale": "de",
    "x-goodchat-timezone": "Europe/Berlin",
    "x-goodchat-client-ip": "203.0.113.5",
    "x-goodchat-gateway-token": "forged",
    "x-goodchat-edge-token": "forged",
    "x-forwarded-for": "203.0.113.6",
    forwarded: "forged",
    "x-real-ip": "203.0.113.7",
    "x-tenant-id": "original-tenant",
  });
  assert.equal(result["x-goodchat-locale"], "de");
  assert.equal(result["x-goodchat-timezone"], "Europe/Berlin");
  assert.equal(result["x-goodchat-client-ip"], "127.0.0.1");
  assert.equal(result["x-goodchat-gateway-token"], "unit-test-gateway-secret");
  assert.equal(result["x-goodchat-edge-token"], undefined);
  for (const key of ["x-forwarded-for", "forwarded", "x-real-ip"]) assert.equal(result[key], undefined);
  assert.equal(result["x-tenant-id"], "original-tenant");
});

test("可信边缘的真实来源仍可传递，装修服务继续拒收主站凭据", () => {
  const edgeSecret = "x".repeat(32);
  const result = forward({ "x-goodchat-edge-token": edgeSecret, "x-real-ip": "203.0.113.10", "x-goodchat-locale": "ja", "x-goodchat-timezone": "Asia/Tokyo", cookie: "private-session", authorization: "private-token" }, 18081, edgeSecret);
  assert.equal(result["x-goodchat-client-ip"], "203.0.113.10");
  assert.equal(result["x-goodchat-locale"], "ja");
  assert.equal(result["x-goodchat-timezone"], "Asia/Tokyo");
  assert.equal(result.cookie, undefined);
  assert.equal(result.authorization, undefined);
});
