/** 文件作用说明：验证自然日期、稳定去重、并发触底与刷新接回活跃老会话，防止游标和身份切换漏项。 */
import assert from "node:assert/strict";
import test from "node:test";
import {
  createConversationPager,
  mergeConversationPages,
  recentConversationPeriod,
  type ConversationListSnapshot,
  type ConversationPage,
} from "../src/lib/conversation-list.ts";
import type { Conversation } from "../src/lib/types.ts";

/** item 创建仅供分页算法测试的会话身份与真实排序字段。 */
function item(id: string, at: string): Conversation {
  return { id, status: "active", contact_id: id, last_message_at: at };
}
/** page 封装一个游标响应，末页不携带下一游标。 */
function page(items: Conversation[], cursor = ""): ConversationPage {
  return { items, next_cursor: cursor, has_more: !!cursor, timezone: "Asia/Shanghai" };
}
/** harness 记录分页器对外发布的结果和错误，不依赖React或网络模拟层。 */
function harness(fetchPage: (cursor: string) => Promise<ConversationPage>) {
  const snapshots: ConversationListSnapshot[] = [];
  const errors: unknown[] = [];
  const pager = createConversationPager({
    fetchPage,
    onSnapshot: (snapshot) => snapshots.push(snapshot),
    onBusy: () => {},
    onError: (error) => errors.push(error),
    isForbidden: (error) => error === "forbidden",
    isInvalidCursor: (error) => error === "invalid_cursor",
  });
  return { pager, snapshots, errors };
}

test("三天范围按租户自然日，跨年和夏令时不按72小时截断", () => {
  assert.deepEqual(recentConversationPeriod(new Date("2026-12-31T17:00:00Z")), {
    from: "2026-12-30",
    to: "2027-01-01",
  });
  assert.deepEqual(recentConversationPeriod(new Date("2026-03-09T02:30:00Z"), "America/New_York"), {
    from: "2026-03-06",
    to: "2026-03-08",
  });
  assert.deepEqual(recentConversationPeriod(new Date("2026-10-29T16:00:00Z")), {
    from: "2026-10-28",
    to: "2026-10-30",
  });
});

test("跨页重复只保留最新快照，活动倒序和相同时间编号升序一致", () => {
  const old = item("old", "2026-10-20T00:00:00Z");
  const moved = { ...old, last_message_at: "2026-10-29T10:00:00Z", unread_count: 1 };
  const result = mergeConversationPages(
    [old, item("b", "2026-10-29T09:00:00Z")],
    [moved, item("a", "2026-10-29T09:00:00Z")],
  );
  assert.deepEqual(
    result.map((entry) => entry.id),
    ["old", "a", "b"],
  );
  assert.equal(result[0].unread_count, 1);
});

test("同毫秒保留微秒活动顺序，跨时区同一时刻仍按编号排序，异常日期按零处理", () => {
  const result = mergeConversationPages(
    [],
    [
      item("a", "2026-10-29T10:00:00.000001Z"),
      item("c", "2026-10-29T18:00:00.000999+08:00"),
      item("b", "2026-10-29T10:00:00.000999Z"),
      item("invalid", "not-a-date"),
      item("before-epoch", "1969-12-31T23:59:59.999999Z"),
    ],
  );
  assert.deepEqual(
    result.map((entry) => entry.id),
    ["b", "c", "a", "invalid", "before-epoch"],
  );
});

test("重复触底只消费一次游标，刷新逐页重建前缀前保留当前尾页", async () => {
  const a = item("a", "2026-10-29T10:00:00Z"),
    b = item("b", "2026-10-29T09:00:00Z"),
    c = item("c", "2026-10-29T08:00:00Z"),
    d = item("d", "2026-10-29T07:00:00Z");
  const moved = item("three-days-old", "2026-10-29T11:00:00Z");
  const calls: string[] = [];
  let phase = 0,
    release = () => {};
  let blocked = new Promise<void>((resolve) => {
    release = resolve;
  });
  const h = harness(async (cursor) => {
    calls.push(cursor);
    if (phase === 0) {
      if (!cursor) return page([a, b], "older");
      await blocked;
      return page([c, d]);
    }
    if (!cursor) return page([moved, a], "new-older");
    await blocked;
    return page([b, c]);
  });
  await h.pager.refresh();
  const more = h.pager.loadMore();
  void h.pager.loadMore();
  void h.pager.loadMore();
  release();
  await more;
  assert.deepEqual(calls, ["", "older"]);
  assert.deepEqual(
    h.snapshots.at(-1)!.items.map((entry) => entry.id),
    ["a", "b", "c", "d"],
  );
  phase = 1;
  blocked = new Promise<void>((resolve) => {
    release = resolve;
  });
  const refresh = h.pager.refresh();
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(
    h.snapshots.at(-1)!.items.map((entry) => entry.id),
    ["a", "b", "c", "d"],
  );
  release();
  await refresh;
  assert.deepEqual(
    h.snapshots.at(-1)!.items.map((entry) => entry.id),
    ["three-days-old", "a", "b", "c"],
  );
  assert.equal(h.snapshots.at(-1)!.loaded_pages, 2);
  assert.equal(h.snapshots.at(-1)!.has_more, false);
  assert.deepEqual(calls, ["", "older", "", "new-older"]);
});

