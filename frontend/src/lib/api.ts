/** 文件作用说明：统一处理同源 API、租户上下文、错误反馈和幂等请求。 */
export class ApiError extends Error {
  status: number;
  code: string;
  requestId?: string;
  messageKey?: string;
  messageParams?: Record<string, string | number>;
  /** constructor 保存可展示错误、HTTP 状态和追踪编号。 */
  constructor(
    message: string,
    status: number,
    code: string,
    requestId?: string,
    messageKey?: string,
    messageParams?: Record<string, string | number>,
  ) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.requestId = requestId;
    this.messageKey = messageKey;
    this.messageParams = messageParams;
  }
}
/** api 请求 Go 服务，成功解包 data，失败保留服务端说明与追踪编号。 */
export async function api<T>(
  path: string,
  options: RequestInit & {
    tenantId?: string;
    bodyJSON?: unknown;
    idempotent?: boolean;
  } = {},
): Promise<T> {
  const { tenantId, bodyJSON, idempotent, ...init } = options;
  const headers = new Headers({
    ...preferenceHeaders(),
    ...Object.fromEntries(new Headers(init.headers)),
  });
  if (tenantId) headers.set("X-Tenant-ID", tenantId);
  if (bodyJSON !== undefined) headers.set("Content-Type", "application/json");
  if (idempotent) headers.set("Idempotency-Key", crypto.randomUUID());
  // 主站CDN不支持PATCH；POST覆盖仍由服务端原PATCH路由校验身份与字段。
  const method = (init.method || "GET").toUpperCase();
  if (method === "PATCH") headers.set("X-HTTP-Method-Override", "PATCH");
  let response: Response;
  const timeout = AbortSignal.timeout(30000);
  const signal = init.signal ? AbortSignal.any([init.signal, timeout]) : timeout;
  try {
    response = await fetch(`/api/v1${path}`, {
      ...init,
      method: method === "PATCH" ? "POST" : method,
      signal,
      credentials: "same-origin",
      cache: "no-store",
      headers,
      body: bodyJSON === undefined ? init.body : JSON.stringify(bodyJSON),
    });
  } catch (error) {
    if (init.signal?.aborted) throw error;
    throw new ApiError(
      timeout.aborted
        ? tr("请求超过等待时间，结果可能尚未确认。请稍后重试或刷新记录。")
        : tr("暂时连接不上服务，请检查网络后重试。"),
      0,
      timeout.aborted ? "REQUEST_TIMEOUT" : "NETWORK_ERROR",
    );
  }
  const payload = await response.json().catch(() => null);
  if (!response.ok)
    throw new ApiError(
      payload?.message || tr("请求未完成（{p0}），请稍后重试。", { p0: String(response.status) }),
      response.status,
      payload?.code || "REQUEST_FAILED",
      payload?.request_id,
      payload?.message_key,
      payload?.message_params,
    );
  return payload?.data as T;
}
/** errorMessage 输出统一错误说明，方便界面给出追踪编号。 */
export function errorMessage(error: unknown): string {
  if (typeof error === "string" && error.trim()) return error;
  if (error instanceof ApiError)
    return error.requestId
      ? tr("{p0}（请求编号 {p1}）", {
          p0: String(localizedError(error)),
          p1: String(error.requestId),
        })
      : localizedError(error);
  return error instanceof Error ? error.message : tr("操作暂时没有完成，请重试。");
}

import { preferenceHeaders, translate as tr } from "../i18n/runtime.ts";

/** localizedError 根据当前语言重现已知系统错误，未知消息使用服务端安全说明。 */
function localizedError(error: ApiError): string {
  if (!error.messageKey) return error.message;
  const value = tr(error.messageKey, error.messageParams);
  return value === error.messageKey ? error.message : value;
}
