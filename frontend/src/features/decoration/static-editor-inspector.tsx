/** 文件作用说明：为静态源码中的选中元素提供文字、图片与 CSS 编辑，不改动核心动作标识。 */
"use client";
import { Alert, Button, Empty, Input, Space, Tag } from "antd";
import { PictureOutlined, UploadOutlined } from "@ant-design/icons";
import type { EditorFile, EditorNode } from "./static-editor-model";

/** StaticEditorInspector 只报告界面字段变化，由父组件更新原始 HTML 文件。 */
export function StaticEditorInspector({
  node,
  styleSelector,
  files,
  onChange,
  onUpload,
  onChooseImage,
}: {
  node?: EditorNode;
  styleSelector?: string;
  files: EditorFile[];
  onChange: (
    patch: Partial<
      Pick<EditorNode, "text" | "style" | "className" | "src" | "alt" | "placeholder">
    >,
  ) => void;
  onUpload: () => void;
  onChooseImage: (path: string) => void;
}) {
  if (!node)
    return (
      <div className="static-editor-inspector">
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description="点击预览中的元素，或从左侧元素树选择"
        />
      </div>
    );
  const images = files.filter((file) => file.content_type.startsWith("image/") && file.data_base64);
  return (
    <div className="static-editor-inspector" aria-label="选中元素属性">
      <div className="static-editor-selected">
        <Tag>{node.tag}</Tag>
        <code>{node.id}</code>
      </div>
      {styleSelector ? (
        <Alert
          type="info"
          showIcon
          title="聊天动态元素仅编辑样式，模拟内容不会保存。"
          description={<code>{styleSelector}</code>}
        />
      ) : (
        node.core && <Alert type="info" showIcon title="核心节点可改样式，功能标识由系统保留。" />
      )}
      {!styleSelector &&
        (node.leaf && !["input", "textarea", "img"].includes(node.tag) ? (
          <label>
            文字内容
            <Input.TextArea
              aria-label="元素文字"
              rows={3}
              value={node.text}
              onChange={(event) => onChange({ text: event.target.value })}
            />
          </label>
        ) : (
          node.tag !== "img" && (
            <p className="static-editor-help">此元素包含子元素，选择内部文字元素即可修改文案。</p>
          )
        ))}
      {!styleSelector && ["input", "textarea"].includes(node.tag) && (
        <label>
          输入提示
          <Input
            aria-label="元素输入提示"
            value={node.placeholder}
            onChange={(event) => onChange({ placeholder: event.target.value })}
          />
        </label>
      )}
      {!styleSelector && node.tag === "img" && (
        <>
          <label>
            图片路径
            <Input
              aria-label="元素图片路径"
              value={node.src.startsWith("data:") ? "内嵌图片（可上传替换）" : node.src}
              readOnly
            />
          </label>
          <label>
            图片说明
            <Input
              aria-label="元素图片说明"
              value={node.alt}
              onChange={(event) => onChange({ alt: event.target.value })}
            />
          </label>
          <Button icon={<UploadOutlined />} onClick={onUpload}>
            上传并替换图片
          </Button>
          <div className="static-editor-image-picker">
            {images.map((file) => (
              <button
                type="button"
                key={file.path}
                title={file.path}
                aria-label={`使用图片 ${file.path}`}
                onClick={() => onChooseImage(file.path)}
              >
                <img
                  src={`data:${file.content_type};base64,${file.data_base64}`}
                  alt={file.path.split("/").at(-1)}
                />
              </button>
            ))}
          </div>
        </>
      )}
      {!styleSelector && (
        <label>
          CSS 类名
          <Input
            aria-label="元素CSS类名"
            value={node.className}
            onChange={(event) => onChange({ className: event.target.value })}
          />
        </label>
      )}
      <label>
        此元素的 CSS
        <Input.TextArea
          className="code-editor"
          aria-label="元素CSS"
          value={node.style}
          rows={10}
          placeholder={"color: #234e70;\nfont-size: 24px;\npadding: 16px;"}
          onChange={(event) => onChange({ style: event.target.value })}
        />
      </label>
      <p className="static-editor-help">
        输入 CSS 声明，优先作用于当前元素。响应式规则可在 CSS 源文件中修改。
      </p>
      <Space>
        <PictureOutlined />
        <span className="static-editor-help">上传图片保存在模板静态目录</span>
      </Space>
    </div>
  );
}
