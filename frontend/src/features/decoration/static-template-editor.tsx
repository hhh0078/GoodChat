/** 文件作用说明：以原始静态资源包为基础提供元素点选、源码和图片编辑，并保存为独立版本草稿。 */
"use client";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { App, Button, Input, Segmented, Select, Space, Spin, Tag } from "antd";
import {
  CodeOutlined,
  PlusOutlined,
  ReloadOutlined,
  SaveOutlined,
  UploadOutlined,
} from "@ant-design/icons";
import { api, errorMessage } from "@/lib/api";
import { ErrorNotice } from "@/components/page-state";
import { StaticEditorPreview, type EditorPreview } from "./static-editor-preview";
import { StaticEditorInspector } from "./static-editor-inspector";
import {
  StaticEditorSidebar,
  StaticEditorSource,
  StaticEditorHTMLDialog,
} from "./static-editor-files";
import {
  editorFileChanges,
  editorAssetReference,
  editorNodes,
  insertEditorHTML,
  replaceEditorFile,
  stampEditorNodes,
  updateEditorNode,
  type EditorFile,
  type EditorNode,
  type EditorSource,
} from "./static-editor-model";
import { prepareEditorImage } from "./static-editor-image";
import { editorRuleDeclarations, updateEditorRule } from "./static-editor-styles";
import "./static-template-editor.css";

export type StaticEditorTarget = {
  scope: "tenant" | "platform";
  kind?: "templates" | "pages";
  id: string;
  name: string;
  version: number;
  packageId?: string;
  status?: string;
};

