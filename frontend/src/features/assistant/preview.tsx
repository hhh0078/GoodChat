/** 文件作用说明：以独立来源 iframe 展示后端生成的模板，手机与桌面预览共用官方模拟消息。 */
"use client";
import { useTranslator } from "@/i18n/use-translator";
import { safeRenderURL } from "@/lib/frame-bridge";
import { DesktopOutlined, MobileOutlined, ReloadOutlined } from "@ant-design/icons";
import { Alert, Button, Empty, Segmented, Space, Tag } from "antd";
import { useEffect, useRef, useState } from "react";
import { previewScale } from "./preview-scale";
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
  const tr = useTranslator();
  const [device, setDevice] = useState<"mobile" | "desktop">("mobile");
  const [revision, setRevision] = useState(0);
  const [size, setSize] = useState({ width: 0, height: 0 });
  const host = useRef<HTMLDivElement | null>(null);
  const source =
    typeof window === "undefined" || !url ? null : safeRenderURL(url, window.location.origin);
  useEffect(() => {
    if (!host.current) return;
    const observer = new ResizeObserver((entries) => {
      const rect = entries[0]?.contentRect;
      setSize({ width: rect?.width || 0, height: rect?.height || 0 });
    });
    observer.observe(host.current);
    return () => observer.disconnect();
  }, []);
  const viewportWidth = device === "mobile" ? 390 : 1080;
  const viewportHeight = device === "mobile" ? 780 : 760;
  const scale = previewScale(size.width, size.height, viewportWidth, viewportHeight);
  return (
    <section className="assistant-preview" aria-label={tr("用户端模板预览")}>
      <div className="assistant-preview-toolbar">
        <div>
          <strong>{tr("用户端预览")}</strong>
          <Tag>{tr("模拟对话")}</Tag>
        </div>
        <Space size={8}>
          <Segmented
            aria-label={tr("预览设备")}
            value={device}
            onChange={(value) => setDevice(value as "mobile" | "desktop")}
            options={[
              { value: "mobile", icon: <MobileOutlined />, label: tr("手机") },
              { value: "desktop", icon: <DesktopOutlined />, label: tr("桌面") },
            ]}
          />
          <Button
            icon={<ReloadOutlined />}
            aria-label={tr("刷新模板预览")}
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
              title={tr("{p0}用户端模板模拟预览", {
                p0: String(device === "mobile" ? tr("手机") : tr("桌面")),
              })}
              src={source.href}
              sandbox="allow-scripts"
              referrerPolicy="no-referrer"
              style={{ width: viewportWidth, height: viewportHeight, transform: `scale(${scale})` }}
            />
          </div>
        ) : url ? (
          <Alert
            type="error"
            showIcon
            title={tr("预览地址未通过来源检查")}
            description={tr("请重新打开模板预览。")}
          />
        ) : (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={
              <>
                <strong>{tr("让想法变成可用的客服页面")}</strong>
                <p>{tr("预览中的消息仅在本地演示。保存模板后，可绑定接入链接。")}</p>
              </>
            }
          />
        )}
      </div>
      <div className="assistant-preview-footer">
        <span>{tr("预览中的消息仅在本地演示。保存模板后，可绑定接入链接。")}</span>
        {(pageId || templateId) && (
          <Button
            href={pageId ? `/console/pages/${pageId}` : `/console/templates/${templateId}`}
            type="link"
          >
            {pageId ? tr("编辑与发布") : tr("编辑模板")}
          </Button>
        )}
      </div>
    </section>
  );
}
