/** 文件作用说明：构建安全的知识目录及路径标签，文档正文不进入目录节点。 */
import type { AIContent } from "./types";
export type AIFolder = { id: string; parent_id: string | null; name: string; version: number };
export type KnowledgeNode = {
  key: string;
  title: string;
  isLeaf?: boolean;
  children?: KnowledgeNode[];
};
export type KnowledgeSelectNode = {
  value: string;
  title: string;
  selectable?: boolean;
  checkable?: boolean;
  isLeaf?: boolean;
  children?: KnowledgeSelectNode[];
};

/** knowledgeFolderPath 用完整祖先路径区分同名目录，并防御损坏响应中的循环引用。 */
export function knowledgeFolderPath(id: string, folders: AIFolder[]): string {
  const seen = new Set<string>();
  const names: string[] = [];
  let current = id;
  while (current && !seen.has(current)) {
    seen.add(current);
    const folder = folders.find((item) => item.id === current);
    if (!folder) break;
    names.unshift(folder.name);
    current = folder.parent_id || "";
  }
  return names.join(" / ");
}

/** buildKnowledgeTree 展示完整层级；搜索保留命中文档的祖先，避免只显示无法定位的叶子。 */
export function buildKnowledgeTree(
  folders: AIFolder[],
  documents: AIContent[],
  search = "",
  keepKey = "",
): KnowledgeNode[] {
  const nodes = new Map<string, KnowledgeNode>();
  for (const f of folders) nodes.set(f.id, { key: `f:${f.id}`, title: f.name, children: [] });
  const roots: KnowledgeNode[] = [];
  for (const f of folders) {
    const node = nodes.get(f.id)!;
    const parent = nodes.get(f.parent_id || "");
    if (parent && f.parent_id !== f.id) parent.children!.push(node);
    else roots.push(node);
  }
  for (const d of documents) {
    const node: KnowledgeNode = { key: `k:${d.id}`, title: d.title, isLeaf: true };
    const folder = nodes.get(d.folder_id || "");
    if (folder) folder.children!.push(node);
    else roots.push(node);
  }
  const seen = new Set<string>();
  const query = search.trim().toLowerCase();
  const descriptions = new Map(documents.map((d) => [`k:${d.id}`, d.description || ""]));
  /** walk 排序并过滤树节点，使用访问集合阻止异常循环造成页面崩溃。 */
  function walk(items: KnowledgeNode[], ancestorMatch = false, parentPath = ""): KnowledgeNode[] {
    return [...items]
      .sort((a, b) => Number(!!a.isLeaf) - Number(!!b.isLeaf) || a.title.localeCompare(b.title))
      .flatMap((node) => {
        if (seen.has(node.key)) return [];
        seen.add(node.key);
        const path = parentPath ? `${parentPath} / ${node.title}` : node.title;
        const searchText = node.isLeaf
          ? `${parentPath} ${node.title} ${descriptions.get(node.key) || ""}`
          : path;
        const matches = ancestorMatch || !query || searchText.toLowerCase().includes(query);
        const children = walk(node.children || [], matches, path);
        return matches || children.length || node.key === keepKey
          ? [{ ...node, ...(!node.isLeaf ? { children } : {}) }]
          : [];
      });
  }
  return walk(roots);
}

/** knowledgeDescendantFolderIDs 收集目录自身及全部后代，防止循环关系使父目录选择或统计无限遍历。 */
export function knowledgeDescendantFolderIDs(id: string, folders: AIFolder[]): Set<string> {
  const result = new Set<string>();
  const pending = [id];
  while (pending.length) {
    const current = pending.pop()!;
    if (result.has(current)) continue;
    result.add(current);
    for (const folder of folders) if (folder.parent_id === current) pending.push(folder.id);
  }
  return result;
}

/** knowledgeFolderSelectTree 返回真实目录选择树，可排除编辑目录自身及其后代作为上级目录。 */
export function knowledgeFolderSelectTree(
  folders: AIFolder[],
  excludeBranchId = "",
): KnowledgeSelectNode[] {
  const blocked = excludeBranchId
    ? knowledgeDescendantFolderIDs(excludeBranchId, folders)
    : new Set<string>();
  /** convert 将内部目录key转换为接口使用的原目录UUID。 */
  function convert(node: KnowledgeNode): KnowledgeSelectNode {
    return {
      value: node.key.slice(2),
      title: node.title,
      children: node.children?.map(convert),
    };
  }
  return buildKnowledgeTree(
    folders.filter((folder) => !blocked.has(folder.id)),
    [],
  ).map(convert);
}

/** knowledgeDocumentSelectTree 共用管理页层级，只有文档叶子可作为接口选择值，空目录不显示授权复选框。 */
export function knowledgeDocumentSelectTree(
  folders: AIFolder[],
  documents: AIContent[],
  search = "",
): KnowledgeSelectNode[] {
  /** convert 保留目录导航与勾选层级，文档值始终保持原UUID。 */
  function convert(node: KnowledgeNode): KnowledgeSelectNode {
    if (node.isLeaf)
      return { value: node.key.slice(2), title: node.title, isLeaf: true, selectable: true };
    const children = (node.children || []).map(convert);
    return {
      value: node.key,
      title: node.title,
      selectable: false,
      checkable: children.some((child) => child.isLeaf || child.checkable),
      children,
    };
  }
  return buildKnowledgeTree(folders, documents, search).map(convert);
}

/** knowledgeDirectoryContents 平常仅列直接子项；全文搜索跨目录匹配路径、标题和描述，不改变文档或授权。 */
export function knowledgeDirectoryContents(
  folders: AIFolder[],
  documents: AIContent[],
  parentId = "",
  search = "",
): { folders: AIFolder[]; documents: AIContent[] } {
  const query = search.trim().toLowerCase();
  if (!query)
    return {
      folders: folders.filter((folder) => (folder.parent_id || "") === parentId),
      documents: documents.filter((document) => (document.folder_id || "") === parentId),
    };
  return {
    folders: folders.filter((folder) =>
      knowledgeFolderPath(folder.id, folders).toLowerCase().includes(query),
    ),
    documents: documents.filter((document) =>
      `${knowledgeFolderPath(document.folder_id || "", folders)} ${document.title} ${document.description || ""}`
        .toLowerCase()
        .includes(query),
    ),
  };
}
