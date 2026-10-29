/** 文件作用说明：以真实消息合并与补拉调度模块复现延迟HTTP响应、撤回和同步末尾微任务的时序边界，不代替真实浏览器冻结验收。 */
import assert from "node:assert/strict";
import test from "node:test";
import { mergeMessages, withdrawMessages } from "../src/lib/chat-state.ts";
import { createSyncPump } from "../src/lib/realtime/sync-pump.ts";

/** deferred 精确控制网络请求返回的微任务时序。 */
function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

test("已处理撤回后，延迟POST或历史响应不能恢复旧正文和附件", () => {
  const persisted = {
    id: "server-message",
    seq: "9007199254740994",
    content: "应被撤回的正文",
    attachment: { id: "private-attachment" },
    attachment_id: "private-attachment",
    options: [{ id: "option", label: "不应恢复", value: "旧选项" }],
  };
  const withdrawn = withdrawMessages([persisted], persisted.id, "2026-10-29T01:00:00Z");
  const receivedLate = mergeMessages(withdrawn, [persisted]);
  assert.equal(receivedLate.length, 1);
  assert.equal(receivedLate[0].content, "");
  assert.equal(receivedLate[0].attachment, undefined);
  assert.equal(receivedLate[0].attachment_id, undefined);
  assert.deepEqual(receivedLate[0].options, []);
  assert.equal(receivedLate[0].withdrawn_at, "2026-10-29T01:00:00Z");
});

test("原消息未加载时收到的撤回也约束后续旧分页和新到引用", () => {
  const receivedLate = mergeMessages(
    [],
    [
      { id: "old", seq: "2", content: "未加载旧正文" },
      {
        id: "reply",
        seq: "14",
        content: "正常回复保留",
        reply: { id: "old", content: "未加载旧正文", withdrawn: false },
      },
    ],
    new Map([["old", "2026-10-29T01:00:00Z"]]),
  );
  assert.equal(receivedLate[0].content, "");
  assert.equal(receivedLate[1].content, "正常回复保留");
  assert.deepEqual(receivedLate[1].reply, { id: "old", content: "", withdrawn: true });
});

test("普通客服回复及未撤回更新仍正常合并，不受无关撤回影响", () => {
  const result = mergeMessages(
    [{ id: "agent", seq: "15", content: "原回复", sender_name: "原姓名" }],
    [
      { id: "agent", seq: "15", content: "正常正文", sender_name: "客服姓名" },
      { id: "new", seq: "16", content: "新回复", sender_name: "客服姓名" },
    ],
    new Map([["unrelated", "2026-10-29T01:00:00Z"]]),
  );
  assert.equal(result.length, 2);
  assert.equal(result[0].content, "正常正文");
  assert.equal(result[0].sender_name, "客服姓名");
  assert.equal(result[1].content, "新回复");
});

test("已处理引用撤回后，旧分页回复不能重新带回引用正文", () => {
  const staleReply = {
    id: "reply",
    seq: "12",
    content: "回复保留",
    reply: { id: "original", content: "已撤回引用", withdrawn: false },
  };
  const withdrawn = withdrawMessages([staleReply], "original", "2026-10-29T01:00:00Z");
  const receivedLate = mergeMessages(withdrawn, [staleReply]);
  assert.equal(receivedLate[0].content, "回复保留");
  assert.deepEqual(receivedLate[0].reply, { id: "original", content: "", withdrawn: true });
});

test("同步末尾清理前到达的微任务唤醒也必须完成后才兑现等待者", async () => {
  const response = deferred();
  const displayed: string[] = [];
  const persisted = ["initial"];
  let calls = 0;
  let runResponse!: Promise<void>;
  const pump = createSyncPump(() => {
    calls++;
    const batch = persisted.slice(displayed.length);
    runResponse = (calls === 1 ? response.promise : Promise.resolve()).then(() => {
      displayed.push(...batch);
    });
    return runResponse;
  });
  const first = pump.trigger();
  let last = Promise.resolve();
  // run内部await先注册。此then恰在外层finally清理前触发，模拟网络完成同时到达的新事件。
  runResponse.then(() => {
    persisted.push("arrived-at-drain-boundary");
    last = pump.trigger();
  });
  response.resolve();
  await first;
  await last;
  assert.deepEqual(displayed, persisted);
  assert.equal(calls, 2);
  pump.stop();
});

test("同步函数同步抛错后清理运行态，下次触发仍能恢复", async () => {
  let calls = 0;
  const pump = createSyncPump(() => {
    if (++calls === 1) throw new Error("同步网络错误");
    return Promise.resolve();
  });
  await assert.rejects(pump.trigger(), /同步网络错误/);
  await pump.trigger();
  assert.equal(calls, 2);
  pump.stop();
});

test("停止并中止进行中请求后，等待者收到中止且不会再启动同步", async () => {
  const controller = new AbortController();
  let calls = 0;
  const pump = createSyncPump(() => {
    calls++;
    return new Promise<void>((_, reject) => {
      controller.signal.addEventListener("abort", () => reject(controller.signal.reason));
    });
  });
  const first = pump.trigger();
  const joined = pump.trigger();
  assert.equal(first, joined);
  pump.stop();
  controller.abort();
  await assert.rejects(first, { name: "AbortError" });
  await pump.trigger();
  assert.equal(calls, 1);
});
