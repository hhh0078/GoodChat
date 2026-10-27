/** 文件作用说明：维护平台租户基础状态与真实套餐分配，保留历史并要求人工开通依据。 */
"use client";
import { useRef, useState } from "react";
import {
  Alert,
  App,
  Button,
  Checkbox,
  Drawer,
  Form,
  Input,
  Select,
  Space,
  Switch,
  Table,
  Tag,
} from "antd";
import { api, errorMessage } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import type { Resource } from "@/lib/types";
import { ErrorNotice, EmptyData } from "@/components/page-state";
type Tenant = Resource & {
  status: string;
  timezone: string;
  plan?: Resource;
  usage?: { seats_used: number; seats_reserved: number; seat_limit: number };
};
/** PlatformTenants 只管理租户运营元数据，不跨租户展示聊天正文。 */
export function PlatformTenants() {
  const [createOpen, setCreateOpen] = useState(false);
  const [createError, setCreateError] = useState<unknown>(null);
  const [createForm] = Form.useForm();
  const creationKey = useRef("");
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState("");
  const tenants = useResource<Tenant>(
    `/platform/tenants?${new URLSearchParams({ q: query, status: filter })}`,
    true,
  );
  const plans = useResource<Resource>("/platform/plans", true);
  const [editing, setEditing] = useState<Tenant | null>(null);
  const [assigning, setAssigning] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [form] = Form.useForm();
  const { message } = App.useApp();
  /** open 初始化基础信息或独立的人工套餐分配表单。 */
  function open(tenant: Tenant, assign = false) {
    form.resetFields();
    setError(null);
    setAssigning(assign);
    if (!assign) form.setFieldsValue({ name: tenant.name, status: tenant.status });
    setEditing(tenant);
  }
  /** save 提交平台操作，套餐分配需人工核对真实收款或审批参考。 */
  async function save(values: Record<string, unknown>) {
    if (!editing) return;
    setBusy(true);
    setError(null);
    try {
      await api(`/platform/tenants/${editing.id}`, {
        method: "PATCH",
        bodyJSON: values,
        idempotent: true,
      });
      message.success(assigning ? "套餐分配已完成" : "租户设置已保存");
      setEditing(null);
      tenants.reload();
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  /** create 建立租户与指定所有者邀请，同一次失败重试沿用稳定幂等键。 */
  async function create(values: Record<string, unknown>) {
    setBusy(true);
    setCreateError(null);
    try {
      const result = await api<{ delivery_status: string; replayed: boolean }>(
        "/platform/tenants",
        { method: "POST", headers: { "Idempotency-Key": creationKey.current }, bodyJSON: values },
      );
      setCreateOpen(false);
      setQuery("");
      tenants.reload();
      if (result.delivery_status === "accepted")
        message.success("租户与邀请已保存，邮件服务器已受理，请等待受邀人验证邮箱激活");
      else message.warning("租户与邀请已保存，邮件结果尚未确认，请在列表核对并按需重发邀请");
    } catch (problem) {
      setCreateError(problem);
      tenants.reload();
    } finally {
      setBusy(false);
    }
  }
  /** resend 仅调用已创建租户的首次开通邀请重发，冷却与所有者状态由后端判断。 */
  async function resend(tenant: Tenant) {
    try {
      const result = await api<{ delivery_status: string }>(
        `/platform/tenants/${tenant.id}/resend-invitation`,
        { method: "POST", bodyJSON: {} },
      );
      if (result.delivery_status === "accepted")
        message.success("邀请邮件已获服务器受理，实际收件请向受邀人核对");
      else message.warning("重发请求已处理，邮件结果尚未确认");
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  return (
    <>
      <div className="resource-tools">
        <Space wrap>
          <Input.Search
            allowClear
            placeholder="租户名称或标识"
            aria-label="搜索租户"
            onSearch={setQuery}
            style={{ width: 260 }}
          />
          <Select
            allowClear
            placeholder="全部状态"
            style={{ width: 150 }}
            onChange={(value) => setFilter(value || "")}
            options={[
              { label: "正常", value: "active" },
              { label: "已停用", value: "suspended" },
              { label: "已到期", value: "expired" },
            ]}
          />
        </Space>
        <Button
          type="primary"
          onClick={() => {
            creationKey.current = crypto.randomUUID();
            createForm.resetFields();
            createForm.setFieldsValue({ timezone: "Asia/Shanghai", can_serve: false });
            setCreateError(null);
            setCreateOpen(true);
          }}
        >
          开通租户
        </Button>
      </div>
      <ErrorNotice error={tenants.error} retry={tenants.reload} />
      <Table
        rowKey="id"
        dataSource={tenants.items}
        loading={tenants.loading}
        scroll={{ x: 1000 }}
        locale={{ emptyText: <EmptyData text="没有符合条件的租户" /> }}
        columns={[
          {
            title: "租户",
            render: (_, row) => (
              <>
                <strong>{row.name}</strong>
                <p className="form-hint">{String(row.slug || row.id)}</p>
              </>
            ),
          },
          {
            title: "状态",
            dataIndex: "status",
            render: (value) => (
              <Tag color={value === "active" ? "green" : "default"}>
                {(
                  { active: "正常", suspended: "已停用", expired: "已到期" } as Record<
                    string,
                    string
                  >
                )[String(value)] || String(value)}
              </Tag>
            ),
          },
          { title: "当前有效版本", render: (_, row) => row.plan?.name || "—" },
          {
            title: "客服坐席",
            render: (_, row) =>
              row.usage
                ? `${row.usage.seats_used} 已用 + ${row.usage.seats_reserved} 预留 / ${row.usage.seat_limit}`
                : "—",
          },
          { title: "时区", dataIndex: "timezone" },
          {
            title: "操作",
            render: (_, row) => (
              <Space>
                <Button type="link" size="small" onClick={() => open(row)}>
                  管理状态
                </Button>
                <Button type="link" size="small" onClick={() => open(row, true)}>
                  分配套餐
                </Button>
                <Button type="link" size="small" onClick={() => void resend(row)}>
                  重发开通邀请
                </Button>
              </Space>
            ),
          },
        ]}
      />
      <Drawer
        title={assigning ? "人工分配套餐" : "管理租户状态"}
        open={!!editing}
        onClose={() => setEditing(null)}
        width={480}
        destroyOnHidden
      >
        <ErrorNotice error={error} />
        <p>租户：{editing?.name}</p>
        <Form form={form} layout="vertical" onFinish={save}>
          {assigning ? (
            <>
              <Alert
                type="warning"
                showIcon
                title="必须先核对实际开通依据"
                description="分配套餐会变更租户权益，本页面不代为收款。"
                style={{ marginBottom: 20 }}
              />
              <Form.Item label="目标版本" name="plan_id" rules={[{ required: true }]}>
                <Select
                  options={plans.items
                    .filter((plan) => plan.is_active)
                    .map((plan) => ({
                      label: `${plan.name} · ¥${Number(plan.price_amount) / 100}`,
                      value: plan.id,
                    }))}
                />
              </Form.Item>
              <Form.Item
                label="真实收款或审批参考"
                name="payment_reference"
                rules={[{ required: true, whitespace: true }]}
              >
                <Input.TextArea rows={3} maxLength={500} />
              </Form.Item>
              <Form.Item
                name="payment_confirmed"
                valuePropName="checked"
                rules={[
                  {
                    validator: (_, value) =>
                      value ? Promise.resolve() : Promise.reject(new Error("请先核对实际开通依据")),
                  },
                ]}
              >
                <Checkbox>我已核对申请；付费版本已确认真实收款。</Checkbox>
              </Form.Item>
            </>
          ) : (
            <>
              <Form.Item
                name="name"
                label="租户名称"
                rules={[{ required: true, whitespace: true }]}
              >
                <Input maxLength={100} />
              </Form.Item>
              <Form.Item name="status" label="运营状态" rules={[{ required: true }]}>
                <Select
                  options={[
                    { label: "正常", value: "active" },
                    { label: "停用", value: "suspended" },
                    { label: "到期", value: "expired" },
                  ]}
                />
              </Form.Item>
              <p className="form-hint">
                停用不删除聊天与资料；会影响租户当前业务访问，请核对后保存。
              </p>
            </>
          )}
          <Button type="primary" htmlType="submit" loading={busy}>
            确认保存
          </Button>
        </Form>
      </Drawer>
      <Drawer
        title="开通租户并邀请所有者"
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        width={500}
        destroyOnHidden
      >
        <ErrorNotice error={createError} />
        <Alert
          type="info"
          showIcon
          title="所有者通过邮件验证后激活"
          description="平台不会发送明文密码。邮件失败时租户可能已经保存，请先核对列表并重发原邀请。"
          style={{ marginBottom: 20 }}
        />
        <Form form={createForm} layout="vertical" onFinish={create}>
          <Form.Item name="name" label="租户名称" rules={[{ required: true, whitespace: true }]}>
            <Input maxLength={100} />
          </Form.Item>
          <Form.Item
            name="admin_email"
            label="初始所有者邮箱"
            rules={[{ required: true }, { type: "email" }]}
          >
            <Input type="email" />
          </Form.Item>
          <Form.Item
            name="admin_name"
            label="初始所有者称呼"
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={50} />
          </Form.Item>
          <Form.Item name="timezone" label="时区">
            <Input readOnly />
          </Form.Item>
          <Form.Item
            name="can_serve"
            label="所有者参与接待"
            valuePropName="checked"
            extra="默认仅管理、不占客服坐席；开启后占用免费版2席中的1席。"
          >
            <Switch />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>
            创建租户并发送激活邀请
          </Button>
        </Form>
      </Drawer>
    </>
  );
}
