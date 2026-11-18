/** 文件作用说明：为平台管理员配置统一客服AI模型、四项售价成本及租户余额运营。 */
"use client";
import { ErrorNotice } from "@/components/page-state";
import { useFormats } from "@/i18n/use-formats";
import { useTranslator } from "@/i18n/use-translator";
import { api, errorMessage } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import {
  App,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Space,
  Switch,
  Table,
  Tag,
} from "antd";
import { useState } from "react";
import type { AIModel } from "./types";
import { AIPlatformJobs } from "./platform-jobs";
import "./customer-ai.css";
type TenantUsage = {
  tenant_id: string;
  tenant_name: string;
  balance_micro: number;
  reserved_micro: number;
  cost_micro: number;
  revenue_micro: number;
  uncertain: number;
  unknown_cost: number;
  unlimited: boolean;
};

/** PlatformCustomerAI 管理独立客服模型配置，不改变已有装修助手配置。 */
export function PlatformCustomerAI() {
  const tr = useTranslator();
  const { message } = App.useApp();
  const { numberFormatter } = useFormats();
  const models = useResource<AIModel>("/platform/ai/models", true);
  const usage = useResource<TenantUsage>("/platform/ai/usage", true);
  const [editing, setEditing] = useState<AIModel | null>(null);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState("");
  const [credit, setCredit] = useState<TenantUsage | null>(null);
  const [audit, setAudit] = useState<TenantUsage | null>(null);
  /** money 以六位小数显示人民币，Token微额费用不丢失。 */
  function money(v: number) {
    return numberFormatter({
      style: "currency",
      currency: "CNY",
      minimumFractionDigits: 2,
      maximumFractionDigits: 6,
    }).format(v / 1000000);
  }
  /** test 实际请求模型并要求工具调用及Token用量同时通过。 */
  async function test(item: AIModel) {
    setBusy(item.id);
    try {
      const result = await api<{
        tool_calling: boolean;
        usage: { prompt_tokens: number; completion_tokens: number };
      }>(`/platform/ai/models/${item.id}/test`, { method: "POST", bodyJSON: {} });
      Modal.success({
        title: tr("模型连接与工具调用成功"),
        content: tr("输入 / 输出Token：{p0} / {p1}", {
          p0: result.usage.prompt_tokens,
          p1: result.usage.completion_tokens,
        }),
      });
    } catch (e) {
      message.error(errorMessage(e));
    } finally {
      setBusy("");
    }
  }
  /** setUnlimited 通过平台权限切换指定租户运营模式，余额与实际消费流水保持不变。 */
  async function setUnlimited(item: TenantUsage, unlimited: boolean) {
    setBusy(item.tenant_id);
    try {
      await api(`/platform/ai/wallets/${item.tenant_id}/mode`, {
        method: "PUT",
        bodyJSON: { unlimited },
      });
      usage.reload();
    } catch (problem) {
      message.error(errorMessage(problem));
    } finally {
      setBusy("");
    }
  }
  return (
    <div className="ai-resource">
      <ErrorNotice
        error={models.error || usage.error}
        retry={() => {
          models.reload();
          usage.reload();
        }}
      />
      <Card
        title={tr("客服AI模型与价格")}
        extra={
          <Button
            type="primary"
            onClick={() => {
              setEditing(null);
              setOpen(true);
            }}
          >
            {tr("新增模型")}
          </Button>
        }
      >
        <p className="ai-help">
          {tr(
            "价格单位为人民币元 / 百万Token。密钥仅保存在服务端；修改价格生成新版本。连接测试会实际调用供应商。",
          )}
        </p>
        <Table
          rowKey="id"
          dataSource={models.items}
          loading={models.loading}
          scroll={{ x: 850 }}
          pagination={false}
          columns={[
            { title: tr("名称"), dataIndex: "name" },
            { title: tr("模型ID"), dataIndex: "model" },
            {
              title: tr("状态"),
              dataIndex: "enabled",
              render: (value: boolean) => <Tag>{value ? tr("启用") : tr("停用")}</Tag>,
            },
            {
              title: tr("成本输入 / 输出"),
              render: (_: unknown, item: AIModel) =>
                `${money(item.cost_input)} / ${money(item.cost_output)}`,
            },
            {
              title: tr("售卖输入 / 输出"),
              render: (_: unknown, item: AIModel) =>
                `${money(item.sale_input)} / ${money(item.sale_output)}`,
            },
            {
              title: tr("操作"),
              render: (_: unknown, item: AIModel) => (
                <Space>
                  <Button
                    size="small"
                    onClick={() => {
                      setEditing(item);
                      setOpen(true);
                    }}
                  >
                    {tr("编辑")}
                  </Button>
                  <Button
                    size="small"
                    loading={busy === item.id}
                    disabled={!item.enabled}
                    onClick={() => void test(item)}
                  >
                    {tr("测试连接")}
                  </Button>
                </Space>
              ),
            },
          ]}
        />
      </Card>
      <Card title={tr("租户AI账目")} extra={<Button onClick={usage.reload}>{tr("刷新")}</Button>}>
        <p className="ai-help">
          {tr("毛利为已入账收入减已报告成本；存在未知成本的记录需核对，不能视为最终利润。")}
        </p>
        <Table
          rowKey="tenant_id"
          dataSource={usage.items}
          loading={usage.loading}
          scroll={{ x: 1050 }}
          pagination={{ pageSize: 10 }}
          columns={[
            { title: tr("租户"), dataIndex: "tenant_name" },
            {
              title: tr("不限制余额"),
              render: (_: unknown, item: TenantUsage) => (
                <Switch
                  checked={item.unlimited}
                  loading={busy === item.tenant_id}
                  onChange={(value) => void setUnlimited(item, value)}
                />
              ),
            },
            { title: tr("余额"), dataIndex: "balance_micro", render: money },
            { title: tr("预算预占"), dataIndex: "reserved_micro", render: money },
            { title: tr("已报告成本"), dataIndex: "cost_micro", render: money },
            { title: tr("已入账收入"), dataIndex: "revenue_micro", render: money },
            {
              title: tr("毛利"),
              render: (_: unknown, item: TenantUsage) =>
                money(item.revenue_micro - item.cost_micro),
            },
            { title: tr("用量待核对"), dataIndex: "uncertain" },
            { title: tr("未知成本记录"), dataIndex: "unknown_cost" },
            {
              title: tr("操作"),
              render: (_: unknown, item: TenantUsage) => (
                <Space>
                  <Button size="small" onClick={() => setCredit(item)}>
                    {tr("充值")}
                  </Button>
                  <Button size="small" onClick={() => setAudit(item)}>
                    {tr("核对记录")}
                  </Button>
                </Space>
              ),
            },
          ]}
        />
      </Card>
      {open && (
        <AIModelEditor
          item={editing}
          onClose={() => setOpen(false)}
          onSaved={() => {
            setOpen(false);
            models.reload();
          }}
        />
      )}
      {credit && (
        <AICredit
          tenant={credit}
          onClose={() => setCredit(null)}
          onSaved={() => {
            setCredit(null);
            usage.reload();
          }}
        />
      )}
      {audit && (
        <AIPlatformJobs
          tenantId={audit.tenant_id}
          tenantName={audit.tenant_name}
          onClose={() => {
            setAudit(null);
            usage.reload();
          }}
        />
      )}
    </div>
  );
}

