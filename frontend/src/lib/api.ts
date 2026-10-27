/** 文件作用说明：统一处理同源 API、租户上下文、错误反馈和幂等请求。 */
export class ApiError extends Error {
  status: number;
  code: string;
  requestId?: string;
  /** constructor 保存可展示错误、HTTP 状态和追踪编号。 */
  constructor(message: string, status: number, code: string, requestId?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }
}

/** api 请求 Go 服务，成功解包 data，失败保留服务端说明与追踪编号。 */
export async function api<T>(
  path: string,
  options: RequestInit & { tenantId?: string; bodyJSON?: unknown; idempotent?: boolean } = {},
): Promise<T> {
  const { tenantId, bodyJSON, idempotent, ...init } = options;
  const headers = new Headers(init.headers);
  if (tenantId) headers.set("X-Tenant-ID", tenantId);
  if (bodyJSON !== undefined) headers.set("Content-Type", "application/json");
  if (idempotent) headers.set("Idempotency-Key", crypto.randomUUID());
  let response: Response;
  const timeout = AbortSignal.timeout(30000);
  const signal = init.signal ? AbortSignal.any([init.signal, timeout]) : timeout;
  try {
    response = await fetch(`/api/v1${path}`, {
      ...init,
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
        ? "请求超过等待时间，结果可能尚未确认。请稍后重试或刷新记录。"
        : "暂时连接不上服务，请检查网络后重试。",
      0,
      timeout.aborted ? "REQUEST_TIMEOUT" : "NETWORK_ERROR",
    );
  }
  const payload = await response.json().catch(() => null);
  if (!response.ok)
    throw new ApiError(
      payload?.message || `请求未完成（${response.status}），请稍后重试。`,
      response.status,
      payload?.code || "REQUEST_FAILED",
      payload?.request_id,
    );
  return payload?.data as T;
}

/** errorMessage 输出统一错误说明，方便界面给出追踪编号。 */
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError)
    return error.requestId ? `${error.message}（请求编号 ${error.requestId}）` : error.message;
  return error instanceof Error ? error.message : "操作暂时没有完成，请重试。";
}
