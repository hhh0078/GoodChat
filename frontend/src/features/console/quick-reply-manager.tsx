/** 文件作用说明：在回复与运营中复用富话术编辑器，统一管理图片、语音、选项及个人公共范围。 */
"use client";
import { useState } from "react";
import { Button, Table, Tag } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import { ErrorNotice, EmptyData } from "@/components/page-state";
import { useResource } from "@/lib/use-resource";
import type { QuickReply } from "@/lib/quick-replies";
import { QuickReplyEditor } from "@/features/chat/quick-reply-editor";

/** QuickReplyManager 与工作台使用相同版本化编辑入口，避免后台覆盖富内容或误传旧格式。 */
export function QuickReplyManager({ manages }: { manages: boolean }) {
  const data = useResource<QuickReply>("/tenant/quick-replies");
  const [editing, setEditing] = useState<QuickReply | null | undefined>();
  const visible = data.items.filter((item) => item.enabled !== false);
  return (
    <section className="table-card studio-table-surface">
      <div className="studio-panel-heading">
        <h2>快捷话术</h2>
        <p>维护个人或公共的文字、图片、语音和选项；使用次数只统计当前成员成功发送。</p>
      </div>
      <ErrorNotice error={data.error} retry={data.reload} />
      <Button
        type="primary"
        icon={<PlusOutlined />}
        onClick={() => setEditing(null)}
        style={{ marginBottom: 16 }}
      >
        添加快捷回复
      </Button>
      <Table
        rowKey="id"
        dataSource={visible}
        loading={data.loading}
        scroll={{ x: 650 }}
        locale={{ emptyText: <EmptyData text="暂无快捷话术" /> }}
        columns={[
          { title: "标题", dataIndex: "title", width: 190 },
          {
            title: "内容",
            render: (_, row) => (
              <>
                <Tag>
                  {row.options?.length
                    ? "选项"
                    : { text: "文字", image: "图片", audio: "语音", file: "文件或视频" }[
                        row.message_type
                      ]}
                </Tag>
                {row.content || row.attachment?.file_name || "—"}
              </>
            ),
            ellipsis: true,
          },
          {
            title: "范围",
            dataIndex: "scope",
            width: 100,
            render: (scope) => (scope === "personal" ? "个人" : "团队公共"),
          },
          {
            title: "已发送",
            dataIndex: "use_count",
            width: 90,
            render: (value) => Number(value || 0),
          },
          {
            title: "操作",
            width: 90,
            render: (_, row) =>
              manages || row.scope === "personal" ? (
                <Button type="link" onClick={() => setEditing(row)}>
                  编辑
                </Button>
              ) : (
                "—"
              ),
          },
        ]}
      />
      {editing !== undefined && (
        <QuickReplyEditor
          tenantId={data.tenantId}
          manages={manages}
          items={visible}
          item={editing}
          onSelect={setEditing}
          onClose={() => setEditing(undefined)}
          onSaved={() => {
            setEditing(undefined);
            data.reload();
          }}
        />
      )}
    </section>
  );
}
