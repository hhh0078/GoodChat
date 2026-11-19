/** 文件作用说明：为管理卡片列表提供触底加载、状态提示及键盘可用的加载与重试入口。 */
"use client";
import { useEffect, useRef } from "react";
import { Button, Spin } from "antd";
import { useTranslator } from "@/i18n/use-translator";

/** LoadMore 在接近页底时追加一页，失败后停止自动请求并保留主动重试。 */
export function LoadMore({
  loading,
  hasMore,
  error,
  onLoad,
}: {
  loading: boolean;
  hasMore: boolean;
  error: unknown;
  onLoad: () => void;
}) {
  const tr = useTranslator();
  const end = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!end.current || loading || !hasMore || error || typeof IntersectionObserver === "undefined")
      return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) onLoad();
      },
      { rootMargin: "160px" },
    );
    observer.observe(end.current);
    return () => observer.disconnect();
  }, [loading, hasMore, error, onLoad]);
  return (
    <div ref={end} className="record-list-end" aria-live="polite" aria-busy={loading}>
      {loading ? (
        <Spin size="small" />
      ) : error ? (
        <Button onClick={onLoad}>{tr("重试")}</Button>
      ) : hasMore ? (
        <Button type="text" onClick={onLoad}>
          {tr("加载更多")}
        </Button>
      ) : (
        <span>{tr("已全部加载")}</span>
      )}
    </div>
  );
}
