/** 文件作用说明：验证链接解除模板绑定和切换更新方式时不会残留旧版本。 */
import assert from "node:assert/strict";
import test from "node:test";
import { linkTemplateBinding } from "../src/lib/link-template-binding.ts";

test("清空模板同时清除旧版本，恢复默认用户端", () => {
  assert.deepEqual(
    linkTemplateBinding({
      page_id: undefined,
      binding_mode: "PIN_RELEASE",
      pinned_release_id: "old",
    }),
    {
      page_id: null,
      binding_mode: "FOLLOW_PAGE",
      pinned_release_id: null,
    },
  );
});

test("跟随发布时清除旧固定版本，固定版本时保留所选编号", () => {
  assert.equal(
    linkTemplateBinding({ page_id: "page", binding_mode: "FOLLOW_PAGE", pinned_release_id: "old" })
      .pinned_release_id,
    null,
  );
  assert.deepEqual(
    linkTemplateBinding({
      page_id: "page",
      binding_mode: "PIN_RELEASE",
      pinned_release_id: "release",
    }),
    {
      page_id: "page",
      binding_mode: "PIN_RELEASE",
      pinned_release_id: "release",
    },
  );
});
