/** 文件作用说明：以独立来源 iframe 展示后端生成的模板，手机与桌面预览共用官方模拟消息。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { Alert, Button, Empty, Segmented, Space, Tag } from "antd";
import { DesktopOutlined, MobileOutlined, ReloadOutlined } from "@ant-design/icons";
import { safeRenderURL } from "@/lib/frame-bridge";

/** AssistantPreview 对服务端预览地址再次检查来源，并按容器宽度缩放完整视口。 */
export function AssistantPreview({
  url,
  templateId,
  pageId,
}: {
  url?: string;
  templateId?: string;
  pageId?: string;
}) {
  const [device, setDevice] = useState<"mobile" | "desktop">("mobile");
  const [revision, setRevision] = useState(0);
  const [width, setWidth] = useState(0);
  const host = useRef<HTMLDivElement | null>(null);
  const source =
    typeof window === "undefined" || !url ? null : safeRenderURL(url, window.location.origin);
  useEffect(() => {
    if (!host.current) return;
    const observer = new ResizeObserver((entries) => setWidth(entries[0]?.contentRect.width || 0));
    observer.observe(host.current);
    return () => observer.disconnect();
  }, []);
  const viewportWidth = device === "mobile" ? 390 : 1080;
  const viewportHeight = device === "mobile" ? 780 : 760;
  const scale = Math.min(1, Math.max(0.15, (width - 32) / viewportWidth));
  return (
    <section className="assistant-preview" aria-label="用户端模板预览">
      <div className="assistant-preview-toolbar">
        <div>
          <strong>用户端预览</strong>
          <Tag>模拟对话</Tag>
        </div>
        <Space size={8}>
          <Segmented
            aria-label="预览设备"
            value={device}
            onChange={(value) => setDevice(value as "mobile" | "desktop")}
            options={[
              { value: "mobile", icon: <MobileOutlined />, label: "手机" },
              { value: "desktop", icon: <DesktopOutlined />, label: "桌面" },
            ]}
          />
          <Button
            icon={<ReloadOutlined />}
            aria-label="刷新模板预览"
            disabled={!source}
            onClick={() => setRevision((value) => value + 1)}
          />
        </Space>
      </div>
      <div className="assistant-preview-stage" ref={host}>
        {source ? (
          <div
            className={`assistant-device is-${device}`}
            style={{ width: viewportWidth * scale, height: viewportHeight * scale }}
          >
            <iframe
              key={`${source.href}:${revision}`}
              title={`${device === "mobile" ? "手机" : "桌面"}用户端模板模拟预览`}
              src={source.href}
              sandbox="allow-scripts allow-same-origin"
              referrerPolicy="no-referrer"
              style={{ width: viewportWidth, height: viewportHeight, transform: `scale(${scale})` }}
            />
          </div>
        ) : url ? (
          <Alert
            type="error"
            showIcon
            title="预览地址未通过来源检查"
            description="请刷新任务记录，或重新生成隔离预览。"
          />
        ) : (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={
              <>
                <strong>让想法变成可用的客服页面</strong>
                <p>生成后将在这里展示真实模板，自动加入模拟消息，可测试发送、选项与附件。</p>
              </>
            }
          />
        )}
      </div>
      <div className="assistant-preview-footer">
        <span>预览中的消息仅在本地演示。保存模板后，可绑定接入链接。</span>
        {(pageId || templateId) && (
          <Button
            href={pageId ? `/console/pages/${pageId}` : `/console/templates/${templateId}`}
            type="link"
          >
            {pageId ? "编辑与发布" : "编辑模板"}
          </Button>
        )}
      </div>
    </section>
  );
}
