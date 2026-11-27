/** 文件作用说明：回归知识树祖先保留、旧根节点兼容、路径区分和异常循环防护。 */
import assert from "node:assert/strict";
import test from "node:test";
import {
  buildKnowledgeTree,
  knowledgeFolderPath,
  knowledgeDescendantFolderIDs,
  knowledgeDirectoryContents,
  knowledgeDocumentSelectTree,
  knowledgeFolderSelectTree,
} from "../src/features/ai/knowledge-tree-data.ts";
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

const products = [
  { id: "hr", name: "GoodHR", parent_id: null, version: 1 },
  { id: "chat", name: "GoodChat", parent_id: null, version: 1 },
  { id: "maintenance", name: "维护记录", parent_id: "hr", version: 1 },
  { id: "sample", name: "验收样例", parent_id: "maintenance", version: 1 },
  { id: "empty", name: "空目录", parent_id: null, version: 1 },
];
const productDocuments = [
  {
    id: "hr-doc",
    title: "简历插件",
    description: "识别招聘平台资料",
    folder_id: "hr",
    enabled: true,
  },
  {
    id: "chat-doc",
    title: "免费坐席",
    description: "两个基础坐席",
    folder_id: "chat",
    enabled: true,
  },
  {
    id: "qa-doc",
    title: "演示代号",
    description: "非业务知识",
    folder_id: "sample",
    enabled: false,
  },
] as AIContent[];

test("根层只列产品目录，进入目录仅列直接子目录和文档，空目录有空结果", () => {
  const root = knowledgeDirectoryContents(products, productDocuments);
  assert.equal(root.documents.length, 0);
  assert.deepEqual(
    root.folders.map((folder) => folder.id),
    ["hr", "chat", "empty"],
  );
  const hr = knowledgeDirectoryContents(products, productDocuments, "hr");
  assert.deepEqual(
    hr.folders.map((folder) => folder.id),
    ["maintenance"],
  );
  assert.deepEqual(
    hr.documents.map((document) => document.id),
    ["hr-doc"],
  );
  assert.deepEqual(knowledgeDirectoryContents(products, productDocuments, "empty"), {
    folders: [],
    documents: [],
  });
});

test("全文搜索可从其他目录找到后代，匹配描述并保留已选节点与祖先", () => {
  const search = knowledgeDirectoryContents(products, productDocuments, "chat", "识别");
  assert.deepEqual(
    search.documents.map((document) => document.id),
    ["hr-doc"],
  );
  const tree = buildKnowledgeTree(products, productDocuments, "基础坐席", "k:hr-doc");
  assert.equal(tree.length, 2);
  assert.equal(tree.find((node) => node.key === "f:hr")?.children?.[0].key, "k:hr-doc");
  assert.equal(tree.find((node) => node.key === "f:chat")?.children?.[0].key, "k:chat-doc");
  const descendant = buildKnowledgeTree(products, productDocuments, "演示");
  assert.equal(descendant[0].children?.[0].children?.[0].children?.[0].key, "k:qa-doc");
});

test("完整祖先路径检索在目录树和右侧结果一致保留命中子树", () => {
  const query = "goodhr / 维护记录";
  const tree = buildKnowledgeTree(products, productDocuments, query);
  assert.deepEqual(
    tree.map((node) => node.key),
    ["f:hr"],
  );
  assert.equal(tree[0].children?.[0].key, "f:maintenance");
  assert.equal(tree[0].children?.length, 1);
  assert.equal(tree[0].children?.[0].children?.[0].children?.[0].key, "k:qa-doc");
  assert.deepEqual(
    knowledgeDirectoryContents(products, productDocuments, "", query).documents.map(
      (doc) => doc.id,
    ),
    ["qa-doc"],
  );
  const documentPath = "GoodHR / 维护记录 / 验收样例 演示代号";
  assert.equal(
    buildKnowledgeTree(products, productDocuments, documentPath)[0].children?.[0].children?.[0]
      .children?.[0].key,
    "k:qa-doc",
  );
});

test("父目录树排除自身与所有后代，其他产品目录可选，异常循环不阻塞", () => {
  assert.deepEqual([...knowledgeDescendantFolderIDs("hr", products)].sort(), [
    "hr",
    "maintenance",
    "sample",
  ]);
  assert.deepEqual(
    knowledgeFolderSelectTree(products, "hr").map((folder) => folder.value),
    ["chat", "empty"],
  );
  assert.deepEqual(
    knowledgeFolderSelectTree(products, "maintenance").find((folder) => folder.value === "hr")
      ?.children,
    [],
  );
  const cycle = [
    { id: "a", name: "A", parent_id: "b", version: 1 },
    { id: "b", name: "B", parent_id: "a", version: 1 },
  ];
  assert.deepEqual([...knowledgeDescendantFolderIDs("a", cycle)].sort(), ["a", "b"]);
});

test("文档选择树只返回原UUID叶子，目录不可单选且无有效文档的空目录不能授权", () => {
  const before = JSON.stringify({ products, productDocuments });
  const selected = knowledgeDocumentSelectTree(
    products,
    productDocuments.filter((document) => document.enabled),
  );
  const hr = selected.find((node) => node.value === "f:hr")!;
  assert.equal(hr.selectable, false);
  assert.equal(hr.checkable, true);
  assert.equal(hr.children?.find((node) => node.value === "hr-doc")?.isLeaf, true);
  assert.equal(hr.children?.find((node) => node.value === "f:maintenance")?.checkable, false);
  assert.equal(selected.find((node) => node.value === "f:empty")?.checkable, false);
  assert.equal(knowledgeFolderPath("sample", products), "GoodHR / 维护记录 / 验收样例");
  assert.equal(JSON.stringify({ products, productDocuments }), before);
});
