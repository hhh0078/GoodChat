/** 文件作用说明：提供静态模板元素树、图片资源和源码文件面板，核心文件只读。 */
"use client";
import { useTranslator } from "@/i18n/use-translator";
import { FileOutlined, PictureOutlined } from "@ant-design/icons";
import { Alert, Button, Drawer, Empty, Input, Modal, Space, Tabs, Tag } from "antd";
import { useEffect, useState } from "react";
import type { EditorFile, EditorNode } from "./static-editor-model";
/** StaticEditorSidebar 展示作者元素和文件列表，点击时由父组件更新选择状态。 */
export function StaticEditorSidebar({
  nodes,
  files,
  selection,
  onSelect,
  onFile,
  onImage,
  onUpload,
}: {
  nodes: EditorNode[];
  files: EditorFile[];
  selection: string;
  onSelect: (id: string) => void;
  onFile: (path: string) => void;
  onImage: (path: string) => void;
  onUpload: () => void;
}) {
  const tr = useTranslator();
  const [filter, setFilter] = useState("");
  return (
    <aside className="static-editor-sidebar">
      <Tabs
        defaultActiveKey="elements"
        items={[
          {
            key: "elements",
            label: tr("元素"),
            children: (
              <>
                <Input.Search
                  aria-label={tr("搜索源码元素")}
                  placeholder={tr("查找文字或标签")}
                  value={filter}
                  onChange={(event) => setFilter(event.target.value)}
                />
                <div className="static-editor-node-list">
                  {nodes
                    .filter((node) =>
                      `${node.tag} ${node.label}`.toLowerCase().includes(filter.toLowerCase()),
                    )
                    .map((node) => (
                      <button
                        key={node.id}
                        type="button"
                        className={selection === node.id ? "is-selected" : ""}
                        style={{ paddingLeft: Math.min(node.depth, 5) * 10 + 10 }}
                        onClick={() => {
                          onSelect(node.id);
                        }}
                        title={node.label}
                      >
                        <code>{node.tag}</code>
                        <span>{node.label}</span>
                      </button>
                    ))}
                </div>
              </>
            ),
          },
          {
            key: "files",
            label: tr("文件"),
            children: (
              <div className="static-editor-file-list">
                {files.map((file) => (
                  <button type="button" key={file.path} onClick={() => onFile(file.path)}>
                    <FileOutlined />
                    <span>{file.path}</span>
                    {file.protected && <Tag>{tr("只读")}</Tag>}
                  </button>
                ))}
              </div>
            ),
          },
          {
            key: "images",
            label: tr("图片"),
            children: (
              <div className="static-editor-assets">
                {files
                  .filter((file) => file.content_type.startsWith("image/"))
                  .map((file) => (
                    <button key={file.path} type="button" onClick={() => onImage(file.path)}>
                      <img
                        src={`data:${file.content_type};base64,${file.data_base64}`}
                        alt={file.path.split("/").at(-1)}
                      />
                      <span>{file.path.split("/").at(-1)}</span>
                    </button>
                  ))}
                <Button icon={<PictureOutlined />} onClick={onUpload}>
                  {tr("添加图片 ")}
                </Button>
              </div>
            ),
          },
        ]}
      />
    </aside>
  );
}
/** StaticEditorSource 保留源文件内容，图片只展示，官方脚本与合约禁止编辑。 */
export function StaticEditorSource({
  files,
  codePath,
  busy,
  onClose,
  onSelect,
  onEdit,
}: {
  files: EditorFile[];
  codePath: string;
  busy: boolean;
  onClose: () => void;
  onSelect: (path: string) => void;
  onEdit: (content: string) => void;
}) {
  const tr = useTranslator();
  const codeFile = files.find((file) => file.path === codePath);
  return (
    <Drawer
      rootClassName="static-editor-source-drawer"
      title={tr("模板源文件")}
      open={!!codePath}
      onClose={onClose}
      width="min(1200px, 96vw)"
      destroyOnHidden
    >
      <div className="static-editor-source-grid">
        <div className="static-editor-file-list">
          {files.map((file) => (
            <button
              type="button"
              className={file.path === codePath ? "is-selected" : ""}
              key={file.path}
              onClick={() => onSelect(file.path)}
            >
              <FileOutlined />
              <span>{file.path}</span>
              {file.protected && <Tag>{tr("只读")}</Tag>}
            </button>
          ))}
        </div>
        <div className="static-editor-source-content">
          <Space>
            <strong>{codePath}</strong>
            {codeFile?.protected && <Tag>{tr("官方核心 \u00B7 只读")}</Tag>}
          </Space>
          {typeof codeFile?.content === "string" ? (
            <Input.TextArea
              aria-label={tr("编辑源码 {p0}", { p0: String(codePath) })}
              className="code-editor"
              value={codeFile.content}
              rows={28}
              readOnly={codeFile.protected || !!busy}
              onChange={(event) => onEdit(event.target.value)}
              spellCheck={false}
            />
          ) : codeFile?.data_base64 && codeFile.content_type.startsWith("image/") ? (
            <img
              className="static-editor-source-image"
              src={`data:${codeFile.content_type};base64,${codeFile.data_base64}`}
              alt={codePath}
            />
          ) : (
            <Empty description={tr("此文件不支持文本编辑")} />
          )}
          <p className="static-editor-help">
            {tr("关闭此面板后更新预览或保存。核心 SDK、通信、合约文件保持只读。 ")}
          </p>
        </div>
      </div>
    </Drawer>
  );
}
/** StaticEditorHTMLDialog 仅编辑追加片段文本，片段由隔离预览和后端完整校验后执行。 */
export function StaticEditorHTMLDialog({
  open,
  label,
  onClose,
  onAdd,
}: {
  open: boolean;
  label?: string;
  onClose: () => void;
  onAdd: (fragment: string) => void;
}) {
  const tr = useTranslator();
  const [html, setHTML] = useState("");
  useEffect(() => {
    if (open) setHTML("");
  }, [open]);
  return (
    <Modal
      title={label ? tr("在 {p0} 元素后增加 HTML", { p0: String(label) }) : tr("增加 HTML 内容")}
      open={open}
      onCancel={onClose}
      okText={tr("加入草稿")}
      okButtonProps={{ disabled: !html.trim() }}
      onOk={() => onAdd(html)}
    >
      <Alert type="info" showIcon title={tr("增加静态界面片段，业务动作继续调用官方方法。")} />
      <Input.TextArea
        aria-label={tr("追加HTML")}
        className="code-editor"
        rows={12}
        value={html}
        onChange={(event) => setHTML(event.target.value)}
        placeholder={tr('<section class="my-notice"><h2>服务说明</h2><p>填写内容</p></section>')}
      />
    </Modal>
  );
}
