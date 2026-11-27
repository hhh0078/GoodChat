/** 文件作用说明：把树选择返回值限制为显式文档UUID，目录节点和标签对象不能成为机器人授权。 */

/** knowledgeSelectionIDs 保留显式文档编号并去重，不依赖搜索后的可见节点，以免移除隐藏的既有授权。 */
export function knowledgeSelectionIDs(values: readonly unknown[]): string[] {
  return [
    ...new Set(
      values.filter(
        (value): value is string =>
          typeof value === "string" &&
          /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value),
      ),
    ),
  ];
}
