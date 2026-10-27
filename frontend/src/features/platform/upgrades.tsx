/** 文件作用说明：审核真实套餐升级申请，平台确认收款依据后才调用订阅开通接口。 */
"use client";
import { useState } from "react";
import { Alert, App, Button, Checkbox, Drawer, Form, Input, Table, Tag } from "antd";
import { api } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import type { Resource } from "@/lib/types";
import { EmptyData, ErrorNotice } from "@/components/page-state";
/** PlatformUpgrades 展示待处理升级并显式要求核对真实收款，界面不执行或模拟支付。 */
export function PlatformUpgrades() {
  const requests = useResource<Resource>("/platform/upgrade-requests", true);
  const plans = useResource<Resource>("/platform/plans", true);
  const [selected, setSelected] = useState<Resource | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [form] = Form.useForm();
  const { message } = App.useApp();
  /** activate 提交审核确认与真实参考信息，接口成功后才刷新开通结果。 */
  async function activate(values: Record<string, unknown>) {
    if (!selected) return;
    setBusy(true);
    setError(null);
    try {
      await api(`/platform/upgrade-requests/${selected.id}/activate`, {
        method: "POST",
        bodyJSON: values,
        idempotent: true,
      });
      message.success("订阅已按服务端结果开通");
      setSelected(null);
      requests.reload();
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="studio-platform-section">
      <div className="studio-panel-heading">
        <h2>升级申请与人工开通</h2>
        <p>核对申请依据后开通订阅。</p>
      </div>
      <Alert
        type="info"
        showIcon
        title="开通需要平台人工核对"
        description="此处不调用支付渠道。请先核对实际收款和开通依据，再处理申请。"
        style={{ marginBottom: 20 }}
      />
      <ErrorNotice error={requests.error} retry={requests.reload} />
      <Table
        rowKey="id"
        dataSource={requests.items}
        loading={requests.loading}
        scroll={{ x: 800 }}
        locale={{ emptyText: <EmptyData text="暂时没有开通申请" /> }}
        columns={[
          { title: "租户", render: (_, row) => String(row.tenant_name || row.tenant_id || "—") },
          {
            title: "申请版本",
            render: (_, row) =>
              String(
                (row.plan_snapshot as Resource | undefined)?.name ||
                  row.plan_name ||
                  plans.items.find((plan) => plan.id === row.plan_id)?.name ||
                  row.plan_id ||
                  "—",
              ),
          },
          {
            title: "状态",
            dataIndex: "status",
            render: (value) => (
              <Tag>
                {(
                  { pending: "待审核", activated: "已开通", rejected: "已拒绝" } as Record<
                    string,
                    string
                  >
                )[String(value)] || String(value)}
              </Tag>
            ),
          },
          {
            title: "申请时间",
            dataIndex: "created_at",
            render: (value) => new Date(String(value)).toLocaleString("zh-CN"),
          },
          {
            title: "操作",
            render: (_, row) => (
              <Button
                type="link"
                disabled={row.status !== "pending"}
                onClick={() => {
                  form.resetFields();
                  setError(null);
                  setSelected(row);
                }}
              >
                审核开通
              </Button>
            ),
          },
        ]}
      />
      <Drawer
        rootClassName="studio-drawer"
        title="核对申请并开通订阅"
        open={!!selected}
        onClose={() => setSelected(null)}
        width={480}
      >
        <ErrorNotice error={error} />
        <p>申请编号：{selected?.id}</p>
        <p>请与真实收款或已批准免费开通的业务记录逐项核对。</p>
        <Form form={form} layout="vertical" onFinish={activate}>
          <Form.Item
            label="真实收款或审批参考"
            name="payment_reference"
            rules={[{ required: true, whitespace: true, message: "请填写实际参考记录" }]}
          >
            <Input.TextArea
              rows={3}
              maxLength={500}
              placeholder="填写实际订单、转账或审批记录编号"
            />
          </Form.Item>
          <Form.Item
            name="payment_confirmed"
            valuePropName="checked"
            rules={[
              {
                validator: (_, value) =>
                  value ? Promise.resolve() : Promise.reject(new Error("请先核对真实收款与申请")),
              },
            ]}
          >
            <Checkbox>我已核对开通申请；如为付费版本，已确认真实收款。</Checkbox>
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>
            确认并开通
          </Button>
        </Form>
      </Drawer>
    </section>
  );
}
