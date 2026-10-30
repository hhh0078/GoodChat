/** 文件作用说明：在模板卡片中延迟加载真实隔离页面，按手机视口缩放并通过覆盖按钮打开完整预览。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { Button, Spin } from "antd";
import { api, errorMessage } from "@/lib/api";
import { safeRenderURL } from "@/lib/frame-bridge";

/** TemplatePreview 只为进入可视范围的卡片申请短期预览，不允许缩略页面接收指针或键盘操作。 */
export function TemplatePreview({
  path,
  name,
  tenantId,
  enabled,
  onPreview,
}: {
  path: string;
  name: string;
  tenantId: string;
  enabled: boolean;
  onPreview: () => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(false);
  const [size, setSize] = useState({ width: 0, height: 0 });
  const [source, setSource] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (!host.current) return;
    const intersection = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          setVisible(true);
          intersection.disconnect();
        }
      },
      { rootMargin: "200px" },
    );
    const resize = new ResizeObserver((entries) => {
      const rect = entries[0]?.contentRect;
      setSize({ width: rect?.width || 0, height: rect?.height || 0 });
    });
    intersection.observe(host.current);
    resize.observe(host.current);
    return () => {
      intersection.disconnect();
      resize.disconnect();
    };
  }, []);
  useEffect(() => {
    setSource("");
    setError(null);
    if (!visible || !enabled) return;
    const controller = new AbortController();
    api<{ preview_url: string }>(path, { method: "POST", tenantId, signal: controller.signal })
      .then((result) => {
        if (controller.signal.aborted) return;
        const url = safeRenderURL(result?.preview_url || "", window.location.origin);
        if (!url) throw new Error("预览地址未通过来源检查");
        setSource(url.href);
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      });
    return () => controller.abort();
  }, [path, tenantId, enabled, visible, revision]);
  const scale = Math.min(size.width / 390, size.height / 780);
  return (
    <div className="studio-template-live" ref={host}>
      {source ? (
        <iframe
          title={`${name}页面缩略预览`}
          src={source}
          sandbox="allow-scripts allow-same-origin"
          referrerPolicy="no-referrer"
          tabIndex={-1}
          aria-hidden="true"
          style={{ width: 390, height: 780, transform: `translate(-50%, -50%) scale(${scale})` }}
        />
      ) : error ? (
        <div className="studio-template-preview-state" role="status">
          <span>{errorMessage(error)}</span>
          <Button size="small" onClick={() => setRevision((value) => value + 1)}>
            重试预览
          </Button>
        </div>
      ) : (
        <div className="studio-template-preview-state">
          {enabled ? <Spin size="small" /> : <span>预览仅管理员可用</span>}
        </div>
      )}
      {enabled && !error && (
        <button
          type="button"
          className="studio-template-preview-open"
          aria-label={`${name}，预览完整页面`}
          onClick={onPreview}
        />
      )}
    </div>
  );
}
