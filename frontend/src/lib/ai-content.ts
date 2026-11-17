/** 文件作用说明：在文本光标位置插入稳定话术引用或富媒体内容块，保留前后文字顺序。 */
import type { AIBlock } from "../features/ai/types.ts";
/** insertKnowledgeReference 仅插入编号引用，引用本身不授予机器人使用权限。 */
export function insertKnowledgeReference(
  text: string,
  start: number,
  end: number,
  id: string,
): string {
  const left = Math.max(0, Math.min(start, text.length));
  const right = Math.max(left, Math.min(end, text.length));
  return `${text.slice(0, left)}[[script:${id}]]${text.slice(right)}`;
}
/** insertMediaBlock 在选中文字处放置媒体，其他内容块保持原顺序。 */
export function insertMediaBlock(
  blocks: AIBlock[],
  index: number,
  start: number,
  end: number,
  media: AIBlock,
): AIBlock[] {
  if (index < 0 || index >= blocks.length || blocks[index].type !== "text")
    return [...blocks, media];
  const text = blocks[index].text || "";
  const left = Math.max(0, Math.min(start, text.length));
  const right = Math.max(left, Math.min(end, text.length));
  const parts: AIBlock[] = [];
  if (left > 0) parts.push({ type: "text", text: text.slice(0, left) });
  parts.push(media);
  if (right < text.length) parts.push({ type: "text", text: text.slice(right) });
  return [...blocks.slice(0, index), ...parts, ...blocks.slice(index + 1)];
}
