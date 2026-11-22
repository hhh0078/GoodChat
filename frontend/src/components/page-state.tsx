/** 文件作用说明：统一资源页标题、错误、无权限与列表空状态。 */
"use client";
import { translate as tr } from "@/i18n/runtime";
import { useTranslator } from "@/i18n/use-translator";
import { showRequestError } from "@/lib/request-error";
import { useEffect, useRef } from "react";
import { Empty } from "antd";
/** PageTitle 仅为辅助阅读保留页面名称与说明，正文直接展示内容和必要操作。 */
export function PageTitle({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action?: React.ReactNode;
}) {
  return (
    <>
      <div className="sr-only">
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      {action && <div className="page-actions">{action}</div>}
    </>
  );
}
/** ErrorNotice 统一弹出需确认的错误，同一故障在轮询期间只提示一次，恢复后可再次提示。 */
export function ErrorNotice({ error, retry }: { error: unknown; retry?: () => void }) {
  const previous = useRef("");
  useEffect(() => {
    if (!error) {
      previous.current = "";
      return;
    }
    const signature = error instanceof Error ? error.message : String(error);
    if (previous.current === signature) return;
    previous.current = signature;
    showRequestError(error, retry);
  }, [error, retry]);
  return null;
}
/** EmptyData 提示资源为空，不补充虚构业务数据。 */
export function EmptyData({ text = tr("这里暂时没有记录") }: { text?: string }) {
  const tr = useTranslator();
  return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={text} />;
}
