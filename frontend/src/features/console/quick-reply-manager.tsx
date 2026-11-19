/** 文件作用说明：在回复与运营中复用富话术编辑器，统一管理图片、语音、选项及个人公共范围。 */
"use client";
import { EmptyData, ErrorNotice } from "@/components/page-state";
import { QuickReplyEditor } from "@/features/chat/quick-reply-editor";
import { useTranslator } from "@/i18n/use-translator";
import type { QuickReply } from "@/lib/quick-replies";
import { useResource } from "@/lib/use-resource";
import { PlusOutlined } from "@ant-design/icons";
import { Button, Table, Tag } from "antd";
import { useState } from "react";
/** QuickReplyManager 与工作台使用相同版本化编辑入口，避免后台覆盖富内容或误传旧格式。 */
export function QuickReplyManager({ manages }: { manages: boolean }) {
  const tr = useTranslator();
  const data = useResource<QuickReply>("/tenant/quick-replies");
  const [editing, setEditing] = useState<QuickReply | null | undefined>();
  const visible = data.items.filter((item) => item.enabled !== false);
  return (
    <section className="table-card studio-table-surface">
      <div className="studio-panel-heading">
        <h2>{tr("快捷话术")}</h2>
        <p>
          {tr(
            "预先保存常用文字、图片、语音、文件或选项，在客服输入框左侧双击标题即可发送给当前客户。 ",
          )}
        </p>
      </div>
      <p className="form-hint">
        {tr(
          "个人话术仅自己可见和编辑，团队公共话术供所有客服使用、由管理员维护。工作台先显示个人，再显示公共，各自按自己成功发送的次数排序；下方\u201C已发送\u201D也只统计当前账号。 ",
        )}
      </p>
      <ErrorNotice error={data.error} retry={data.reload} />
      <Button
        type="primary"
        icon={<PlusOutlined />}
        onClick={() => setEditing(null)}
        style={{ marginBottom: 16 }}
      >
        {tr("添加快捷回复 ")}
      </Button>
      <Table
        rowKey="id"
        dataSource={visible}
        loading={data.loading}
        scroll={{ x: 650 }}
        locale={{ emptyText: <EmptyData text={tr("暂无快捷话术")} /> }}
        columns={[
          { title: tr("标题"), dataIndex: "title", width: 190 },
          {
            title: tr("内容"),
            render: (_, row) => (
              <>
                <Tag>
                  {row.options?.length
                    ? tr("选项")
                    : {
                        text: tr("文字"),
                        form: tr("表单"),
                        image: tr("图片"),
                        audio: tr("语音"),
                        file: tr("文件或视频"),
                      }[row.message_type]}
                </Tag>
                {row.content || row.attachment?.file_name || "—"}
              </>
            ),
            ellipsis: true,
          },
          {
            title: tr("范围"),
            dataIndex: "scope",
            width: 100,
            render: (scope) => (scope === "personal" ? tr("个人") : tr("团队公共")),
          },
          {
            title: tr("已发送"),
            dataIndex: "use_count",
            width: 90,
            render: (value) => Number(value || 0),
          },
          {
            title: tr("操作"),
            width: 90,
            render: (_, row) =>
              manages || row.scope === "personal" ? (
                <Button type="link" onClick={() => setEditing(row)}>
                  {tr("编辑 ")}
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
