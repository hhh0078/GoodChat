/** 文件作用说明：验证统计日期或租户切换失败不冒用旧缓存，慢请求不重叠且取消后不再轮询。 */
import assert from "node:assert/strict";
import test from "node:test";
import { reportScopeKey, scopedReport, serialPoll } from "../src/lib/reporting-state.ts";

test("日期或租户切换失败时不把前一范围成功数据标成当前数据", () => {
  const a = reportScopeKey("tenant-a", "from=2026-10-27&to=2026-10-27");
  const b = reportScopeKey("tenant-a", "from=2026-10-28&to=2026-10-28");
  const snapshot = { scope: a, data: { online_minutes: 120 } };
  assert.deepEqual(scopedReport(snapshot, a), { online_minutes: 120 });
  assert.notEqual(
    reportScopeKey("tenant-a", "today:2026-10-28"),
    reportScopeKey("tenant-a", "today:2026-10-29"),
  );
  assert.equal(scopedReport(snapshot, b), null);
  assert.equal(
    scopedReport(snapshot, reportScopeKey("tenant-b", "from=2026-10-27&to=2026-10-27")),
    null,
  );
  assert.deepEqual(scopedReport(snapshot, a), { online_minutes: 120 });
});

test("慢统计请求期间多个轮询触发只启动一次，取消后拒绝新请求", async () => {
  const controller = new AbortController();
  let calls = 0,
    release = () => {};
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  const poll = serialPoll(controller.signal, async () => {
    calls++;
    await pending;
  });
  const first = poll();
  await Promise.all([poll(), poll()]);
  assert.equal(calls, 1);
  release();
  await first;
  await poll();
  assert.equal(calls, 2);
  controller.abort();
  await poll();
  assert.equal(calls, 2);
});

test("失败请求释放轮询占用，后续仍可恢复读取", async () => {
  const controller = new AbortController();
  let calls = 0;
  const poll = serialPoll(controller.signal, async () => {
    if (++calls === 1) throw new Error("暂时不可用");
  });
  await assert.rejects(poll(), /暂时不可用/);
  await poll();
  assert.equal(calls, 2);
});