test("尾页失败保留列表，失效游标首屏重建，授权失效立即清空", async () => {
  let mode = "initial";
  const h = harness(async (cursor) => {
    if (mode === "forbidden") throw "forbidden";
    if (cursor && mode === "network") throw new Error("network");
    if (cursor && mode === "invalid") {
      mode = "recovered";
      throw "invalid_cursor";
    }
    return page([item("a", "2026-10-29T00:00:00Z")], "older");
  });
  await h.pager.refresh();
  mode = "network";
  await h.pager.loadMore();
  assert.equal(h.snapshots.at(-1)!.items.length, 1);
  mode = "invalid";
  await h.pager.loadMore();
  assert.equal(h.snapshots.at(-1)!.loaded_pages, 1);
  assert.equal(h.errors.at(-1), null);
  mode = "forbidden";
  await h.pager.refresh();
  assert.equal(h.snapshots.at(-1)!.items.length, 0);
  assert.equal(h.snapshots.at(-1)!.has_more, false);
});

test("查询取消后不发布慢响应，末尾微任务新事件不丢刷新", async () => {
  let release = (_value: ConversationPage) => {};
  const h = harness(
    () =>
      new Promise((resolve) => {
        release = resolve;
      }),
  );
  const pending = h.pager.refresh();
  h.pager.stop();
  release(page([item("old-tenant", "2026-10-29T00:00:00Z")]));
  await pending;
  assert.equal(h.snapshots.length, 0);
  let calls = 0;
  let pager: ReturnType<typeof createConversationPager>;
  pager = createConversationPager({
    fetchPage: async () => {
      calls++;
      return page([]);
    },
    onSnapshot: () => {
      if (calls === 1)
        queueMicrotask(() => {
          void pager.refresh();
        });
    },
    onBusy: () => {},
    onError: () => {},
    isForbidden: () => false,
    isInvalidCursor: () => false,
  });
  await pager.refresh();
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(calls, 2);
});

test("持续实时刷新时已排队触底在当前多页刷新后有界执行", async () => {
  const calls: string[] = [];
  let release = () => {};
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let storm = false;
  let headBlocked = false;
  let remainingEvents = 6;
  let snapshot: ConversationListSnapshot | undefined;
  let pager: ReturnType<typeof createConversationPager>;
  pager = createConversationPager({
    fetchPage: async (cursor) => {
      calls.push(cursor);
      if (storm && !headBlocked && !cursor) {
        headBlocked = true;
        await gate;
      }
      if (!cursor) return page([item("a", "2026-10-29T10:00:00Z")], "tail-one");
      if (cursor === "tail-one") return page([item("b", "2026-10-29T09:00:00Z")], "tail-two");
      return page([item("c", "2026-10-29T08:00:00Z")]);
    },
    onSnapshot: (next) => {
      snapshot = next;
      // 每次完整刷新又收到消息，模拟高并发下refreshPending持续被重新点亮。
      if (storm && remainingEvents-- > 0) void pager.refresh();
    },
    onBusy: () => {},
    onError: () => {},
    isForbidden: () => false,
    isInvalidCursor: () => false,
  });
  await pager.refresh();
  await pager.loadMore();
  assert.equal(snapshot!.loaded_pages, 2);
  storm = true;
  const refresh = pager.refresh();
  void pager.refresh();
  void pager.loadMore();
  release();
  await refresh;
  assert.deepEqual(calls.slice(2, 5), ["", "tail-one", "tail-two"]);
  assert.equal(snapshot!.loaded_pages, 3);
  assert.deepEqual(
    snapshot!.items.map((entry) => entry.id),
    ["a", "b", "c"],
  );
});
