/** 文件作用说明：提供应用统一入口，覆盖来源IP头并代理HTTP及WebSocket，后端只信任本机认证网关。 */
import http from "node:http";
import { timingSafeEqual } from "node:crypto";
import { isIP } from "node:net";

const port = Number(process.env.PORT || 3000);
const secret =
  process.env.SUPPORT_GATEWAY_SECRET || process.env.SUPPORT_AUTH_SECRET;
if (!secret) throw new Error("缺少应用网关密钥");

/** 从固定路由选择本机服务，不接受用户控制的目标URL。 */
function target(path) {
  if (
    ["/render/", "/assets/", "/preview-assets/"].some((prefix) =>
      path.startsWith(prefix),
    )
  )
    return 18081;
  return path.startsWith("/api/") ||
    path.startsWith("/ws/") ||
    path === "/healthz"
    ? 18080
    : 3001;
}

/** 只从部署配置允许的域名解析当前站点，不接受访客伪造的转发来源或任意Host。 */
function publicOrigin(request) {
  const fallback = new URL(
    process.env.SUPPORT_PUBLIC_URL ||
      process.env.SUPPORT_CODE_ORIGIN ||
      "http://127.0.0.1:13000",
  ).origin;
  const allowed = [
    fallback,
    ...(process.env.SUPPORT_PUBLIC_ORIGINS || "").split(","),
  ].flatMap((value) => {
    try {
      const url = new URL(value.trim());
      return !url.username &&
        !url.password &&
        ["http:", "https:"].includes(url.protocol) &&
        url.pathname === "/" &&
        !url.search &&
        !url.hash
        ? [url.origin]
        : [];
    } catch {
      return [];
    }
  });
  const host = String(request.headers.host || "").toLowerCase();
  return (
    allowed.find((origin) => new URL(origin).host.toLowerCase() === host) ||
    fallback
  );
}

/** 丢弃访客伪造的转发头，并写入仅后端可验证的真实连接来源。 */
function headers(request, destination) {
  const expected = Buffer.from(process.env.SUPPORT_EDGE_SECRET || "");
  const supplied = Buffer.from(
    String(request.headers["x-goodchat-edge-token"] || ""),
  );
  const trusted =
    expected.length >= 32 &&
    supplied.length === expected.length &&
    timingSafeEqual(expected, supplied);
  const edgeIP = String(request.headers["x-real-ip"] || "");
  const origin = publicOrigin(request);
  const configuredCode =
    process.env.SUPPORT_CODE_ORIGIN || process.env.SUPPORT_PUBLIC_URL;
  const sameCode =
    !configuredCode ||
    new URL(configuredCode).origin ===
      new URL(process.env.SUPPORT_PUBLIC_URL || configuredCode).origin;
  const value = {
    ...request.headers,
    host:
      destination === 18081
        ? sameCode
          ? new URL(origin).host
          : new URL(configuredCode).host
        : `127.0.0.1:${destination}`,
  };
  if (destination === 18081) {
    delete value.cookie;
    delete value.authorization;
  }
  // 语言时区是公开偏好，交由后端验证；其他内部网关头仍全部覆盖或清除。
  for (const key of Object.keys(value))
    if (
      key.startsWith("x-forwarded-") ||
      (key.startsWith("x-goodchat-") &&
        !["x-goodchat-locale", "x-goodchat-timezone"].includes(key)) ||
      key === "forwarded" ||
      key === "x-real-ip"
    )
      delete value[key];
  value["x-goodchat-client-ip"] =
    trusted && isIP(edgeIP) ? edgeIP : request.socket.remoteAddress || "";
  value["x-goodchat-gateway-token"] = secret;
  value["x-goodchat-public-origin"] = origin;
  return value;
}

const server = http.createServer((request, response) => {
  const destination = target(request.url || "/");
  const upstream = http.request(
    {
      hostname: "127.0.0.1",
      port: destination,
      path: request.url,
      method: request.method,
      headers: headers(request, destination),
    },
    (incoming) => {
      response.writeHead(incoming.statusCode || 502, incoming.headers);
      incoming.pipe(response);
    },
  );
  upstream.on("error", () => {
    if (!response.headersSent)
      response.writeHead(502, { "content-type": "application/json" });
    response.end(
      JSON.stringify({
        code: "service_starting",
        message: "服务正在准备，请稍后重试",
      }),
    );
  });
  request.on("aborted", () => upstream.destroy());
  request.pipe(upstream);
});

server.on("upgrade", (request, socket, head) => {
  if (!(request.url || "").startsWith("/ws/")) {
    socket.destroy();
    return;
  }
  const upstream = http.request({
    hostname: "127.0.0.1",
    port: 18080,
    path: request.url,
    method: "GET",
    headers: headers(request, 18080),
  });
  upstream.on("upgrade", (response, connection, upstreamHead) => {
    socket.write(
      `HTTP/1.1 ${response.statusCode} Switching Protocols\r\n` +
        Object.entries(response.headers)
          .map(([key, value]) => `${key}: ${value}\r\n`)
          .join("") +
        "\r\n",
    );
    if (upstreamHead.length) socket.write(upstreamHead);
    if (head.length) connection.write(head);
    socket.pipe(connection);
    connection.pipe(socket);
    connection.on("error", () => socket.destroy());
    socket.on("error", () => connection.destroy());
  });
  upstream.on("response", (response) => {
    socket.write(
      `HTTP/1.1 ${response.statusCode || 502} Rejected\r\nConnection: close\r\n\r\n`,
    );
    response.pipe(socket);
  });
  upstream.on("error", () => socket.destroy());
  upstream.end();
});
server.listen(port, "0.0.0.0");
process.on("SIGTERM", () => server.close(() => process.exit(0)));
