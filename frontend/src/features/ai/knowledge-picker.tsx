/** 文件作用说明：使用管理知识库的完整目录树勾选机器人可读文档，授权仍保存显式文档编号。 */
"use client";
import { useTranslator } from "@/i18n/use-translator";
import { TreeSelect } from "antd";
import { knowledgeSelectionIDs } from "./knowledge-picker-data";
import { knowledgeDocumentSelectTree, type AIFolder } from "./knowledge-tree-data";
import type { AIContent } from "./types";

/** AIKnowledgePicker 保留搜索隐藏项及暂缺节点的选中值，目录勾选仅展开当前后代文档。 */
export function AIKnowledgePicker({
  value = [],
  onChange,
  folders,
  documents,
  loading,
  disabled,
  id,
}: {
  value?: string[];
  onChange?: (ids: string[]) => void;
  folders: AIFolder[];
  documents: AIContent[];
  loading: boolean;
  disabled: boolean;
  id?: string;
}) {
  const tr = useTranslator();
  return (
    <TreeSelect<string[]>
      id={id}
      className="ai-knowledge-picker"
      value={value}
      onChange={(ids) => onChange?.(knowledgeSelectionIDs(ids))}
      treeData={knowledgeDocumentSelectTree(
        folders,
        documents.filter((document) => document.enabled),
      )}
      treeCheckable
      showCheckedStrategy={TreeSelect.SHOW_CHILD}
      treeDefaultExpandAll
      treeLine
      showSearch={{ treeNodeFilterProp: "title" }}
      placeholder={tr("选择知识目录或文档")}
      allowClear
      maxTagCount="responsive"
      loading={loading}
      disabled={disabled || loading}
      listHeight={320}
    />
  );
}
