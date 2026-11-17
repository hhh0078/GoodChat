/** 文件作用说明：展示知识库成功发布的正文描述历史，并原子恢复所选版本。 */
"use client";
import { ErrorNotice } from "@/components/page-state";
import { useSession } from "@/features/auth/session";
import { useFormats } from "@/i18n/use-formats";
import { useTranslator } from "@/i18n/use-translator";
import { api, errorMessage } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import { App, Button, Modal, Popconfirm, Table } from "antd";
import { useState } from "react";
import type { AIContent } from "./types";
type KnowledgeVersion = { version: number; body: string; description: string; created_at: string };
/** AIKnowledgeHistory 恢复已生成的正文和描述，不额外调用模型或覆盖他人更新。 */
export function AIKnowledgeHistory({
  item,
  onClose,
  onRestored,
}: {
  item: AIContent;
  onClose: () => void;
  onRestored: () => void;
}) {
  const tr = useTranslator();
  const { tenantId } = useSession();
  const { message } = App.useApp();
  const { dateTimeFormatter } = useFormats();
  const data = useResource<KnowledgeVersion>(`/tenant/ai/knowledge/${item.id}/versions`);
  const [busy, setBusy] = useState(false);
  /** restore 使用编辑时的乐观版本，存在并发修改时保持原稿不动。 */
  async function restore(version: number) {
    setBusy(true);
    try {
      await api(`/tenant/ai/knowledge/${item.id}/restore`, {
        method: "POST",
        tenantId,
        bodyJSON: { version, expected_version: item.version },
      });
      onRestored();
    } catch (e) {
      message.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      open
      title={tr("历史版本")}
      width={800}
      footer={null}
      onCancel={busy ? undefined : onClose}
    >
      <ErrorNotice error={data.error} retry={data.reload} />
      <Table
        rowKey="version"
        dataSource={data.items}
        loading={data.loading}
        pagination={{ pageSize: 8 }}
        expandable={{
          expandedRowRender: (row) => <p style={{ whiteSpace: "pre-wrap" }}>{row.body}</p>,
        }}
        columns={[
          { title: tr("版本"), dataIndex: "version", width: 70 },
          { title: tr("使用描述"), dataIndex: "description" },
          {
            title: tr("时间"),
            dataIndex: "created_at",
            render: (value: string) =>
              dateTimeFormatter({ dateStyle: "short", timeStyle: "short" }).format(new Date(value)),
          },
          {
            title: tr("操作"),
            render: (_: unknown, row: KnowledgeVersion) => (
              <Popconfirm
                title={tr("恢复此版本？当前草稿将被替换。")}
                onConfirm={() => restore(row.version)}
              >
                <Button loading={busy}>{tr("恢复版本")}</Button>
              </Popconfirm>
            ),
          },
        ]}
      />
    </Modal>
  );
}
