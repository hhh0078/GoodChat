/** 文件作用说明：验证桌面切换服务器时相同业务编号的草稿不会串用或静默丢失。 */
import assert from "node:assert/strict";
import test from "node:test";
import { switchDraftServer } from "../../desktop/frontend/src/drafts.ts";

/** memoryStorage 提供隔离的会话存储，不访问用户浏览器数据。 */
function memoryStorage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    clear() {
      values.clear();
    },
    getItem(key) {
      return values.get(key) ?? null;
    },
    key(index) {
      return [...values.keys()][index] ?? null;
    },
    removeItem(key) {
      values.delete(key);
    },
    setItem(key, value) {
      values.set(key, value);
    },
  };
}

test("不同服务器相同业务编号各自保留草稿，切回后恢复原内容", () => {
  const storage = memoryStorage();
  switchDraftServer(storage, "https://a.example");
  storage.setItem("goodchat.draft.same-id", "服务器A草稿");
  switchDraftServer(storage, "https://b.example");
  assert.equal(storage.getItem("goodchat.draft.same-id"), null);
  storage.setItem("goodchat.draft.same-id", "服务器B草稿");
  switchDraftServer(storage, "https://a.example");
  assert.equal(storage.getItem("goodchat.draft.same-id"), "服务器A草稿");
  switchDraftServer(storage, "https://b.example");
  assert.equal(storage.getItem("goodchat.draft.same-id"), "服务器B草稿");
});

test("目标草稿损坏时停止切换并保留当前服务器草稿", () => {
  const storage = memoryStorage();
  switchDraftServer(storage, "https://a.example");
  storage.setItem("goodchat.draft.same-id", "保留正文");
  storage.setItem(
    `goodchat.desktop.savedDrafts:${encodeURIComponent("https://b.example")}`,
    "broken",
  );
  assert.throws(() => switchDraftServer(storage, "https://b.example"));
  assert.equal(storage.getItem("goodchat.draft.same-id"), "保留正文");
  assert.equal(storage.getItem("goodchat.desktop.server"), "https://a.example");
});

test("草稿校验使用界面传入的译文且失败时保留原草稿", () => {
  const storage = memoryStorage();
  switchDraftServer(storage, "https://a.example");
  storage.setItem("goodchat.draft.same-id", "保留正文");
  storage.setItem(`goodchat.desktop.savedDrafts:${encodeURIComponent("https://b.example")}`, "[]");
  assert.throws(
    () => switchDraftServer(storage, "https://b.example", "Saved drafts are invalid."),
    { message: "Saved drafts are invalid." },
  );
  assert.equal(storage.getItem("goodchat.draft.same-id"), "保留正文");
  assert.equal(storage.getItem("goodchat.desktop.server"), "https://a.example");
});
