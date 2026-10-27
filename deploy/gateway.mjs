/** 文件作用说明：提供应用统一入口，覆盖来源IP头并代理HTTP及WebSocket，后端只信任本机认证网关。 */
import http from "node:http";

const port = Number(process.env.PORT || 3000);
const secret = process.env.SUPPORT_GATEWAY_SECRET || process.env.SUPPORT_AUTH_SECRET;
if (!secret) throw new Error("缺少应用网关密钥");

/** 从固定路由选择本机服务，不接受用户控制的目标URL。 */
function target(path) { return path.startsWith("/api/") || path.startsWith("/ws/") || path === "/healthz" ? 18080 : 3001; }

/** 丢弃访客伪造的转发头，并写入仅后端可验证的真实连接来源。 */
function headers(request, destination) {
  const value = { ...request.headers, host: `127.0.0.1:${destination}` };
  for (const key of Object.keys(value)) if (key.startsWith("x-forwarded-") || key.startsWith("x-goodchat-") || key === "forwarded" || key === "x-real-ip") delete value[key];
  value["x-goodchat-client-ip"] = request.socket.remoteAddress || "";
  value["x-goodchat-gateway-token"] = secret;
  return value;
}

const server = http.createServer((request, response) => {
  const destination = target(request.url || "/");
  const upstream = http.request({ hostname: "127.0.0.1", port: destination, path: request.url, method: request.method, headers: headers(request, destination) }, (incoming) => {
    response.writeHead(incoming.statusCode || 502, incoming.headers);
    incoming.pipe(response);
  });
  upstream.on("error", () => { if (!response.headersSent) response.writeHead(502, { "content-type": "application/json" }); response.end(JSON.stringify({ code: "service_starting", message: "服务正在准备，请稍后重试" })); });
  request.on("aborted", () => upstream.destroy());
  request.pipe(upstream);
});

server.on("upgrade", (request, socket, head) => {
  if (!(request.url || "").startsWith("/ws/")) { socket.destroy(); return; }
  const upstream = http.request({ hostname: "127.0.0.1", port: 18080, path: request.url, method: "GET", headers: headers(request, 18080) });
  upstream.on("upgrade", (response, connection, upstreamHead) => {
    socket.write(`HTTP/1.1 ${response.statusCode} Switching Protocols\r\n` + Object.entries(response.headers).map(([key, value]) => `${key}: ${value}\r\n`).join("") + "\r\n");
    if (upstreamHead.length) socket.write(upstreamHead);
    if (head.length) connection.write(head);
    socket.pipe(connection); connection.pipe(socket);
    connection.on("error", () => socket.destroy()); socket.on("error", () => connection.destroy());
  });
  upstream.on("response", (response) => { socket.write(`HTTP/1.1 ${response.statusCode || 502} Rejected\r\nConnection: close\r\n\r\n`); response.pipe(socket); });
  upstream.on("error", () => socket.destroy()); upstream.end();
});
server.listen(port, "0.0.0.0");
process.on("SIGTERM", () => server.close(() => process.exit(0)));
