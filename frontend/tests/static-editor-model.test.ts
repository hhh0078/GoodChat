/** 文件作用说明：验证静态编辑器文件变更、核心文件保护、图片路径和动态聊天样式边界。 */
import assert from "node:assert/strict";
import test from "node:test";
import {
  editorAssetReference,
  editorFileChanges,
  editorImagePath,
  replaceEditorFile,
  type EditorFile,
} from "../src/features/decoration/static-editor-model.ts";
import {
  editorRuleDeclarations,
  editorStyleSelectors,
} from "../src/features/decoration/static-editor-styles.ts";

const source: EditorFile[] = [
  { path: "index.html", content_type: "text/html", size: 4, protected: false, content: "原文" },
  {
    path: "static/goodchat/sdk.js",
    content_type: "text/javascript",
    size: 4,
    protected: true,
    content: "核心",
  },
  {
    path: "static/images/a.png",
    content_type: "image/png",
    size: 1,
    protected: false,
    data_base64: "AA==",
  },
];

test("编辑器保留空文本与图片二进制语义，仅提交变化文件", () => {
  const changed = replaceEditorFile(source, "index.html", "");
  changed.push({
    path: "static/images/b.png",
    content_type: "image/png",
    size: 1,
    protected: false,
    data_base64: "AQ==",
  });
  assert.deepEqual(editorFileChanges(changed, source), [
    { path: "index.html", content: "" },
    { path: "static/images/b.png", data_base64: "AQ==" },
  ]);
  assert.equal(source[0].content, "原文");
  assert.equal(source[2].data_base64, "AA==");
});

test("官方核心文件禁止源码编辑，新增界面脚本保持普通资源", () => {
  assert.throws(() => replaceEditorFile(source, "static/goodchat/sdk.js", "改变"), /只读/);
  const changed = replaceEditorFile(source, "static/custom.js", "/* 页面扩展 */");
  assert.equal(changed.at(-1)?.protected, false);
  assert.equal(changed.at(-1)?.content_type, "text/javascript");
});

test("图片只使用受控类型和随机文件名，子目录入口生成正确的相对资源路径", () => {
  assert.equal(editorImagePath("image/webp", "test-123"), "static/images/upload-test-123.webp");
  assert.throws(() => editorImagePath("image/svg+xml", "test"));
  assert.throws(() => editorImagePath("image/png", "../escape"));
  assert.equal(editorAssetReference("index.html", "static/a.png"), "static/a.png");
  assert.equal(editorAssetReference("pages/chat/index.html", "static/a.png"), "../../static/a.png");
});

test("动态消息样式只识别官方选择器，读取对应块时不混入其他作者CSS", () => {
  const selector = ".gc-avatar";
  const files: EditorFile[] = [
    {
      path: "static/editor-overrides.css",
      content_type: "text/css",
      size: 10,
      protected: false,
      content: `body{color:red}\n/* GoodChat editor ${selector} */\n${selector} {\nwidth: 36px;\nborder-radius: 50%;\n}\n/* End GoodChat editor */\n`,
    },
  ];
  assert.equal(editorRuleDeclarations(files, selector), "width: 36px;\nborder-radius: 50%;");
  assert.equal(editorRuleDeclarations(files, "body"), "");
  assert.equal(editorStyleSelectors.has(".gc-message-row.gc-own .gc-message"), true);
  assert.equal(editorStyleSelectors.has("iframe"), false);
});
