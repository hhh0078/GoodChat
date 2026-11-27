/** 文件作用说明：验证访客会话缓存按入口隔离、保留业务参数并且不将缓存编号或内部头像当作身份和输入。 */
import assert from "node:assert/strict";
import test from "node:test";
import { parseVisitorParameters } from "../src/lib/visitor-params.ts";
import {
  mergeVisitorParameters,
  readVisitorSessionCache,
  recoverVisitorSessionCache,
  forgetVisitorSessionCache,
  safeVisitorParameters,
  visitorCacheKey,
  writeVisitorSessionCache,
  type VisitorSessionCache,
} from "../src/lib/visitor-session-cache.ts";

/** MemoryStorage 提供独立缓存容器，不读写真实浏览器或用户资料。 */
class MemoryStorage {
  values = new Map<string, string>();
  /** length 返回测试存储中实际键数，保持与浏览器Storage枚举一致。 */
  get length() {
    return this.values.size;
  }
  /** key 返回指定顺序的测试键，不读取真实同源缓存。 */
  key(index: number) {
    return [...this.values.keys()][index] ?? null;
  }
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

/** 验证租户归并改变默认空间后，恢复同入口原凭证和会话，并保留服务端重新验证流程所需资料。 */
test("同入口更换默认空间可恢复原凭证和会话，新缓存写入后清理旧键", () => {
  const storage = new MemoryStorage(),
    value = fixture();
  writeVisitorSessionCache(storage, value);
  const restored = recoverVisitorSessionCache(storage, "merged-space", "entry-a")!;
  assert.equal(restored.service_space_id, "merged-space");
  assert.equal(restored.entry_key, "entry-a");
  assert.equal(restored.visitor_token, value.visitor_token);
  assert.equal(restored.conversation_id, value.conversation_id);
  assert.deepEqual(restored.parameters, value.parameters);
  assert.equal(storage.getItem(visitorCacheKey("merged-space", "entry-a")), null);
  assert.ok(storage.getItem(visitorCacheKey("space-a", "entry-a")));
  writeVisitorSessionCache(storage, restored);
  assert.equal(storage.getItem(visitorCacheKey("space-a", "entry-a")), null);
  assert.equal(
    readVisitorSessionCache(storage, "merged-space", "entry-a")?.visitor_token,
    value.visitor_token,
  );
});

/** 验证恢复与清理同时校验真实入口字段，后缀相似的其他入口也不能混用。 */
test("跨入口和伪造键不恢复也不清理其他访客资料", () => {
  const storage = new MemoryStorage(),
    value = fixture(),
    other = { ...fixture(), entry_key: "another.entry-a" };
  writeVisitorSessionCache(storage, other);
  assert.equal(recoverVisitorSessionCache(storage, "merged-space", "entry-a"), null);
  storage.setItem(
    visitorCacheKey("old-space", "entry-a"),
    JSON.stringify({ ...value, service_space_id: "old-space", entry_key: "entry-b" }),
  );
  assert.equal(recoverVisitorSessionCache(storage, "merged-space", "entry-a"), null);
  writeVisitorSessionCache(storage, value);
  const restored = recoverVisitorSessionCache(storage, "merged-space", "entry-a")!;
  writeVisitorSessionCache(storage, restored);
  assert.equal(
    readVisitorSessionCache(storage, "space-a", "another.entry-a")?.entry_key,
    other.entry_key,
  );
  forgetVisitorSessionCache(storage, "merged-space", "entry-a");
  assert.equal(recoverVisitorSessionCache(storage, "merged-space", "entry-a"), null);
  assert.ok(readVisitorSessionCache(storage, "space-a", "another.entry-a"));
});

/** 验证当前空间优先与凭证失效清理，不从旧空间反复恢复已经拒绝的身份。 */
test("优先当前键，旧凭证失效时清理同入口的所有空间候选", () => {
  const storage = new MemoryStorage(),
    old = fixture(),
    current = {
      ...fixture(),
      service_space_id: "merged-space",
      visitor_token: "v_" + "new-synthetic-credential".repeat(2),
    };
  storage.setItem(visitorCacheKey(old.service_space_id, old.entry_key), JSON.stringify(old));
  storage.setItem(
    visitorCacheKey(current.service_space_id, current.entry_key),
    JSON.stringify(current),
  );
  assert.equal(
    recoverVisitorSessionCache(storage, "merged-space", "entry-a")?.visitor_token,
    current.visitor_token,
  );
  forgetVisitorSessionCache(storage, "merged-space", "entry-a");
  assert.equal(recoverVisitorSessionCache(storage, "merged-space", "entry-a"), null);
  assert.equal(storage.length, 0);
});

/** 验证旧空间中损坏凭证不充当恢复身份，多个合法历史键按最后保存时间选择。 */
test("旧空间只恢复有形态合法凭证的最近一次同入口缓存", () => {
  const storage = new MemoryStorage(),
    old = fixture(),
    recent = { ...fixture(), service_space_id: "space-recent", updated_at: "2026-11-27T00:00:00Z" };
  for (const value of [old, recent])
    storage.setItem(
      visitorCacheKey(value.service_space_id, value.entry_key),
      JSON.stringify(value),
    );
  assert.equal(
    recoverVisitorSessionCache(storage, "merged-space", "entry-a")?.updated_at,
    recent.updated_at,
  );
  storage.values.clear();
  storage.setItem(
    visitorCacheKey(old.service_space_id, old.entry_key),
    JSON.stringify({ ...old, visitor_token: "invalid" }),
  );
  assert.equal(recoverVisitorSessionCache(storage, "merged-space", "entry-a"), null);
});

/** 验证新空间未确认的无凭证占位不能遮住同入口旧空间仍可验证的原身份。 */
test("新键尚无凭证时仍可恢复旧空间的同入口身份", () => {
  const storage = new MemoryStorage(),
    value = fixture();
  storage.setItem(visitorCacheKey(value.service_space_id, value.entry_key), JSON.stringify(value));
  storage.setItem(
    visitorCacheKey("merged-space", "entry-a"),
    JSON.stringify({ ...value, service_space_id: "merged-space", visitor_token: undefined }),
  );
  assert.equal(
    recoverVisitorSessionCache(storage, "merged-space", "entry-a")?.visitor_token,
    value.visitor_token,
  );
});
