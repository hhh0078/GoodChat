/** 文件作用说明：为官方动态聊天元素维护受限选择器样式，避免把模拟消息正文保存回 HTML。 */
import { editorAssetReference, replaceEditorFile, type EditorFile } from "./static-editor-model.ts";

export const editorStyleSelectors = new Set([
  ".gc-message-row.gc-own .gc-message",
  ".gc-message-row:not(.gc-own):not(.gc-system) .gc-message",
  ".gc-message-row.gc-system .gc-message",
  ".gc-avatar",
  ".gc-message-meta",
  ".gc-read-status",
  ".gc-welcome-option",
  ".gc-reply",
  ".gc-file-save",
  ".gc-attachment-preview",
]);
const path = "static/editor-overrides.css";
const endMarker = "/* End GoodChat editor */";

/** editorRuleDeclarations 只读取由编辑器写入的对应聊天样式块，不解析或改写作者其他 CSS。 */
export function editorRuleDeclarations(files: EditorFile[], selector: string): string {
  if (!editorStyleSelectors.has(selector)) return "";
  const content = files.find((file) => file.path === path)?.content || "";
  const start = content.indexOf(`/* GoodChat editor ${selector} */`);
  if (start < 0) return "";
  const end = content.indexOf(endMarker, start);
  const block = content.slice(start, end < 0 ? undefined : end);
  return block.slice(block.indexOf("{") + 1, block.lastIndexOf("}")).trim();
}

/** updateEditorRule 为官方白名单聊天元素追加 CSS 文件，保持模拟内容与固定脚本不变。 */
export function updateEditorRule(
  files: EditorFile[],
  entrypoint: string,
  markup: string,
  selector: string,
  declarations: string,
): EditorFile[] {
  if (!editorStyleSelectors.has(selector)) throw new Error("只能为已登记的聊天界面元素修改样式。");
  const marker = `/* GoodChat editor ${selector} */`;
  let content =
    files.find((file) => file.path === path)?.content ||
    "/* 文件作用说明：保存通过元素编辑器设置的聊天样式，不包含业务逻辑。 */\n";
  const start = content.indexOf(marker);
  const end = start < 0 ? -1 : content.indexOf(endMarker, start);
  if (start >= 0 && end >= 0)
    content = content.slice(0, start) + content.slice(end + endMarker.length);
  content = content.trimEnd() + `\n${marker}\n${selector} {\n${declarations}\n}\n${endMarker}\n`;
  let next = replaceEditorFile(files, path, content);
  const document = new DOMParser().parseFromString(markup, "text/html");
  const href = editorAssetReference(entrypoint, path);
  if (
    !Array.from(document.querySelectorAll('link[rel="stylesheet"]')).some(
      (node) => node.getAttribute("href") === href,
    )
  ) {
    const link = document.createElement("link");
    link.setAttribute("rel", "stylesheet");
    link.setAttribute("href", href);
    document.head.append(link);
    next = replaceEditorFile(
      next,
      entrypoint,
      "<!doctype html>\n" + document.documentElement.outerHTML,
    );
  }
  return next;
}