/** AIModelEditor 将界面人民币费率转换为服务器整数微元费率。 */
function AIModelEditor({
  item,
  onClose,
  onSaved,
}: {
  item: AIModel | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const tr = useTranslator();
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const prices = ["cost_input", "cost_output", "sale_input", "sale_output"] as const;
  const initial: Record<string, unknown> = {
    name: "",
    base_url: "",
    model: "",
    api_key: "",
    enabled: false,
    supports_images: false,
    max_input_tokens: 24000,
    max_output_tokens: 1024,
    ...item,
  };
  for (const key of prices) initial[key] = (item?.[key] || 0) / 1000000;
  const labels = {
    cost_input: tr("成本输入价格"),
    cost_output: tr("成本输出价格"),
    sale_input: tr("售卖输入价格"),
    sale_output: tr("售卖输出价格"),
  };
  /** save 不回显旧密钥，留空时仅在服务器沿用已存加密密钥。 */
  async function save() {
    try {
      const values = await form.validateFields();
      setBusy(true);
      for (const key of prices) values[key] = Math.round(values[key] * 1000000);
      await api(`/platform/ai/models${item ? `/${item.id}` : ""}`, {
        method: item ? "PATCH" : "POST",
        bodyJSON: { ...values, version: item?.version || 0 },
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
      title={tr("客服AI模型配置")}
      width={700}
      onCancel={busy ? undefined : onClose}
      onOk={() => void save()}
      confirmLoading={busy}
      okText={tr("保存")}
      cancelText={tr("取消")}
    >
      <Form form={form} layout="vertical" initialValues={initial} disabled={busy}>
        <Form.Item name="name" label={tr("名称")} rules={[{ required: true, whitespace: true }]}>
          <Input maxLength={100} />
        </Form.Item>
        <Form.Item
          name="base_url"
          label={tr("接口地址")}
          extra={tr("填写OpenAI兼容接口基础地址，例如 https://example.com/v1。")}
          rules={[{ required: true, whitespace: true }]}
        >
          <Input autoComplete="off" />
        </Form.Item>
        <Form.Item name="model" label={tr("模型ID")} rules={[{ required: true, whitespace: true }]}>
          <Input />
        </Form.Item>
        <Form.Item
          name="api_key"
          label="API Key"
          rules={[{ required: !item?.has_api_key }]}
          extra={item?.has_api_key ? tr("已保存密钥，留空保持原值。") : undefined}
        >
          <Input.Password autoComplete="new-password" />
        </Form.Item>
        <div className="ai-form-grid">
          {prices.map((key) => (
            <Form.Item
              key={key}
              name={key}
              label={labels[key]}
              extra={tr("元 / 百万Token")}
              rules={[{ required: true }]}
            >
              <InputNumber min={0} max={1000000} precision={6} />
            </Form.Item>
          ))}
        </div>
        <div className="ai-form-grid">
          <Form.Item
            name="max_input_tokens"
            label={tr("单次最大输入Token")}
            rules={[{ required: true }]}
          >
            <InputNumber min={4096} max={64000} />
          </Form.Item>
          <Form.Item
            name="max_output_tokens"
            label={tr("单次最大输出Token")}
            rules={[{ required: true }]}
          >
            <InputNumber min={128} max={4096} />
          </Form.Item>
        </div>
        <Form.Item name="supports_images" label={tr("支持访客图片识别")} valuePropName="checked">
          <Switch />
        </Form.Item>
        <Form.Item name="enabled" label={tr("启用")} valuePropName="checked">
          <Switch />
        </Form.Item>
      </Form>
    </Modal>
  );
}

/** AICredit 保留同一弹窗的幂等编号，网络重试不会重复充值。 */
function AICredit({
  tenant,
  onClose,
  onSaved,
}: {
  tenant: TenantUsage;
  onClose: () => void;
  onSaved: () => void;
}) {
  const tr = useTranslator();
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const [key] = useState(() => crypto.randomUUID());
  /** save 创建带原因和平台操作人记录的真实余额流水。 */
  async function save() {
    try {
      const values = await form.validateFields();
      setBusy(true);
      await api(`/platform/ai/wallets/${tenant.tenant_id}/credit`, {
        method: "POST",
        bodyJSON: {
          amount_micro: Math.round(values.amount * 1000000),
          reason: values.reason,
          client_key: key,
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
      title={`${tr("AI余额充值")} · ${tenant.tenant_name}`}
      onCancel={busy ? undefined : onClose}
      onOk={() => void save()}
      confirmLoading={busy}
      okText={tr("确认入账")}
      cancelText={tr("取消")}
    >
      <Form form={form} layout="vertical" disabled={busy}>
        <Form.Item name="amount" label={tr("金额（元）")} rules={[{ required: true }]}>
          <InputNumber min={0.000001} max={1000000} precision={6} />
        </Form.Item>
        <Form.Item
          name="reason"
          label={tr("充值说明")}
          rules={[{ required: true, whitespace: true }]}
        >
          <Input.TextArea maxLength={500} />
        </Form.Item>
      </Form>
    </Modal>
  );
}
