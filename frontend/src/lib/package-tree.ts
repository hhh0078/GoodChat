/** 文件作用说明：把已校验资源清单转换为稳定文件树，文件选择只保留原始相对路径。 */
export type PackageTreeNode = {
  title: string;
  key: string;
  isLeaf?: boolean;
  children?: PackageTreeNode[];
};
/** buildPackageTree 构建目录优先的资源树，忽略无法表示为安全相对路径的异常清单项。 */
export function buildPackageTree(files: { path: string }[]): PackageTreeNode[] {
  const roots: PackageTreeNode[] = [];
  for (const file of files) {
    const parts = file.path.split("/");
    if (parts.some((part) => !part || part === "." || part === "..") || file.path.includes("\\"))
      continue;
    let children = roots;
    for (let index = 0; index < parts.length; index++) {
      const path = parts.slice(0, index + 1).join("/");
      const leaf = index === parts.length - 1;
      const key = `${leaf ? "file" : "dir"}:${path}`;
      let node = children.find((item) => item.key === key);
      if (!node) {
        node = { title: parts[index], key, isLeaf: leaf, ...(leaf ? {} : { children: [] }) };
        children.push(node);
      }
      if (!leaf) children = node.children!;
    }
  }
  /** sortNodes 保持目录先于文件并按显示名稳定排序。 */
  function sortNodes(nodes: PackageTreeNode[]) {
    nodes.sort((a, b) => Number(a.isLeaf) - Number(b.isLeaf) || a.title.localeCompare(b.title));
    nodes.forEach((node) => {
      if (node.children) sortNodes(node.children);
    });
  }
  sortNodes(roots);
  return roots;
}
