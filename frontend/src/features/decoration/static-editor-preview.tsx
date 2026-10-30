/** 文件作用说明：通过严格校验的隔离预览桥接实现静态元素选择，模板无法获得管理端凭证。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { Alert, Empty, Segmented, Space, Tag } from "antd";
import { DesktopOutlined, MobileOutlined } from "@ant-design/icons";
import { safeRenderURL } from "@/lib/frame-bridge";
import { previewScale } from "@/features/assistant/preview-scale";
import { editorStyleSelectors } from "./static-editor-styles";

export type EditorPreview = { preview_url: string; release_id: string; nonce: string };

/** StaticEditorPreview 按来源、窗口、版本和随机数校验选择消息，缩放只影响展示尺寸。 */
export function StaticEditorPreview({
  preview,
  mode,
  selected,
  styleSelector,
  onSelect,
}: {
  preview?: EditorPreview;
  mode: "select" | "interact";
  selected: string;
  styleSelector?: string;
  onSelect: (id: string, selector?: string) => void;
}) {
  const frame = useRef<HTMLIFrameElement>(null);
  const host = useRef<HTMLDivElement>(null);
  const [device, setDevice] = useState("mobile");
  const [size, setSize] = useState({ width: 0, height: 0 });
  const source =
    typeof window !== "undefined" && preview
      ? safeRenderURL(preview.preview_url, window.location.origin)
      : null;
  const origin = source?.origin;
  const src = source?.href;
  useEffect(() => {
    if (!host.current) return;
    const observer = new ResizeObserver(([entry]) =>
      setSize({ width: entry?.contentRect.width || 0, height: entry?.contentRect.height || 0 }),
    );
    observer.observe(host.current);
    return () => observer.disconnect();
  }, []);
  /** postMode 仅向当前隔离预览发送选择状态，不发送源码或身份信息。 */
  function postMode() {
    if (!origin || !preview) return;
    frame.current?.contentWindow?.postMessage(
      {
        type: "goodchat:editor-mode",
        release_id: preview.release_id,
        nonce: preview.nonce,
        mode,
        element_id: selected,
        style_selector: styleSelector,
      },
      origin,
    );
    frame.current?.contentWindow?.postMessage(
      {
        type: "goodchat:editor-highlight",
        release_id: preview.release_id,
        nonce: preview.nonce,
        element_id: selected,
        style_selector: styleSelector,
      },
      origin,
    );
  }
  useEffect(() => {
    if (!preview || !origin) return;
    /** receive 仅接受当前 iframe 的短期编辑事件，限制字段类型与长度。 */
    function receive(event: MessageEvent) {
      const data = event.data;
      if (
        event.source !== frame.current?.contentWindow ||
        event.origin !== origin ||
        !data ||
        data.release_id !== preview!.release_id ||
        data.nonce !== preview!.nonce
      )
        return;
      if (data.type === "goodchat:editor-ready") postMode();
      if (
        data.type === "goodchat:editor-select" &&
        mode === "select" &&
        typeof data.element_id === "string" &&
        /^[a-zA-Z][a-zA-Z0-9_-]{0,90}$/.test(data.element_id)
      )
        onSelect(
          data.element_id,
          typeof data.style_selector === "string" && editorStyleSelectors.has(data.style_selector)
            ? data.style_selector
            : undefined,
        );
    }
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, [origin, preview?.release_id, preview?.nonce, mode, selected, styleSelector, onSelect]);
  useEffect(() => {
    postMode();
  }, [mode, selected, styleSelector, src]);
  const width = device === "mobile" ? 390 : 1280;
  const height = device === "mobile" ? 844 : 800;
  const scale = previewScale(size.width, size.height, width, height);
  return (
    <section className="static-editor-preview" aria-label="元素编辑预览">
      <div className="static-editor-preview-toolbar">
        <Space size={6}>
          <strong>{mode === "select" ? "点击页面选择元素" : "交互预览"}</strong>
          <Tag>模拟消息</Tag>
        </Space>
        <Segmented
          aria-label="编辑预览设备"
          value={device}
          onChange={setDevice}
          options={[
            { value: "mobile", label: "手机", icon: <MobileOutlined /> },
            { value: "desktop", label: "电脑", icon: <DesktopOutlined /> },
          ]}
        />
      </div>
      <div className="static-editor-canvas" ref={host}>
        {source && preview ? (
          <div
            className={`static-editor-device is-${device}`}
            style={{ width: width * scale, height: height * scale }}
          >
            <iframe
              ref={frame}
              title="模板元素编辑预览"
              src={source.href}
              sandbox="allow-scripts allow-same-origin"
              referrerPolicy="no-referrer"
              onLoad={postMode}
              style={{ width, height, transform: `scale(${scale})` }}
            />
          </div>
        ) : preview ? (
          <Alert type="error" title="预览来源校验失败，请重新准备预览。" />
        ) : (
          <Empty description="正在准备可点选的模板预览" image={Empty.PRESENTED_IMAGE_SIMPLE} />
        )}
      </div>
      <p className="static-editor-caption">
        {mode === "select"
          ? "选中后在右侧修改。更新预览后显示最新改动。"
          : "可测试消息、附件与留言，所有预览消息仅在本地模拟。"}
      </p>
    </section>
  );
}
