/** 文件作用说明：维护静态模板的源码节点、文件补丁和选中元素，避免将运行时聊天 DOM 保存回模板。 */
export type EditorFile = {
  path: string;
  content_type: string;
  size: number;
  sha256?: string;
  content?: string;
  data_base64?: string;
  protected: boolean;
};
export type EditorSource = {
  package: { id: string; name: string };
  entrypoint: string;
  files: EditorFile[];
  name?: string;
  status?: string;
  version?: number;
};
export type EditorNode = {
  id: string;
  tag: string;
  label: string;
  depth: number;
  text: string;
  leaf: boolean;
  style: string;
  className: string;
  src: string;
  alt: string;
  placeholder: string;
  core: boolean;
};
const ignored = new Set(["SCRIPT", "STYLE", "LINK", "META", "TEMPLATE", "BR"]);
const idPattern = /^[a-zA-Z][a-zA-Z0-9_-]{0,90}$/;

/** editableDocument 只解析用户源文件，不接触预览页面和官方脚本生成的节点。 */
function editableDocument(markup: string): Document {
  return new DOMParser().parseFromString(markup, "text/html");
}

/** serializeDocument 保留完整 HTML 文档与纯静态内容。 */
function serializeDocument(document: Document): string {
  return "<!doctype html>\n" + document.documentElement.outerHTML;
}

/** stampEditorNodes 为可编辑静态元素补稳定标识，已存在且唯一的标识保持不变。 */
export function stampEditorNodes(markup: string): string {
  const document = editableDocument(markup);
  const used = new Set<string>();
  const reserved = new Set(
    Array.from(document.body.querySelectorAll("[data-gc-edit-id]"))
      .map((node) => node.getAttribute("data-gc-edit-id") || "")
      .filter((id) => idPattern.test(id)),
  );
  let serial = 1;
  for (const node of document.body.querySelectorAll<HTMLElement>("*")) {
    if (ignored.has(node.tagName)) continue;
    let id = node.getAttribute("data-gc-edit-id") || "";
    if (!idPattern.test(id) || used.has(id)) {
      do {
        id = `edit-${serial++}`;
      } while (reserved.has(id) || used.has(id));
      node.setAttribute("data-gc-edit-id", id);
    }
    used.add(id);
  }
  return serializeDocument(document);
}

/** editorNodes 返回作者源码的扁平元素树，消息正文等运行时内容不会出现在此列表中。 */
export function editorNodes(markup: string): EditorNode[] {
  const document = editableDocument(markup);
  return Array.from(document.body.querySelectorAll<HTMLElement>("[data-gc-edit-id]")).map(
    (node) => {
      let depth = 0;
      let parent = node.parentElement;
      while (parent && parent !== document.body) {
        depth++;
        parent = parent.parentElement;
      }
      const text = node.textContent || "";
      return {
        id: node.getAttribute("data-gc-edit-id")!,
        tag: node.tagName.toLowerCase(),
        label:
          node.getAttribute("aria-label") ||
          node.getAttribute("alt") ||
          text.trim().slice(0, 24) ||
          node.className ||
          node.tagName.toLowerCase(),
        depth,
        text,
        leaf: node.children.length === 0,
        style: node.getAttribute("style") || "",
        className: node.className,
        src: node.getAttribute("src") || "",
        alt: node.getAttribute("alt") || "",
        placeholder: node.getAttribute("placeholder") || "",
        core: (node.id || "").startsWith("gc-") || node.hasAttribute("data-gc-action"),
      };
    },
  );
}

/** updateEditorNode 修改源码中的一个元素，只允许界面属性，始终保留核心 ID、类型和动作。 */
export function updateEditorNode(
  markup: string,
  id: string,
  patch: Partial<Pick<EditorNode, "text" | "style" | "className" | "src" | "alt" | "placeholder">>,
): string {
  const document = editableDocument(markup);
  const node = Array.from(document.body.querySelectorAll<HTMLElement>("[data-gc-edit-id]")).find(
    (item) => item.getAttribute("data-gc-edit-id") === id,
  );
  if (!node) throw new Error("选中的源码元素已经移除，请重新选择。");
  if (
    patch.text !== undefined &&
    node.children.length === 0 &&
    !["TEXTAREA", "INPUT", "IMG"].includes(node.tagName)
  )
    node.textContent = patch.text;
  for (const [key, value] of Object.entries(patch)) {
    if (key === "text") continue;
    node.setAttribute(key === "className" ? "class" : key, value);
  }
  return serializeDocument(document);
}

/** replaceEditorFile 只替换指定的普通文件，不允许修改受保护的核心文件。 */
export function replaceEditorFile(
  files: EditorFile[],
  path: string,
  content: string,
): EditorFile[] {
  const found = files.find((file) => file.path === path);
  if (found?.protected) throw new Error("官方核心文件只读，请编辑页面或扩展文件。");
  const next = {
    path,
    content,
    content_type:
      found?.content_type ||
      (path.endsWith(".css") ? "text/css" : path.endsWith(".js") ? "text/javascript" : "text/html"),
    protected: false,
    size: new TextEncoder().encode(content).byteLength,
  };
  return found ? files.map((file) => (file.path === path ? next : file)) : [...files, next];
}

/** editorFileChanges 仅提交发生变化的普通文件，二进制图片不会被转换成文本。 */
export function editorFileChanges(files: EditorFile[], baseline: EditorFile[]) {
  return files
    .filter((file) => {
      const original = baseline.find((item) => item.path === file.path);
      return (
        !file.protected &&
        (!original ||
          original.content !== file.content ||
          original.data_base64 !== file.data_base64)
      );
    })
    .map((file) => ({
      path: file.path,
      ...(typeof file.content === "string"
        ? { content: file.content }
        : { data_base64: file.data_base64 }),
    }));
}

/** editorImagePath 为本地图片生成安全的包内路径，不采用用户文件名中的路径片段。 */
export function editorImagePath(type: string, unique: string): string {
  const extension = (
    { "image/png": "png", "image/jpeg": "jpg", "image/webp": "webp", "image/gif": "gif" } as Record<
      string,
      string
    >
  )[type];
  if (!extension || !/^[a-zA-Z0-9_-]+$/.test(unique))
    throw new Error("不支持的图片类型或文件标识。");
  return `static/images/upload-${unique}.${extension}`;
}

/** editorAssetReference 将包根目录资源转换成入口文件的相对路径，兼容子目录 HTML 入口。 */
export function editorAssetReference(entrypoint: string, path: string): string {
  return "../".repeat(Math.max(0, entrypoint.split("/").length - 1)) + path;
}

/** insertEditorHTML 在选中静态元素后追加片段，无选中项时置于核心脚本之前，源码只在隔离预览执行。 */
export function insertEditorHTML(markup: string, selected: string, fragment: string): string {
  const document = editableDocument(markup);
  const container = document.createElement("template");
  container.innerHTML = fragment;
  const node = Array.from(document.body.querySelectorAll("[data-gc-edit-id]")).find(
    (item) => item.getAttribute("data-gc-edit-id") === selected,
  );
  const anchor = node || document.body.querySelector("script");
  if (node) node.after(container.content);
  else if (anchor) anchor.before(container.content);
  else document.body.append(container.content);
  return stampEditorNodes(serializeDocument(document));
}
