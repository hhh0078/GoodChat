/** 文件作用说明：验证CSV导出保留筛选与租户授权，拒绝错误/空文件并安全处理中文文件名。 */
import assert from "node:assert/strict";
import test from "node:test";
import { ApiError } from "../src/lib/api.ts";
import { exportFilename, requestCSV } from "../src/lib/csv-download.ts";
test("中文文件名正确解码并去除路径与控制字符", () => {
  assert.equal(
    exportFilename("attachment; filename*=UTF-8''%E4%BC%9A%E8%AF%9D.csv", "export.csv"),
    "会话.csv",
  );
  assert.equal(
    exportFilename('attachment; filename="../../records.csv"', "export.csv"),
    "records.csv",
  );
  assert.equal(exportFilename(null, "export.csv"), "export.csv");
});
test("CSV请求沿用筛选和租户头，成功返回实际文件与行数", async () => {
  const original = globalThis.fetch;
  try {
    globalThis.fetch = async (input, init) => {
      assert.equal(input, "/api/v1/tenant/chat-records/export?status=closed&include_messages=true");
      assert.equal(new Headers(init?.headers).get("X-Tenant-ID"), "tenant-a");
      return new Response("id,content\n1,真实消息\n", {
        headers: {
          "Content-Type": "text/csv; charset=utf-8",
          "X-Export-Row-Count": "1",
          "Content-Disposition": "attachment; filename=messages.csv",
        },
      });
    };
    const result = await requestCSV(
      "/tenant/chat-records/export?status=closed&include_messages=true",
      "tenant-a",
      "fallback.csv",
    );
    assert.equal(result.rows, 1);
    assert.equal(result.filename, "messages.csv");
    assert.match(await result.blob.text(), /真实消息/);
  } finally {
    globalThis.fetch = original;
  }
});
test("导出超限保持真实错误码及追踪编号", async () => {
  const original = globalThis.fetch;
  try {
    globalThis.fetch = async () =>
      new Response(
        JSON.stringify({
          code: "export_limit_exceeded",
          message: "请缩小范围",
          request_id: "request-a",
        }),
        { status: 409, headers: { "Content-Type": "application/json" } },
      );
    await assert.rejects(
      requestCSV("/tenant/chat-records/export", "tenant-a", "export.csv"),
      (error) =>
        error instanceof ApiError &&
        error.status === 409 &&
        error.code === "export_limit_exceeded" &&
        error.requestId === "request-a",
    );
  } finally {
    globalThis.fetch = original;
  }
});
test("空CSV、仅表头零行及HTML成功不能冒充导出成功", async () => {
  const original = globalThis.fetch;
  try {
    for (const response of [
      new Response("", { headers: { "Content-Type": "text/csv" } }),
      new Response("id,content\n", {
        headers: { "Content-Type": "text/csv", "X-Export-Row-Count": "0" },
      }),
      new Response("<html>登录</html>", { headers: { "Content-Type": "text/html" } }),
    ]) {
      globalThis.fetch = async () => response;
      await assert.rejects(
        requestCSV("/tenant/chat-records/export", "tenant-a", "export.csv"),
        (error) =>
          error instanceof ApiError &&
          ["EMPTY_EXPORT", "INVALID_EXPORT_RESPONSE"].includes(error.code),
      );
    }
  } finally {
    globalThis.fetch = original;
  }
});
