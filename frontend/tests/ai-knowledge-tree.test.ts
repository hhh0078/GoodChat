/** 文件作用说明：回归知识树祖先保留、旧根节点兼容、路径区分和异常循环防护。 */
import assert from "node:assert/strict";
import test from "node:test";
import { buildKnowledgeTree, knowledgeFolderPath } from "../src/features/ai/knowledge-tree-data.ts";
import type { AIContent } from "../src/features/ai/types.ts";
const folders = [
  { id: "root", name: "产品", parent_id: null, version: 1 },
  { id: "child", name: "安装", parent_id: "root", version: 1 },
];
const docs = [
  { id: "a", title: "安装指南", folder_id: "child" },
  { id: "b", title: "旧知识", folder_id: null },
] as AIContent[];
test("完整目录保留所有文档和祖先，搜索保留可定位路径", () => {
  const tree = buildKnowledgeTree(folders, docs);
  assert.equal(tree.length, 2);
  assert.equal(tree[0].children?.[0].children?.[0].key, "k:a");
  assert.equal(tree[1].key, "k:b");
  const search = buildKnowledgeTree(folders, docs, "指南");
  assert.equal(search.length, 1);
  assert.equal(search[0].children?.[0].children?.[0].key, "k:a");
  assert.equal(knowledgeFolderPath("child", folders), "产品 / 安装");
});
test("目录异常循环不导致路径或树构建无限递归", () => {
  const bad = [...folders, { id: "loop", name: "异常", parent_id: "loop", version: 1 }];
  assert.equal(knowledgeFolderPath("loop", bad), "异常");
  assert.doesNotThrow(() => buildKnowledgeTree(bad, docs));
});
