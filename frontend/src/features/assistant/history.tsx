/** 文件作用说明：列出当前租户的真实 AI 对话，允许从任意模板页恢复消息、任务及预览。 */
"use client";
import { Button, Table, Tag } from "antd";
import { EmptyData, ErrorNotice } from "@/components/page-state";
import { useResource } from "@/lib/use-resource";
import { useAssistantLauncher } from "./launcher";
import type { AssistantThread } from "./state";

/** AssistantHistory 使用对话编号恢复全局助手，不把历史记录转为新的模型请求。 */
export function AssistantHistory() {
  const data = useResource<AssistantThread>("/tenant/assistant/threads");
  const { openAssistant } = useAssistantLauncher();
  return (
    <div className="table-card">
      <ErrorNotice error={data.error} retry={data.reload} />
      <Table
        rowKey="id"
        dataSource={data.items}
        loading={data.loading}
        locale={{ emptyText: <EmptyData text="打开右上角 AI 助手，开始第一次模板创作。" /> }}
        columns={[
          { title: "对话", dataIndex: "title" },
          {
            title: "使用配置",
            dataIndex: "source",
            render: (value) => <Tag>{value === "platform" ? "平台 AI" : "本租户 AI"}</Tag>,
          },
          {
            title: "操作",
            render: (_, row) => (
              <Button type="link" onClick={() => openAssistant({ threadId: row.id })}>
                恢复对话与预览
              </Button>
            ),
          },
        ]}
      />
    </div>
  );
}
