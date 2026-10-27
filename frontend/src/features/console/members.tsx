/** 文件作用说明：管理租户成员及邀请，支持真实服务端角色调整、停用与重发。 */
"use client";
import { useEffect, useState } from "react";
import {
  App,
  Button,
  Drawer,
  Modal,
  Alert,
  Form,
  Input,
  InputNumber,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
} from "antd";
import { PlusOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { api, errorMessage } from "@/lib/api";
import { useSession } from "@/features/auth/session";
import { useResource } from "@/lib/use-resource";
import type { Member, Resource } from "@/lib/types";
import { PageTitle, ErrorNotice, EmptyData } from "@/components/page-state";

/** MembersPage 维护当前租户成员及指定邮箱邀请。 */
export function MembersPage() {
  const session = useSession();
  const owns = session.current.memberships.some(
    (item) => item.tenant_id === session.tenantId && item.role === "owner",
  );
  const [transferOpen, setTransferOpen] = useState(false);
  const [transferCodeWait, setTransferCodeWait] = useState(0);
  const [transferForm] = Form.useForm();
  const [transferInfo, setTransferInfo] = useState("");
  useEffect(() => {
    if (!transferCodeWait) return;
    const timer = setTimeout(() => setTransferCodeWait((value) => value - 1), 1000);
    return () => clearTimeout(timer);
  }, [transferCodeWait]);
  const members = useResource<Member>("/tenant/members");
  const invitations = useResource<Resource>("/tenant/invitations");
  const [editing, setEditing] = useState<Member | null | undefined>();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [form] = Form.useForm();
  const { message } = App.useApp();
  /** edit 填充成员编辑或邀请默认值。 */
  function edit(member: Member | null) {
    form.resetFields();
    form.setFieldsValue(member || { role: "agent", can_serve: true, capacity: 5, disabled: false });
    setError(null);
    setEditing(member);
  }
  /** save 保存成员权限或发送具有实际状态的邀请。 */
  async function save(values: Record<string, unknown>) {
    setSaving(true);
    setError(null);
    try {
      if (editing) {
        const body = {
          display_name: values.display_name,
          can_serve: values.can_serve,
          capacity: values.capacity,
          disabled: values.disabled,
          ...(editing.role === "owner" ? {} : { role: values.role }),
        };
        await api(`/tenant/members/${editing.id}`, {
          method: "PATCH",
          tenantId: members.tenantId,
          bodyJSON: body,
        });
        message.success("成员设置已保存");
      } else {
        await api("/tenant/invitations", {
          method: "POST",
          tenantId: members.tenantId,
          bodyJSON: values,
          idempotent: true,
        });
        message.success("邀请已创建，邮件发送状态请在邀请列表查看");
      }
      setEditing(undefined);
      members.reload();
      invitations.reload();
    } catch (problem) {
      setError(problem);
    } finally {
      setSaving(false);
    }
  }
  /** mutate 执行移出、重发或撤销，并展示真实后端结果。 */
  async function mutate(path: string, method: string) {
    try {
      await api(path, { method, tenantId: members.tenantId });
      message.success("操作已完成");
      members.reload();
      invitations.reload();
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  /** sendTransferCode 只向当前所有者邮箱申请转移验证码，展示邮件服务器的实际受理状态。 */
  async function sendTransferCode() {
    setSaving(true);
    setError(null);
    try {
      const result = await api<{ delivery_status: string; retry_after: number }>(
        "/auth/send-code",
        { method: "POST", bodyJSON: { email: session.current.user.email, purpose: "ownership" } },
      );
      setTransferCodeWait(result.retry_after || 60);
      setTransferInfo(
        result.delivery_status === "accepted"
          ? "邮件服务器已受理，请查看当前所有者邮箱。"
          : "邮件投递结果尚未确认，请核对收件箱后再继续。",
      );
    } catch (problem) {
      setError(problem);
    } finally {
      setSaving(false);
    }
  }
  /** transferOwnership 验证所有者邮箱后转移权限，成功再重新读取成员身份。 */
  async function transferOwnership(values: Record<string, string>) {
    setSaving(true);
    setError(null);
    try {
      await api("/tenant/transfer-ownership", {
        method: "POST",
        tenantId: members.tenantId,
        bodyJSON: values,
      });
      setTransferOpen(false);
      members.reload();
      await session.reload();
      message.success("租户所有者已转移");
    } catch (problem) {
      setError(problem);
    } finally {
      setSaving(false);
    }
  }
  const memberColumns: ColumnsType<Member> = [
    {
      title: "成员",
      key: "name",
      render: (_, row) => (
        <>
          <strong>{row.display_name || "未填写称呼"}</strong>
          <div className="muted" style={{ fontSize: 12 }}>
            {row.email}
          </div>
        </>
      ),
    },
    {
      title: "角色",
      dataIndex: "role",
      render: (value) => (
        <Tag>{value === "owner" ? "所有者" : value === "admin" ? "管理员" : "客服"}</Tag>
      ),
    },
    {
      title: "接待资格",
      dataIndex: "can_serve",
      render: (value) => (value ? "可参与接待" : "仅管理"),
    },
    { title: "接待上限", dataIndex: "capacity" },
    {
      title: "账号状态",
      dataIndex: "disabled",
      render: (value) => <Tag color={value ? "default" : "green"}>{value ? "已停用" : "正常"}</Tag>,
    },
    {
      title: "操作",
      key: "actions",
      render: (_, row) => (
        <Space>
          <Button type="link" size="small" onClick={() => edit(row)}>
            编辑
          </Button>
          {row.role !== "owner" && (
            <Popconfirm
              title="将这位成员移出当前租户？"
              description="历史记录会保留，当前接待由服务端检查。"
              onConfirm={() => mutate(`/tenant/members/${row.id}`, "DELETE")}
            >
              <Button type="link" danger size="small">
                移出
              </Button>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];
  const invitationColumns: ColumnsType<Resource> = [
    { title: "邮箱", dataIndex: "email" },
    { title: "称呼", dataIndex: "display_name" },
    {
      title: "角色",
      dataIndex: "role",
      render: (value) => (value === "admin" ? "管理员" : "客服"),
    },
    {
      title: "邀请状态",
      dataIndex: "status",
      render: (value) => <Tag>{String(value || "—")}</Tag>,
    },
    { title: "发信状态", dataIndex: "delivery_status", render: (value) => String(value || "—") },
    {
      title: "到期时间",
      dataIndex: "expires_at",
      render: (value) => (value ? new Date(String(value)).toLocaleString("zh-CN") : "—"),
    },
    {
      title: "操作",
      render: (_, row) => (
        <Space>
          <Button
            size="small"
            type="link"
            onClick={() => mutate(`/tenant/invitations/${row.id}/resend`, "POST")}
          >
            重发
          </Button>
          <Popconfirm
            title="撤销此邀请？"
            onConfirm={() => mutate(`/tenant/invitations/${row.id}`, "DELETE")}
          >
            <Button size="small" type="link" danger>
              撤销
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];
  return (
    <>
      <PageTitle
        title="团队成员"
        description="管理员与客服分别授权，邀请成员加入当前租户。"
        action={
          <Space>
            {owns && (
              <Button
                onClick={() => {
                  setError(null);
                  transferForm.resetFields();
                  setTransferInfo("");
                  setTransferOpen(true);
                }}
              >
                转移所有者
              </Button>
            )}
            <Button type="primary" icon={<PlusOutlined />} onClick={() => edit(null)}>
              邀请成员
            </Button>
          </Space>
        }
      />
      <div className="table-card">
        <Tabs
          items={[
            {
              key: "members",
              label: `成员 ${members.items.length}`,
              children: (
                <>
                  <ErrorNotice error={members.error} retry={members.reload} />
                  <Table
                    rowKey="id"
                    columns={memberColumns}
                    dataSource={members.items}
                    loading={members.loading}
                    scroll={{ x: 900 }}
                    locale={{ emptyText: <EmptyData /> }}
                  />
                </>
              ),
            },
            {
              key: "invitations",
              label: "邀请记录",
              children: (
                <>
                  <ErrorNotice error={invitations.error} retry={invitations.reload} />
                  <Table
                    rowKey="id"
                    columns={invitationColumns}
                    dataSource={invitations.items}
                    loading={invitations.loading}
                    scroll={{ x: 1000 }}
                    locale={{ emptyText: <EmptyData text="还没有邀请记录" /> }}
                  />
                </>
              ),
            },
          ]}
        />
      </div>
      <Drawer
        title={editing ? "编辑成员" : "邀请团队成员"}
        open={editing !== undefined}
        onClose={() => setEditing(undefined)}
        width={480}
        destroyOnHidden
      >
        <ErrorNotice error={error} />
        <Form form={form} layout="vertical" onFinish={save}>
          {!editing && (
            <Form.Item
              name="email"
              label="受邀邮箱"
              rules={[{ required: true }, { type: "email", message: "请填写有效邮箱" }]}
            >
              <Input type="email" autoComplete="email" />
            </Form.Item>
          )}
          <Form.Item
            name="display_name"
            label="显示名称"
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={50} />
          </Form.Item>
          {editing?.role === "owner" ? (
            <p>当前角色：租户所有者</p>
          ) : (
            <Form.Item name="role" label="角色" rules={[{ required: true }]}>
              <Select
                options={[
                  { label: "客服", value: "agent" },
                  { label: "租户管理员", value: "admin" },
                ]}
              />
            </Form.Item>
          )}
          <Form.Item name="can_serve" label="允许参与接待" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="capacity" label="同时接待上限" rules={[{ required: true }]}>
            <InputNumber min={1} max={1000} style={{ width: "100%" }} />
          </Form.Item>
          {editing && (
            <Form.Item
              name="disabled"
              label="停用此成员"
              valuePropName="checked"
              extra="停用会影响现有登录与新增接待，最后一名管理员受服务端保护。"
            >
              <Switch />
            </Form.Item>
          )}
          <Space>
            <Button type="primary" htmlType="submit" loading={saving}>
              {editing ? "保存设置" : "创建邀请"}
            </Button>
            <Button onClick={() => setEditing(undefined)}>取消</Button>
          </Space>
        </Form>
      </Drawer>
      <Modal
        title="转移租户所有者"
        open={transferOpen}
        onCancel={() => setTransferOpen(false)}
        footer={null}
      >
        <ErrorNotice error={error} />
        <Alert
          type="warning"
          showIcon
          title="此操作会把最高租户管理权限交给所选成员"
          description="请核对接收成员；通过当前所有者邮箱验证后才能完成。"
          style={{ marginBottom: 20 }}
        />
        {transferInfo && <p className="muted">{transferInfo}</p>}
        <Form form={transferForm} layout="vertical" onFinish={transferOwnership}>
          <Form.Item name="member_id" label="新的租户所有者" rules={[{ required: true }]}>
            <Select
              options={members.items
                .filter((item) => !item.disabled && item.role !== "owner")
                .map((item) => ({ label: `${item.display_name} · ${item.email}`, value: item.id }))}
            />
          </Form.Item>
          <p className="form-hint">验证邮箱：{session.current.user.email}</p>
          <Form.Item label="邮箱验证码" required>
            <div className="code-row">
              <Form.Item
                name="code"
                noStyle
                rules={[{ required: true, pattern: /^\d{6}$/, message: "请输入6位验证码" }]}
              >
                <Input inputMode="numeric" maxLength={6} autoComplete="one-time-code" />
              </Form.Item>
              <Button
                loading={saving}
                disabled={transferCodeWait > 0}
                onClick={() => void sendTransferCode()}
              >
                {transferCodeWait ? `${transferCodeWait} 秒后重发` : "获取验证码"}
              </Button>
            </div>
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={saving}>
            验证并转移所有者
          </Button>
        </Form>
      </Modal>
    </>
  );
}
