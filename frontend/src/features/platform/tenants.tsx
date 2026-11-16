/** 文件作用说明：维护平台租户基础状态与真实套餐分配，保留历史并要求人工开通依据。 */
"use client";
import filterStyles from "@/components/filter-toolbar.module.css";
import { EmptyData, ErrorNotice } from "@/components/page-state";
import { useFormats } from "@/i18n/use-formats";
import { useTranslator } from "@/i18n/use-translator";
import { api, errorMessage } from "@/lib/api";
import type { Resource } from "@/lib/types";
import { useResource } from "@/lib/use-resource";
import {
  AppstoreOutlined,
  FileTextOutlined,
  InfoCircleOutlined,
  TeamOutlined,
} from "@ant-design/icons";
import {
  Alert,
  App,
  Avatar,
  Button,
  Checkbox,
  Drawer,
  Form,
  Input,
  Select,
  Skeleton,
  Space,
  Switch,
  Table,
  Tag,
} from "antd";
import { useEffect, useRef, useState } from "react";
type Tenant = Resource & {
  status: string;
  timezone: string;
  plan?: Resource;
  usage?: {
    seats_used: number;
    seats_reserved: number;
    seat_limit: number;
  };
};
/** PlatformTenants 只管理租户运营元数据，不跨租户展示聊天正文。 */
export function PlatformTenants({ onManagePlans }: { onManagePlans?: () => void }) {
  const { currentPreferences, numberFormatter } = useFormats();

  const tr = useTranslator();
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
  const requests = useResource<Resource>("/platform/upgrade-requests", true);
  const [overview, setOverview] = useState<{
    tenants: number;
  } | null>(null);
  const [overviewError, setOverviewError] = useState<unknown>(null);
  const [summaryRevision, setSummaryRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setOverview(null);
    setOverviewError(null);
    api<{
      tenants: number;
    }>("/platform/overview", { signal: controller.signal })
      .then((result) => {
        if (!controller.signal.aborted) setOverview(result);
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setOverviewError(problem);
      });
    return () => controller.abort();
  }, [tenants.items, summaryRevision]);
  const freePlan = plans.items.find((plan) => plan.code === "free");
  /** refreshSummary 重新读取平台真实规模、公开套餐与待处理申请，不以失败结果填零。 */
  function refreshSummary() {
    setSummaryRevision((value) => value + 1);
    plans.reload();
    requests.reload();
  }
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
      message.success(assigning ? tr("套餐分配已完成") : tr("租户设置已保存"));
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
      const result = await api<{
        delivery_status: string;
        replayed: boolean;
      }>("/platform/tenants", {
        method: "POST",
        headers: { "Idempotency-Key": creationKey.current },
        bodyJSON: values,
      });
      setCreateOpen(false);
      setQuery("");
      tenants.reload();
      if (result.delivery_status === "accepted")
        message.success(tr("租户与邀请已保存，邮件服务器已受理，请等待受邀人验证邮箱激活"));
      else message.warning(tr("租户与邀请已保存，邮件结果尚未确认，请在列表核对并按需重发邀请"));
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
      const result = await api<{
        delivery_status: string;
      }>(`/platform/tenants/${tenant.id}/resend-invitation`, { method: "POST", bodyJSON: {} });
      if (result.delivery_status === "accepted")
        message.success(tr("邀请邮件已获服务器受理，实际收件请向受邀人核对"));
      else message.warning(tr("重发请求已处理，邮件结果尚未确认"));
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  return (
    <div className="studio-platform-tenants">
      <div className="studio-platform-summary">
        <div className="studio-tenant-summary">
          {[
            {
              title: tr("租户总数"),
              icon: TeamOutlined,
              value: overview?.tenants,
              loading: !overview && !overviewError,
              error: overviewError,
            },
            {
              title: tr("公开套餐"),
              icon: AppstoreOutlined,
              value: plans.items.filter((plan) => plan.is_active && plan.is_public).length,
              loading: plans.loading,
              error: plans.error,
            },
            {
              title: tr("待处理申请"),
              icon: FileTextOutlined,
              value: requests.items.filter((request) => request.status === "pending").length,
              loading: requests.loading,
              error: requests.error,
            },
          ].map((metric) => (
            <div key={metric.title}>
              <metric.icon aria-hidden="true" />
              <div>
                <span>{metric.title}</span>
                <strong>
                  {metric.loading ? (
                    <Skeleton.Input active size="small" />
                  ) : metric.error ? (
                    "—"
                  ) : (
                    (metric.value ?? "—")
                  )}
                </strong>
              </div>
            </div>
          ))}
        </div>
        <ErrorNotice
          error={overviewError || plans.error || requests.error}
          retry={refreshSummary}
        />
      </div>
      <section className="studio-platform-section">
        <div className="studio-panel-heading">
          <h2>{tr("租户运营")}</h2>
          <p>{tr("管理租户状态、有效套餐与开通邀请。")}</p>
        </div>
        <div className={filterStyles.toolbar}>
          <Input.Search
            className={filterStyles.search}
            allowClear
            placeholder={tr("名称 / 标识")}
            aria-label={tr("租户名称或标识")}
            onSearch={setQuery}
            style={{ width: 200 }}
          />
          <div className={filterStyles.field} style={{ width: 130 }}>
            <span>{tr("状态")}</span>
            <Select
              aria-label={tr("租户状态")}
              variant="borderless"
              allowClear
              placeholder={tr("全部")}
              onChange={(value) => setFilter(value || "")}
              options={[
                { label: tr("正常"), value: "active" },
                { label: tr("已停用"), value: "suspended" },
                { label: tr("已到期"), value: "expired" },
              ]}
            />
          </div>
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
            {tr("开通租户 ")}
          </Button>
        </div>
        <ErrorNotice error={tenants.error} retry={tenants.reload} />
        <Table
          rowKey="id"
          dataSource={tenants.items}
          loading={tenants.loading}
          scroll={{ x: 1000 }}
          locale={{ emptyText: <EmptyData text={tr("没有符合条件的租户")} /> }}
          columns={[
            {
              title: tr("租户"),
              render: (_, row) => (
                <div className="studio-tenant-identity">
                  <Avatar shape="square" size={40}>
                    {String(row.name || row.slug || tr("租")).slice(0, 1)}
                  </Avatar>
                  <div>
                    <strong>{row.name}</strong>
                    <p className="form-hint" title={String(row.slug || row.id)}>
                      {String(row.slug || row.id)}
                    </p>
                  </div>
                </div>
              ),
            },
            {
              title: tr("状态"),
              dataIndex: "status",
              render: (value) => (
                <Tag color={value === "active" ? "blue" : "default"}>
                  {(
                    {
                      active: tr("正常"),
                      suspended: tr("已停用"),
                      expired: tr("已到期"),
                    } as Record<string, string>
                  )[String(value)] || String(value)}
                </Tag>
              ),
            },
            { title: tr("当前有效版本"), render: (_, row) => row.plan?.name || "—" },
            {
              title: tr("客服坐席"),
              render: (_, row) =>
                row.usage
                  ? tr("{p0} 已用 + {p1} 预留 / {p2}", {
                      p0: String(row.usage.seats_used),
                      p1: String(row.usage.seats_reserved),
                      p2: String(row.usage.seat_limit),
                    })
                  : "—",
            },
            { title: tr("时区"), dataIndex: "timezone" },
            {
              title: tr("操作"),
              render: (_, row) => (
                <Space>
                  <Button type="link" size="small" onClick={() => open(row)}>
                    {tr("管理状态 ")}
                  </Button>
                  <Button type="link" size="small" onClick={() => open(row, true)}>
                    {tr("分配套餐 ")}
                  </Button>
                  <Button type="link" size="small" onClick={() => void resend(row)}>
                    {tr("重发开通邀请 ")}
                  </Button>
                </Space>
              ),
            },
          ]}
        />
        {freePlan && !plans.error && (
          <div className="studio-platform-rule">
            <InfoCircleOutlined aria-hidden="true" />
            <span>
              {String(freePlan.name)}：¥
              {(Number(freePlan.price_amount) / 100).toLocaleString(
                currentPreferences().locale,
              )} · {Number(freePlan.seat_limit)}
              {tr(" 个客服坐席 \u00B7")}{" "}
              {Number(freePlan.validity_days) === 0
                ? tr("长期有效")
                : tr("{p0} 天", { p0: String(Number(freePlan.validity_days)) })}
            </span>
            <Button type="link" onClick={onManagePlans} disabled={!onManagePlans}>
              {tr("管理套餐规则 ")}
            </Button>
          </div>
        )}
        <Drawer
          rootClassName="studio-drawer"
          title={assigning ? tr("人工分配套餐") : tr("管理租户状态")}
          open={!!editing}
          onClose={() => setEditing(null)}
          width={480}
          destroyOnHidden
        >
          <ErrorNotice error={error} />
          <p>
            {tr("租户：")}
            {editing?.name}
          </p>
          <Form form={form} layout="vertical" onFinish={save}>
            {assigning ? (
              <>
                <Alert
                  type="warning"
                  showIcon
                  title={tr("必须先核对实际开通依据")}
                  description={tr("分配套餐会变更租户权益，本页面不代为收款。")}
                  style={{ marginBottom: 20 }}
                />
                <Form.Item label={tr("目标版本")} name="plan_id" rules={[{ required: true }]}>
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
                  label={tr("真实收款或审批参考")}
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
                        value
                          ? Promise.resolve()
                          : Promise.reject(new Error(tr("请先核对实际开通依据"))),
                    },
                  ]}
                >
                  <Checkbox>{tr("我已核对申请；付费版本已确认真实收款。")}</Checkbox>
                </Form.Item>
              </>
            ) : (
              <>
                <Form.Item
                  name="name"
                  label={tr("租户名称")}
                  rules={[{ required: true, whitespace: true }]}
                >
                  <Input maxLength={100} />
                </Form.Item>
                <Form.Item name="status" label={tr("运营状态")} rules={[{ required: true }]}>
                  <Select
                    options={[
                      { label: tr("正常"), value: "active" },
                      { label: tr("停用"), value: "suspended" },
                      { label: tr("到期"), value: "expired" },
                    ]}
                  />
                </Form.Item>
                <p className="form-hint">
                  {tr("停用不删除聊天与资料；会影响租户当前业务访问，请核对后保存。 ")}
                </p>
              </>
            )}
            <Button type="primary" htmlType="submit" loading={busy}>
              {tr("确认保存 ")}
            </Button>
          </Form>
        </Drawer>
        <Drawer
          rootClassName="studio-drawer"
          title={tr("开通租户并邀请所有者")}
          open={createOpen}
          onClose={() => setCreateOpen(false)}
          width={500}
          destroyOnHidden
        >
          <ErrorNotice error={createError} />
          <Alert
            type="info"
            showIcon
            title={tr("所有者通过邮件验证后激活")}
            description={tr(
              "平台不会发送明文密码。邮件失败时租户可能已经保存，请先核对列表并重发原邀请。",
            )}
            style={{ marginBottom: 20 }}
          />
          <Form form={createForm} layout="vertical" onFinish={create}>
            <Form.Item
              name="name"
              label={tr("租户名称")}
              rules={[{ required: true, whitespace: true }]}
            >
              <Input maxLength={100} />
            </Form.Item>
            <Form.Item
              name="admin_email"
              label={tr("初始所有者邮箱")}
              rules={[{ required: true }, { type: "email" }]}
            >
              <Input type="email" />
            </Form.Item>
            <Form.Item
              name="admin_name"
              label={tr("初始所有者称呼")}
              rules={[{ required: true, whitespace: true }]}
            >
              <Input maxLength={50} />
            </Form.Item>
            <Form.Item name="timezone" label={tr("时区")}>
              <Input readOnly />
            </Form.Item>
            <Form.Item
              name="can_serve"
              label={tr("所有者参与接待")}
              valuePropName="checked"
              extra={tr("默认仅管理、不占客服坐席；开启后占用免费版2席中的1席。")}
            >
              <Switch />
            </Form.Item>
            <Button type="primary" htmlType="submit" loading={busy}>
              {tr("创建租户并发送激活邀请 ")}
            </Button>
          </Form>
        </Drawer>
      </section>
    </div>
  );
}
