/** 文件作用说明：统一资源页标题、错误、无权限与列表空状态。 */
"use client";
import { Alert, Button, Empty } from "antd";
import { errorMessage } from "@/lib/api";
/** PageTitle 显示页面说明和唯一主要动作。 */
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
    <div className="page-title-row">
      <div>
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      {action}
    </div>
  );
}
/** ErrorNotice 展示真实请求错误，并按需提供重试。 */
export function ErrorNotice({ error, retry }: { error: unknown; retry?: () => void }) {
  if (!error) return null;
  return (
    <Alert
      className="resource-error"
      type="error"
      showIcon
      title={errorMessage(error)}
      action={
        retry && (
          <Button size="small" onClick={retry}>
            重试
          </Button>
        )
      }
    />
  );
}
/** EmptyData 提示资源为空，不补充虚构业务数据。 */
export function EmptyData({ text = "这里暂时没有记录" }: { text?: string }) {
  return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={text} />;
}
