/** 文件作用说明：验证现有TreeSelect包的目录聚合只保存显式文档UUID，搜索隐藏值和未来文档不会改变授权。 */
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";
import { knowledgeSelectionIDs } from "../src/features/ai/knowledge-picker-data.ts";
import { knowledgeDocumentSelectTree } from "../src/features/ai/knowledge-tree-data.ts";
import type { AIContent } from "../src/features/ai/types.ts";

const require = createRequire(import.meta.url);
const { convertDataToEntities } = require("@rc-component/tree/lib/utils/treeUtil.js");
const { conductCheck } = require("@rc-component/tree/lib/utils/conductUtil.js");
const {
  formatStrategyValues,
  SHOW_CHILD,
} = require("@rc-component/tree-select/lib/utils/strategyUtil.js");
const first = "11111111-1111-4111-8111-111111111111";
const second = "22222222-2222-4222-8222-222222222222";
const future = "33333333-3333-4333-8333-333333333333";
const missing = "44444444-4444-4444-8444-444444444444";
const folders = [
  { id: "product", name: "GoodHR", parent_id: null, version: 1 },
  { id: "category", name: "产品功能", parent_id: "product", version: 1 },
];
const documents = [
  { id: first, title: "产品介绍", folder_id: "category", enabled: true },
  { id: second, title: "操作指南", folder_id: "category", enabled: true },
] as AIContent[];
const fields = { key: "value", value: "value", title: "title", children: "children" };

/** checkedDocuments 使用已安装包执行与TreeSelect相同的父子勾选及SHOW_CHILD策略。 */
function checkedDocuments(keys: string[], docs = documents): string[] {
  const tree = knowledgeDocumentSelectTree(folders, docs);
  const { keyEntities } = convertDataToEntities(tree, { fieldNames: fields });
  const { checkedKeys } = conductCheck(keys, true, keyEntities);
  return knowledgeSelectionIDs(formatStrategyValues(checkedKeys, SHOW_CHILD, keyEntities, fields));
}

test("产品或分类批量勾选展开当下文档，提交值不包含文件夹键", () => {
  assert.deepEqual(new Set(checkedDocuments(["f:product"])), new Set([first, second]));
  assert.deepEqual(new Set(checkedDocuments(["f:category"])), new Set([first, second]));
  assert.deepEqual(
    knowledgeSelectionIDs(["f:product", first, first, { value: second }, "invalid"]),
    [first],
  );
});

test("保存后的叶子授权不会因未来新增同目录文档而扩大", () => {
  const saved = checkedDocuments(["f:product"]);
  const later = [...documents, { ...documents[0], id: future, title: "新增文档" }];
  assert.deepEqual(new Set(checkedDocuments(saved, later)), new Set([first, second]));
  assert.equal(checkedDocuments(saved, later).includes(future), false);
});

test("转换不以搜索可见列表裁剪原授权，暂缺但有效的文档编号仍保留", () => {
  assert.deepEqual(knowledgeSelectionIDs([first, missing, second]), [first, missing, second]);
  assert.deepEqual(knowledgeSelectionIDs([]), []);
});
