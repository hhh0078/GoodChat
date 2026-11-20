/** 文件作用说明：构建安全的知识目录及路径标签，文档正文不进入目录节点。 */
import type { AIContent } from "./types";
export type AIFolder = { id: string; parent_id: string | null; name: string; version: number };
export type KnowledgeNode = {
  key: string;
  title: string;
  isLeaf?: boolean;
  children?: KnowledgeNode[];
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
  /** walk 排序并过滤树节点，使用访问集合阻止异常循环造成页面崩溃。 */
  function walk(items: KnowledgeNode[], ancestorMatch = false): KnowledgeNode[] {
    return [...items]
      .sort((a, b) => Number(!!a.isLeaf) - Number(!!b.isLeaf) || a.title.localeCompare(b.title))
      .flatMap((node) => {
        if (seen.has(node.key)) return [];
        seen.add(node.key);
        const matches = ancestorMatch || !query || node.title.toLowerCase().includes(query);
        const children = walk(node.children || [], matches);
        return matches || children.length
          ? [{ ...node, ...(!node.isLeaf ? { children } : {}) }]
          : [];
      });
  }
  return walk(roots);
}
