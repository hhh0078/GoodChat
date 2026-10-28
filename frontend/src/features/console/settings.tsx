/** 文件作用说明：维护真实访客进入回调配置、投递日志和当前账号设备退出。 */
"use client";
import { useEffect, useState } from "react";
import {
  Alert,
  App,
  Button,
  Card,
  Drawer,
  Form,
  Input,
  Modal,
  Popconfirm,
  Space,
  Switch,
  Table,
  Tag,
  Tabs,
} from "antd";
import { api, errorMessage } from "@/lib/api";
import { useSession } from "@/features/auth/session";
import { useResource } from "@/lib/use-resource";
import type { Resource } from "@/lib/types";
import { AvatarSettings } from "./avatar-settings";
import { ErrorNotice, PageTitle, EmptyData } from "@/components/page-state";

type CallbackSettings = {
  enabled: boolean;
  url: string;
  include_context: boolean;
  include_phone: boolean;
  version: number;
  secret_configured: boolean;
};
/** SettingsPage 管理账号会话与租户进入事件回调，不在页面暴露已有密钥。 */
export function SettingsPage() {
  const { current, tenantId } = useSession();
  const membership = current.memberships.find((member) => member.tenant_id === tenantId);
  const manages = current.memberships.some(
    (member) => member.tenant_id === tenantId && ["owner", "admin"].includes(member.role),
  );
  const [form] = Form.useForm<CallbackSettings>();
  const [settings, setSettings] = useState<CallbackSettings | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [saving, setSaving] = useState(false);
  const [secret, setSecret] = useState("");
  const [selectedDelivery, setSelectedDelivery] = useState<{
    tenantId: string;
    record: Resource;
  } | null>(null);
  const activeDelivery =
    selectedDelivery?.tenantId === tenantId && manages ? selectedDelivery.record : null;
  const deliveries = useResource<Resource>(manages ? "/tenant/callback-deliveries" : "");
  const { message } = App.useApp();
  /** load 从服务端读取回调设置，保持租户切换时隔离。 */
  async function load() {
    if (!tenantId || !manages) return;
    setSettings(null);
    setError(null);
    try {
      const data = await api<CallbackSettings>("/tenant/callback-settings", { tenantId });
      setSettings(data);
      form.setFieldsValue(data);
    } catch (problem) {
      setError(problem);
    }
  }
  useEffect(() => {
    void load();
  }, [tenantId, manages]);
  /** save 提交带版本的配置变更，失败保留输入。 */
  async function save(values: CallbackSettings) {
    setSaving(true);
    setError(null);
    try {
      const result = await api<CallbackSettings>("/tenant/callback-settings", {
        method: "PUT",
        tenantId,
        bodyJSON: { ...values, version: settings?.version, events: ["visitor.entered"] },
      });
      setSettings(result);
      form.setFieldsValue(result);
      message.success("回调配置已保存");
    } catch (problem) {
      setError(problem);
    } finally {
      setSaving(false);
    }
  }
  /** perform 执行真实测试、重投或密钥轮换并刷新投递记录。 */
  async function perform(path: string) {
    try {
      const result = await api<{ signing_secret?: string }>(path, {
        method: "POST",
        tenantId,
        bodyJSON: {},
      });
      if (result?.signing_secret) setSecret(result.signing_secret);
      else message.success("请求已提交，请查看真实投递状态");
      deliveries.reload();
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  /** logoutOthers 撤销账号的其他设备登录，不伪造本地完成状态。 */
  async function logoutOthers() {
    try {
      await api("/auth/logout-others", { method: "POST" });
      message.success("其他设备登录已撤销");
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  return (
    <section className="console-screen console-settings">
      <PageTitle
        title="租户与账号设置"
        description="管理访客进入回调、查看投递结果和账号设备会话。"
      />
      <Tabs
        items={[
          {
            key: "callback",
            label: "访客进入回调",
            children: (
              <div className="two-columns console-settings-columns">
                <Card title="回调地址与内容">
                  <ErrorNotice error={error} retry={() => void load()} />
                  <Alert
                    type="info"
                    showIcon
                    title="访客进入后，平台会异步向已配置地址投递 visitor.entered 事件。保存配置不会直接通知该地址。"
                    style={{ marginBottom: 22 }}
                  />
                  <Form form={form} layout="vertical" onFinish={save}>
                    <Form.Item name="enabled" label="启用进入回调" valuePropName="checked">
                      <Switch />
                    </Form.Item>
                    <Form.Item
                      name="url"
                      label="回调 URL"
                      rules={[{ type: "url", message: "请填写有效的 HTTPS 地址" }]}
                      extra="建议使用商户后端公开 HTTPS 地址，投递范围由服务端校验。"
                    >
                      <Input placeholder="https://your-service.example/webhooks/goodchat" />
                    </Form.Item>
                    <Form.Item
                      name="include_context"
                      label="发送本次业务上下文"
                      valuePropName="checked"
                    >
                      <Switch />
                    </Form.Item>
                    <Form.Item
                      name="include_phone"
                      label="发送访客提供的手机号"
                      valuePropName="checked"
                      extra="请仅在业务确实需要时开启，并向访客说明用途。"
                    >
                      <Switch />
                    </Form.Item>
                    <Space wrap>
                      <Button type="primary" htmlType="submit" loading={saving}>
                        保存配置
                      </Button>
                      <Button
                        onClick={() => void perform("/tenant/callback-settings/test")}
                        disabled={!settings?.enabled}
                      >
                        发送测试事件
                      </Button>
                    </Space>
                  </Form>
                </Card>
                <Card title="签名验证">
                  <p className="muted">
                    接收服务应验证事件签名，并按事件编号去重。回调失败不影响访客正常咨询。
                  </p>
                  <p>
                    签名密钥：
                    <Tag color={settings?.secret_configured ? "green" : "default"}>
                      {settings?.secret_configured ? "已配置" : "尚未配置"}
                    </Tag>
                  </p>
                  <Popconfirm
                    title="生成新的签名密钥？"
                    description="旧密钥将失效，请同步更新商户接收服务。"
                    onConfirm={() => perform("/tenant/callback-settings/rotate-secret")}
                  >
                    <Button>生成或轮换签名密钥</Button>
                  </Popconfirm>
                  <p className="form-hint">新密钥只展示一次；关闭窗口后无法再次读取原文。</p>
                </Card>
              </div>
            ),
          },
          {
            key: "deliveries",
            label: "回调投递日志",
            children: (
              <div className="table-card">
                <ErrorNotice error={deliveries.error} retry={deliveries.reload} />
                <Table
                  rowKey="id"
                  dataSource={deliveries.items}
                  loading={deliveries.loading}
                  scroll={{ x: 1050 }}
                  locale={{ emptyText: <EmptyData text="还没有回调投递记录" /> }}
                  columns={[
                    { title: "事件", dataIndex: "event_type" },
                    {
                      title: "投递状态",
                      dataIndex: "status",
                      render: (value) => <Tag>{String(value || "—")}</Tag>,
                    },
                    { title: "尝试次数", dataIndex: "attempts" },
                    {
                      title: "HTTP 状态",
                      dataIndex: "response_status",
                      render: (value) => value || "—",
                    },
                    {
                      title: "结果说明",
                      dataIndex: "last_error",
                      ellipsis: true,
                      render: (value) => value || "—",
                    },
                    {
                      title: "创建时间",
                      dataIndex: "created_at",
                      render: (value) =>
                        value ? new Date(String(value)).toLocaleString("zh-CN") : "—",
                    },
                    {
                      title: "操作",
                      render: (_, row) => (
                        <Space>
                          <Button
                            type="link"
                            size="small"
                            onClick={() => setSelectedDelivery({ tenantId, record: row })}
                          >
                            查看详情
                          </Button>
                          <Button
                            type="link"
                            size="small"
                            onClick={() =>
                              void perform(`/tenant/callback-deliveries/${row.id}/retry`)
                            }
                          >
                            重试投递
                          </Button>
                        </Space>
                      ),
                    },
                  ]}
                />
              </div>
            ),
          },
          {
            key: "avatars",
            label: "访客头像池",
            children: <AvatarSettings key={`${tenantId}.pool`} mode="pool" />,
          },
          {
            key: "account",
            label: "账号与设备",
            children: (
              <section className="console-account">
                <AvatarSettings key={`${tenantId}.agent`} mode="agent" />
                <h2>当前账号</h2>
                <dl className="console-account-details">
                  <div>
                    <dt>显示名称</dt>
                    <dd>{current.user.display_name || "未填写"}</dd>
                  </div>
                  <div>
                    <dt>邮箱</dt>
                    <dd>{current.user.email}</dd>
                  </div>
                  <div>
                    <dt>当前租户</dt>
                    <dd>{membership?.tenant_name || "未加入工作空间"}</dd>
                  </div>
                  <div>
                    <dt>角色</dt>
                    <dd>
                      {membership?.role === "owner"
                        ? "所有者"
                        : membership?.role === "admin"
                          ? "管理员"
                          : membership
                            ? "客服"
                            : "—"}
                    </dd>
                  </div>
                </dl>
                <h2>账号会话</h2>
                <p className="muted">退出其他设备后，当前设备仍可继续使用。</p>
                <Space>
                  <Button href="/reset-password">重置密码</Button>
                  <Popconfirm title="退出其他设备上的登录？" onConfirm={logoutOthers}>
                    <Button>退出其他设备</Button>
                  </Popconfirm>
                </Space>
              </section>
            ),
          },
        ].filter((item) => manages || item.key === "account")}
      />
      <Drawer
        rootClassName="console-drawer"
        title="投递详情"
        open={!!activeDelivery}
        onClose={() => setSelectedDelivery(null)}
        width={480}
      >
        <dl className="console-account-details">
          <div>
            <dt>事件编号</dt>
            <dd>{activeDelivery?.id || "—"}</dd>
          </div>
          <div>
            <dt>事件</dt>
            <dd>{String(activeDelivery?.event_type || "—")}</dd>
          </div>
          <div>
            <dt>投递状态</dt>
            <dd>{String(activeDelivery?.status || "—")}</dd>
          </div>
          <div>
            <dt>累计尝试</dt>
            <dd>{String(activeDelivery?.attempts ?? "—")}</dd>
          </div>
          <div>
            <dt>HTTP 状态</dt>
            <dd>{String(activeDelivery?.response_status || "—")}</dd>
          </div>
          <div>
            <dt>结果说明</dt>
            <dd>{String(activeDelivery?.last_error || "—")}</dd>
          </div>
        </dl>
        <p className="form-hint">这里显示当前投递记录的累计结果。</p>
      </Drawer>
      <Modal
        title="请保存新的签名密钥"
        open={!!secret}
        onCancel={() => setSecret("")}
        footer={
          <Button type="primary" onClick={() => setSecret("")}>
            我已保存，关闭窗口
          </Button>
        }
      >
        <Alert
          type="warning"
          title="此密钥只展示一次，请复制到商户接收服务的安全配置中。"
          showIcon
          style={{ marginBottom: 16 }}
        />
        <Input.TextArea readOnly value={secret} rows={3} />
      </Modal>
    </section>
  );
}
