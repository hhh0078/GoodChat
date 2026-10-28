/** 文件作用说明：验证访客会话缓存按入口隔离、保留业务参数并且不将缓存编号或内部头像当作身份和输入。 */
import assert from "node:assert/strict";
import test from "node:test";
import { parseVisitorParameters } from "../src/lib/visitor-params.ts";
import {
  mergeVisitorParameters,
  readVisitorSessionCache,
  safeVisitorParameters,
  visitorCacheKey,
  writeVisitorSessionCache,
  type VisitorSessionCache,
} from "../src/lib/visitor-session-cache.ts";

/** MemoryStorage 提供独立缓存容器，不读写真实浏览器或用户资料。 */
class MemoryStorage {
  values = new Map<string, string>();
  /** getItem 返回测试缓存。 */
  getItem(key: string) {
    return this.values.get(key) ?? null;
  }
  /** setItem 保存测试缓存。 */
  setItem(key: string, value: string) {
    this.values.set(key, value);
  }
  /** removeItem 删除测试缓存。 */
  removeItem(key: string) {
    this.values.delete(key);
  }
}

/** fixture 创建明确的虚构恢复资料，凭证不用于真实服务。 */
function fixture(): VisitorSessionCache {
  return {
    version: 1,
    service_space_id: "space-a",
    entry_key: "entry-a",
    visitor_token: "v_" + "synthetic-test-credential".repeat(2),
    conversation_id: "11111111-1111-4111-8111-111111111111",
    contact: {
      display_name: "演示客户",
      phone: "13800000000",
      avatar_url: "/images/avatars/default-01.png",
    },
    parameters: safeVisitorParameters(
      parseVisitorParameters(
        new URLSearchParams({
          姓名: "演示客户",
          extra: '{"是否逾期":"无","数量":0,"开关":false}',
          handoff_code: "one-time-do-not-cache",
        }),
      ),
    ),
    updated_at: "2026-10-29T00:00:00Z",
  };
}

test("缓存会话与资料跨刷新恢复，入口和服务空间各自隔离", () => {
  const storage = new MemoryStorage(),
    value = fixture();
  writeVisitorSessionCache(storage, value);
  const restored = readVisitorSessionCache(storage, "space-a", "entry-a")!;
  assert.equal(restored.conversation_id, value.conversation_id);
  assert.equal(restored.visitor_token, value.visitor_token);
  assert.equal(restored.contact.phone, "13800000000");
  assert.equal(readVisitorSessionCache(storage, "space-a", "entry-b"), null);
  assert.equal(readVisitorSessionCache(storage, "space-b", "entry-a"), null);
  assert.equal(JSON.stringify(restored).includes("one-time-do-not-cache"), false);
});

test("新入链参数覆盖同入口缓存，未提供的0和false保留，内部头像不反填公开头像参数", () => {
  const value = fixture();
  const merged = mergeVisitorParameters(
    value,
    parseVisitorParameters(new URLSearchParams({ 姓名: "新称呼", extra: '{"是否逾期":"有"}' })),
  );
  assert.equal(merged.profile.display_name, "新称呼");
  assert.equal(merged.context["是否逾期"], "有");
  assert.equal(merged.context["数量"], 0);
  assert.equal(merged.context["开关"], false);
  assert.equal(merged.profile.avatar_url, undefined);
});

test("损坏或篡改缓存不提供恢复编号，未知路由字段被剔除", () => {
  const storage = new MemoryStorage(),
    value = fixture();
  storage.setItem(visitorCacheKey("space-a", "entry-a"), "broken-json");
  assert.equal(readVisitorSessionCache(storage, "space-a", "entry-a"), null);
  storage.setItem(
    visitorCacheKey("space-a", "entry-a"),
    JSON.stringify({
      ...value,
      conversation_id: "other/id",
      visitor_token: "not-a-credential",
      parameters: { ...value.parameters, routing: { tenant_id: "other", agent_id: "agent-a" } },
    }),
  );
  const result = readVisitorSessionCache(storage, "space-a", "entry-a")!;
  assert.equal(result.conversation_id, undefined);
  assert.equal(result.visitor_token, undefined);
  assert.deepEqual(result.parameters.routing, { agent_id: "agent-a" });
});

test("缓存不可用显式抛出供页面提示，不吞成恢复成功", () => {
  assert.throws(
    () =>
      readVisitorSessionCache(
        {
          getItem: () => {
            throw new Error("storage denied");
          },
          setItem() {},
          removeItem() {},
        },
        "space-a",
        "entry-a",
      ),
    /denied/,
  );
});
