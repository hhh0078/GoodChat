/** 文件作用说明：核对供应商未知用量，保留审计及平台承担记录，失败任务不补扣租户。 */
"use client";
import { ErrorNotice } from "@/components/page-state";
import { useTranslator } from "@/i18n/use-translator";
import { api, errorMessage } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import { App, Button, Form, Input, InputNumber, Modal, Select, Table } from "antd";
import { useState } from "react";
import type { AIJob } from "./types";
type AuditJob = AIJob & {
  cost_micro: number;
  attempts: { call_index: number; status: string; input_tokens: number; output_tokens: number }[];
};
/** AIPlatformJobs 按指定租户加载平台审计，不把模型密钥或内部提示快照返回浏览器。 */
export function AIPlatformJobs({
  tenantId,
  tenantName,
  onClose,
}: {
  tenantId: string;
  tenantName: string;
  onClose: () => void;
}) {
  const tr = useTranslator();
  const data = useResource<AuditJob>(`/platform/ai/tenants/${tenantId}/jobs`, true);
  const [editing, setEditing] = useState<AuditJob | null>(null);
  return (
    <Modal
      open
      title={`${tr("核对记录")} · ${tenantName}`}
      width={1000}
      footer={null}
      onCancel={onClose}
    >
      <ErrorNotice error={data.error} retry={data.reload} />
      <Table
        rowKey="id"
        dataSource={data.items}
        loading={data.loading}
        scroll={{ x: 750 }}
        pagination={{ pageSize: 10 }}
        columns={[
          { title: tr("任务编号"), dataIndex: "id", ellipsis: true },
          { title: tr("输入Token"), dataIndex: "input_tokens" },
          { title: tr("输出Token"), dataIndex: "output_tokens" },
          {
            title: tr("计费状态"),
            dataIndex: "billing_status",
            render: (value: string) =>
              ({
                reported: tr("已报告"),
                not_called: tr("未调用"),
                uncertain: tr("用量待核对"),
                reconciled: tr("已核对"),
                waived_unknown_cost: tr("平台承担，成本未知"),
              })[value] || value,
          },
          {
            title: tr("操作"),
            render: (_: unknown, item: AuditJob) =>
              item.status === "uncertain" ? (
                <Button size="small" onClick={() => setEditing(item)}>
                  {tr("处理")}
                </Button>
              ) : null,
          },
        ]}
      />
      {editing && (
        <AIReconcile
          tenantId={tenantId}
          job={editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            data.reload();
          }}
        />
      )}
    </Modal>
  );
}
/** AIReconcile 接受实际供应商账单或明确承担未知成本，两者都释放租户预占。 */
function AIReconcile({
  tenantId,
  job,
  onClose,
  onSaved,
}: {
  tenantId: string;
  job: AuditJob;
  onClose: () => void;
  onSaved: () => void;
}) {
  const tr = useTranslator();
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const [key] = useState(() => crypto.randomUUID());
  const action = Form.useWatch("action", form) || "report";
  const pending = job.attempts.filter((a) => ["started", "uncertain"].includes(a.status));
  /** save 只提交人工填写的用量，不用估计值替代供应商账单。 */
  async function save() {
    try {
      const values = await form.validateFields();
      setBusy(true);
      await api(`/platform/ai/tenants/${tenantId}/jobs/${job.id}/reconcile`, {
        method: "POST",
        bodyJSON: {
          action: values.action,
          reason: values.reason,
          client_key: key,
          attempts:
            action === "report"
              ? pending.map((a) => ({
                  call_index: a.call_index,
                  input_tokens: values[`input_${a.call_index}`],
                  output_tokens: values[`output_${a.call_index}`],
                }))
              : [],
        },
      });
      onSaved();
    } catch (e) {
      if (!(e && typeof e === "object" && "errorFields" in e)) message.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      open
      title={tr("核对未知用量")}
      onCancel={busy ? undefined : onClose}
      onOk={() => void save()}
      confirmLoading={busy}
      okText={tr("确认核对")}
      cancelText={tr("取消")}
    >
      <p>
        {tr(
          "失败任务不补扣租户。填入实际用量用于平台成本核算；无法核实时可释放预占，并保留未知成本标记。",
        )}
      </p>
      <Form form={form} layout="vertical" initialValues={{ action: "report" }} disabled={busy}>
        <Form.Item name="action" label={tr("处理方式")}>
          <Select
            options={[
              { value: "report", label: tr("填写供应商实际用量") },
              { value: "waive", label: tr("平台承担并释放预占") },
            ]}
          />
        </Form.Item>
        {action === "report" &&
          pending.map((a) => (
            <div key={a.call_index} className="ai-form-grid">
              <Form.Item
                name={`input_${a.call_index}`}
                label={`${tr("输入Token")} · ${a.call_index}`}
                rules={[{ required: true }]}
              >
                <InputNumber min={0} max={1000000} precision={0} />
              </Form.Item>
              <Form.Item
                name={`output_${a.call_index}`}
                label={`${tr("输出Token")} · ${a.call_index}`}
                rules={[{ required: true }]}
              >
                <InputNumber min={0} max={1000000} precision={0} />
              </Form.Item>
            </div>
          ))}
        <Form.Item
          name="reason"
          label={tr("核对说明")}
          rules={[{ required: true, whitespace: true }]}
        >
          <Input.TextArea maxLength={500} />
        </Form.Item>
      </Form>
    </Modal>
  );
}