/** StaticTemplateEditor 维护本次编辑草稿；保存静态包后再以版本号更新模板，发布仍由原页面完成。 */
export function StaticTemplateEditor({
  target,
  tenantId,
  onSaved,
  onDirtyChange,
}: {
  target: StaticEditorTarget;
  tenantId?: string;
  onSaved: () => void | Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { message, modal } = App.useApp();
  const [source, setSource] = useState<EditorSource>();
  const [files, setFiles] = useState<EditorFile[]>([]);
  const [name, setName] = useState(target.name);
  const [status, setStatus] = useState(target.status || "hidden");
  const [initial, setInitial] = useState("");
  const [selection, setSelection] = useState("");
  const [styleSelector, setStyleSelector] = useState<string>();
  const [mode, setMode] = useState<"select" | "interact">("select");
  const [preview, setPreview] = useState<EditorPreview>();
  const [previewSignature, setPreviewSignature] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [codePath, setCodePath] = useState("");
  const [htmlOpen, setHTMLOpen] = useState(false);
  const [reload, setReload] = useState(0);
  const upload = useRef<HTMLInputElement>(null);
  const requestSerial = useRef(0);
  const activeRequest = useRef<AbortController | undefined>(undefined);
  const pendingPackage = useRef<{ signature: string; id: string } | undefined>(undefined);
  const signature = JSON.stringify({ name, status, files });
  const dirty = !!source && signature !== initial;
  const entrypoint = source?.entrypoint || "index.html";
  const entry = files.find((file) => file.path === entrypoint)?.content || "";
  const nodes = useMemo(() => (entry ? editorNodes(entry) : []), [entry]);
  const selected = nodes.find((node) => node.id === selection);
  const base =
    target.scope === "platform"
      ? `/platform/templates/${target.id}`
      : `/tenant/code-packages/${target.packageId}`;
  const chooseElement = useCallback((id: string, selector?: string) => {
    setSelection(id);
    setStyleSelector(selector);
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    activeRequest.current = controller;
    const serial = ++requestSerial.current;
    setSource(undefined);
    setFiles([]);
    setPreview(undefined);
    setError(null);
    setBusy("load");
    /** load 读取认证接口中的源文件，只对入口副本添加选择标识。 */
    async function load() {
      try {
        const result = await api<EditorSource>(`${base}/source`, {
          tenantId,
          signal: controller.signal,
        });
        if (serial !== requestSerial.current || controller.signal.aborted) return;
        const prepared = result.files.map((file) =>
          file.path === result.entrypoint && typeof file.content === "string"
            ? { ...file, content: stampEditorNodes(file.content) }
            : file,
        );
        const nextName = result.name || target.name;
        const nextStatus = result.status || target.status || "hidden";
        setSource(result);
        setFiles(prepared);
        setName(nextName);
        setStatus(nextStatus);
        setSelection("");
        setStyleSelector(undefined);
        setCodePath("");
        pendingPackage.current = undefined;
        setInitial(JSON.stringify({ name: nextName, status: nextStatus, files: prepared }));
        await requestPreview(result, prepared, serial, controller.signal);
      } catch (problem) {
        if (!controller.signal.aborted && serial === requestSerial.current) setError(problem);
      } finally {
        if (!controller.signal.aborted && serial === requestSerial.current) setBusy("");
      }
    }
    void load();
    return () => {
      controller.abort();
      requestSerial.current++;
    };
  }, [base, tenantId, target.id, target.version, reload]);
  useEffect(() => {
    onDirtyChange?.(dirty);
    return () => onDirtyChange?.(false);
  }, [dirty, onDirtyChange]);
  useEffect(() => {
    if (!dirty) return;
    /** prevent 保护当前尚未保存的源码与图片改动。 */
    function prevent(event: BeforeUnloadEvent) {
      event.preventDefault();
      event.returnValue = "";
    }
    window.addEventListener("beforeunload", prevent);
    return () => window.removeEventListener("beforeunload", prevent);
  }, [dirty]);
  /** requestPreview 创建短期编辑快照，版本与随机数只用于本次隔离通信。 */
  async function requestPreview(
    snapshot: EditorSource,
    candidate: EditorFile[],
    serial: number,
    signal?: AbortSignal,
  ) {
    const nonce = crypto.randomUUID();
    const result = await api<Omit<EditorPreview, "nonce">>(`${base}/editor-preview`, {
      method: "POST",
      tenantId,
      signal,
      bodyJSON: {
        entrypoint: snapshot.entrypoint,
        files: editorFileChanges(candidate, snapshot.files),
        editor_nonce: nonce,
      },
    });
    if (serial !== requestSerial.current || signal?.aborted) return;
    setPreview({ ...result, nonce });
    setPreviewSignature(JSON.stringify(candidate));
  }
  /** refreshPreview 将当前源码校验为新快照，错误时保留全部本地改动。 */
  async function refreshPreview() {
    if (!source || busy) return;
    const serial = requestSerial.current;
    setBusy("preview");
    setError(null);
    try {
      const prepared = replaceEditorFile(files, entrypoint, stampEditorNodes(entry));
      setFiles(prepared);
      await requestPreview(source, prepared, serial, activeRequest.current?.signal);
    } catch (problem) {
      if (serial === requestSerial.current) setError(problem);
    } finally {
      if (serial === requestSerial.current) setBusy("");
    }
  }
  /** updateNode 只修改被选中的作者节点，不保存 iframe 的动态消息或资源地址。 */
  function updateNode(
    patch: Partial<
      Pick<EditorNode, "text" | "style" | "className" | "src" | "alt" | "placeholder">
    >,
  ) {
    try {
      if (styleSelector) {
        if (patch.style !== undefined)
          setFiles(updateEditorRule(files, entrypoint, entry, styleSelector, patch.style));
        return;
      }
      setFiles(replaceEditorFile(files, entrypoint, updateEditorNode(entry, selection, patch)));
    } catch (problem) {
      setError(problem);
    }
  }
  /** save 先创建不可变包，再用版本号保存草稿；并发冲突时不丢弃用户修改。 */
  async function save() {
    if (!source || busy || !name.trim()) return;
    const serial = requestSerial.current;
    const signal = activeRequest.current?.signal;
    setBusy("save");
    setError(null);
    try {
      const prepared = replaceEditorFile(files, entrypoint, stampEditorNodes(entry));
      const changes = editorFileChanges(prepared, source.files);
      if (target.scope === "platform") {
        await api(`${base}/revisions`, {
          method: "POST",
          tenantId,
          signal,
          bodyJSON: {
            version: source.version || target.version,
            name: name.trim(),
            status,
            entrypoint,
            files: changes,
          },
        });
      } else {
        let packageId = target.packageId!;
        const currentSignature = JSON.stringify(prepared);
        if (changes.length) {
          if (pendingPackage.current?.signature === currentSignature)
            packageId = pendingPackage.current.id;
          else {
            const result = await api<{ package: { id: string } }>(`${base}/revisions`, {
              method: "POST",
              tenantId,
              signal,
              bodyJSON: { name: name.trim(), entrypoint, files: changes },
            });
            if (serial !== requestSerial.current) return;
            packageId = result.package.id;
            pendingPackage.current = { signature: currentSignature, id: packageId };
          }
        }
        await api(`/tenant/${target.kind || "templates"}/${target.id}`, {
          method: "PATCH",
          tenantId,
          signal,
          bodyJSON: {
            name: name.trim(),
            render_mode: "code",
            version: target.version,
            [target.kind === "pages" ? "draft" : "design"]: {
              package_id: packageId,
              html: "",
              css: "",
              javascript: "",
            },
          },
        });
      }
      if (serial !== requestSerial.current) return;
      setInitial(JSON.stringify({ name, status, files: prepared }));
      setFiles(prepared);
      message.success(
        target.scope === "platform"
          ? "平台模板新版本已保存"
          : "模板草稿已保存，线上页面需发布后更新",
      );
      await onSaved();
      if (serial === requestSerial.current) setReload((value) => value + 1);
    } catch (problem) {
      if (serial === requestSerial.current) setError(problem);
    } finally {
      if (serial === requestSerial.current) setBusy("");
    }
  }
  /** selectImage 将上传图片写入本地包草稿，选中图片节点时同步替换源路径。 */
  async function selectImage(file?: File) {
    if (!file || busy) return;
    const serial = requestSerial.current;
    setBusy("image");
    setError(null);
    try {
      const result = await prepareEditorImage(file);
      if (serial !== requestSerial.current) return;
      const next = [...files, result.file];
      setFiles(
        selected?.tag === "img"
          ? replaceEditorFile(
              next,
              entrypoint,
              updateEditorNode(entry, selection, {
                src: editorAssetReference(entrypoint, result.file.path),
              }),
            )
          : next,
      );
      message.success(
        result.compressed
          ? "图片已压缩并加入草稿，保存后写入模板"
          : "图片已加入草稿，保存后写入模板",
      );
    } catch (problem) {
      if (serial === requestSerial.current) setError(problem);
    } finally {
      if (serial === requestSerial.current) setBusy("");
      if (upload.current) upload.current.value = "";
    }
  }
  /** discard 经确认后重新读取服务端版本，不触碰已发布版本。 */
  function discard() {
    const serial = requestSerial.current;
    if (!dirty) {
      setReload((value) => value + 1);
      return;
    }
    modal.confirm({
      title: "放弃当前未保存的改动？",
      okText: "重新读取",
      cancelText: "继续编辑",
      onOk: () => {
        if (serial === requestSerial.current) setReload((value) => value + 1);
      },
    });
  }
  /** openExtension 打开唯一受控的额外界面脚本，缺少时补入已有入口的固定脚本之后。 */
  function openExtension() {
    let next = files;
    if (!files.some((file) => file.path === "static/custom.js")) {
      next = replaceEditorFile(
        next,
        "static/custom.js",
        "/** 文件作用说明：仅处理当前模板的附加界面交互；消息与业务动作调用官方 GoodChat SDK。 */\n",
      );
      const document = new DOMParser().parseFromString(entry, "text/html");
      const script = document.createElement("script");
      script.setAttribute("src", editorAssetReference(entrypoint, "static/custom.js"));
      document.body.append(script);
      next = replaceEditorFile(
        next,
        entrypoint,
        "<!doctype html>\n" + document.documentElement.outerHTML,
      );
      setFiles(next);
    }
    setCodePath("static/custom.js");
  }
  if (!source)
    return (
      <div className="static-editor-loading">
        <ErrorNotice error={error} retry={() => setReload((value) => value + 1)} />
        {!error && (
          <>
            <Spin />
            <p>正在读取模板源文件</p>
          </>
        )}
      </div>
    );
  return (
    <section className="static-template-editor" aria-label="静态模板可视化编辑器">
      <div className="static-editor-topbar">
        <div>
          <Input
            aria-label="模板编辑名称"
            value={name}
            maxLength={100}
            onChange={(event) => setName(event.target.value)}
            disabled={!!busy}
          />
          <span className="static-editor-help">
            {target.scope === "platform" ? "平台设计" : "独立草稿"} · v
            {source.version || target.version}
            {dirty && <Tag color="gold">未保存</Tag>}
          </span>
        </div>
        <Space wrap>
          <Segmented
            aria-label="模板编辑模式"
            value={mode}
            onChange={(value) => setMode(value as "select" | "interact")}
            options={[
              { label: "选择编辑", value: "select" },
              { label: "交互预览", value: "interact" },
            ]}
          />
          <Button
            icon={<ReloadOutlined />}
            onClick={() => void refreshPreview()}
            loading={busy === "preview"}
            disabled={!!busy && busy !== "preview"}
          >
            更新预览
          </Button>
          <Button
            type="primary"
            icon={<SaveOutlined />}
            onClick={() => void save()}
            loading={busy === "save"}
            disabled={!dirty || !name.trim() || (!!busy && busy !== "save")}
          >
            保存{target.scope === "platform" ? "新版本" : "草稿"}
          </Button>
        </Space>
      </div>
      <ErrorNotice error={error} />
      <div className="static-editor-tools">
        <Space wrap>
          <Button
            icon={<PlusOutlined />}
            onClick={() => {
              setHTMLOpen(true);
            }}
            disabled={!!busy}
          >
            增加 HTML
          </Button>
          <Button icon={<CodeOutlined />} onClick={openExtension} disabled={!!busy}>
            额外 JavaScript
          </Button>
          <Button
            icon={<UploadOutlined />}
            onClick={() => upload.current?.click()}
            loading={busy === "image"}
            disabled={!!busy && busy !== "image"}
          >
            上传图片
          </Button>
          <Button onClick={discard} disabled={!!busy}>
            重新读取
          </Button>
        </Space>
        {target.scope === "platform" && (
          <Select
            aria-label="平台模板可见范围"
            value={status}
            onChange={setStatus}
            disabled={!!busy}
            options={[
              { label: "上架", value: "published" },
              { label: "隐藏", value: "hidden" },
            ]}
          />
        )}
        {previewSignature !== JSON.stringify(files) && <Tag color="gold">有改动待更新预览</Tag>}
      </div>
      <input
        ref={upload}
        type="file"
        accept="image/png,image/jpeg,image/webp,image/gif"
        aria-label="上传模板图片"
        hidden
        onChange={(event) => void selectImage(event.target.files?.[0])}
      />
      <div className={`static-editor-grid${busy ? " is-busy" : ""}`} aria-busy={!!busy}>
        <StaticEditorSidebar
          nodes={nodes}
          files={files}
          selection={selection}
          onSelect={(id) => {
            chooseElement(id);
            setMode("select");
          }}
          onFile={setCodePath}
          onImage={(path) =>
            selected?.tag === "img"
              ? updateNode({ src: editorAssetReference(entrypoint, path) })
              : setCodePath(path)
          }
          onUpload={() => upload.current?.click()}
        />
        <StaticEditorPreview
          preview={preview}
          mode={mode}
          selected={selection}
          styleSelector={styleSelector}
          onSelect={chooseElement}
        />
        <aside className="static-editor-properties">
          <div className="static-editor-panel-title">元素属性</div>
          <StaticEditorInspector
            node={
              selected && styleSelector
                ? { ...selected, style: editorRuleDeclarations(files, styleSelector) }
                : selected
            }
            styleSelector={styleSelector}
            files={files}
            onChange={updateNode}
            onUpload={() => upload.current?.click()}
            onChooseImage={(path) => updateNode({ src: editorAssetReference(entrypoint, path) })}
          />
        </aside>
      </div>
      <StaticEditorSource
        files={files}
        codePath={codePath}
        busy={!!busy}
        onClose={() => setCodePath("")}
        onSelect={setCodePath}
        onEdit={(content) => setFiles(replaceEditorFile(files, codePath, content))}
      />
      <StaticEditorHTMLDialog
        open={htmlOpen}
        label={selected?.tag}
        onClose={() => setHTMLOpen(false)}
        onAdd={(fragment) => {
          try {
            setFiles(
              replaceEditorFile(files, entrypoint, insertEditorHTML(entry, selection, fragment)),
            );
            setHTMLOpen(false);
          } catch (problem) {
            message.error(errorMessage(problem));
          }
        }}
      />
    </section>
  );
}
