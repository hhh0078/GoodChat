/** 文件作用说明：验证话术引用和富媒体光标插入不会丢失文字、乱序或修改原始内容。 */
import assert from "node:assert/strict";
import test from "node:test";
import { insertKnowledgeReference, insertMediaBlock } from "../src/lib/ai-content.ts";

test("knowledge references replace only the selected text", () => {
  assert.equal(insertKnowledgeReference("安装说明", 2, 2, "k1"), "安装[[script:k1]]说明");
  assert.equal(insertKnowledgeReference("abcdef", 1, 3, "id"), "a[[script:id]]def");
});
test("media inserted at the text cursor preserves surrounding content and order", () => {
  const original = [
    { type: "text" as const, text: "安装前检查" },
    { type: "audio" as const, media_id: "voice" },
  ];
  const result = insertMediaBlock(original, 0, 2, 2, { type: "video", media_id: "video" });
  assert.deepEqual(result, [
    { type: "text", text: "安装" },
    { type: "video", media_id: "video" },
    { type: "text", text: "前检查" },
    { type: "audio", media_id: "voice" },
  ]);
  assert.equal(original[0].text, "安装前检查");
  assert.deepEqual(insertMediaBlock([], -1, 0, 0, { type: "image", media_id: "image" }), [
    { type: "image", media_id: "image" },
  ]);
});
