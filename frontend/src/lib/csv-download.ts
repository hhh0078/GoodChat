/** 文件作用说明：真实请求授权CSV流，检查错误、类型和空文件，并区分生成与浏览器已发起下载。 */
import { ApiError } from "./api.ts";
/** exportFilename 从受控下载响应提取文件名，移除路径和控制字符。 */
export function exportFilename(header: string | null, fallback: string): string {
  let value = "";
  const encoded = header?.match(/filename\*=UTF-8''([^;]+)/i)?.[1];
  if (encoded) {
    try {
      value = decodeURIComponent(encoded.trim());
    } catch {
      value = "";
    }
  }
  if (!value)
    value =
      header?.match(/filename="([^"]+)"/i)?.[1] ||
      header?.match(/filename=([^;]+)/i)?.[1] ||
      fallback;
  return (
    value
      .replace(/[\u0000-\u001f\u007f]/g, "")
      .split(/[\\/]/)
      .at(-1)
      ?.trim() || fallback
  );
}
/** requestCSV 请求当前租户的实际导出，JSON错误和空成功不能作为文件下载。 */
export async function requestCSV(
  path: string,
  tenantId: string,
  fallback: string,
  signal?: AbortSignal,
): Promise<{ blob: Blob; filename: string; rows: number | null }> {
  const timeout = AbortSignal.timeout(60000);
  let response: Response;
  try {
    response = await fetch(`/api/v1${path}`, {
      credentials: "same-origin",
      cache: "no-store",
      headers: { "X-Tenant-ID": tenantId },
      signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
    });
  } catch (problem) {
    if (signal?.aborted) throw problem;
    throw new ApiError(
      timeout.aborted ? "导出等待超时，请缩小筛选范围后重试。" : "导出服务连接失败，请稍后重试。",
      0,
      timeout.aborted ? "EXPORT_TIMEOUT" : "NETWORK_ERROR",
    );
  }
  if (!response.ok) {
    const body = await response.json().catch(() => null);
    throw new ApiError(
      body?.message || "导出没有完成，请重试。",
      response.status,
      body?.code || "EXPORT_FAILED",
      body?.request_id,
    );
  }
  if (!response.headers.get("Content-Type")?.toLowerCase().includes("text/csv"))
    throw new ApiError(
      "导出没有返回有效CSV文件，请联系平台检查。",
      response.status,
      "INVALID_EXPORT_RESPONSE",
    );
  const count = response.headers.get("X-Export-Row-Count");
  const rows = count !== null && /^\d+$/.test(count) ? Number(count) : null;
  const blob = await response.blob();
  if (rows === 0 || blob.size <= 3)
    throw new ApiError("当前筛选没有可导出数据。", 200, "EMPTY_EXPORT");
  return {
    blob,
    filename: exportFilename(response.headers.get("Content-Disposition"), fallback),
    rows,
  };
}
/** startFileDownload 只发起浏览器下载，不把浏览器已保存到磁盘当作可验证事实。 */
export function startFileDownload(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.style.display = "none";
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10000);
}
