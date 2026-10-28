/** 文件作用说明：验证头像池默认库回退、多选切换和客服固定头像单选，不改写原选择数组。 */
import assert from "node:assert/strict";
import test from "node:test";
import { selectedAvatarReferences, toggleAvatarReference } from "../src/lib/avatar-selection.ts";
const assets = [
  {
    id: "default-01",
    name: "职业头像01",
    category: "professional",
    url: "/images/avatars/default-01.png",
    builtin: true,
  },
  {
    id: "default-21",
    name: "卡通头像01",
    category: "cartoon",
    url: "/images/avatars/default-21.png",
    builtin: true,
  },
  {
    id: "tenant-upload",
    name: "上传头像",
    category: "uploaded",
    url: "/api/v1/avatar-assets/upload-id",
    builtin: false,
  },
];
test("空头像池只展开平台库，显式配置保留租户上传与去重顺序", () => {
  assert.deepEqual(selectedAvatarReferences([], assets), ["default-01", "default-21"]);
  assert.deepEqual(
    selectedAvatarReferences(["tenant-upload", "default-21", "tenant-upload"], assets),
    ["tenant-upload", "default-21"],
  );
});
test("访客池多选可移除与添加，个人头像只保留固定一项且不污染原选择", () => {
  const refs = ["default-01", "default-21"];
  assert.deepEqual(toggleAvatarReference(refs, "default-01", true), ["default-21"]);
  assert.deepEqual(toggleAvatarReference(refs, "tenant-upload", true), [
    "default-01",
    "default-21",
    "tenant-upload",
  ]);
  assert.deepEqual(toggleAvatarReference(refs, "tenant-upload", false), ["tenant-upload"]);
  assert.deepEqual(toggleAvatarReference(["default-01"], "default-01", true), []);
  assert.deepEqual(refs, ["default-01", "default-21"]);
});
