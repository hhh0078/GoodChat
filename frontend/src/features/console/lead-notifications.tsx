/** 文件作用说明：配置默认关闭的留言邮件提醒并展示真实排队、SMTP受理、失败及不确定结果。 */
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
  InputNumber,
  Popconfirm,
  Space,
  Switch,
  Table,
  Tag,
} from "antd";
import { api, errorMessage } from "@/lib/api";
import { useSession } from "@/features/auth/session";
import { useResource } from "@/lib/use-resource";
import { ErrorNotice } from "@/components/page-state";
import type { Resource } from "@/lib/types";
const deliveryNames: Record<string, string> = {
  queued: "排队中",
  running: "投递中",
  accepted: "邮件服务已受理",
  failed: "失败",
  unknown: "结果不确定",
};
const failureNames: Record<string, string> = {
  configuration: "发信服务配置不完整",
  settings_changed: "提醒设置已改变，未开始投递",
  temporary: "临时失败",
  permanent: "邮件服务明确拒绝",
  transport: "传输失败",
  confirmation_lost: "最终受理确认未收到",
  worker_interrupted: "执行中断，无法确定受理结果",
  tls_required: "发信服务需要安全连接",
  invalid_notification: "通知输入无效",
  invalid_sender_result: "投递结果无法识别",
};
/** LeadNotifications 保存管理员主动选择的提醒邮箱，不提供任意客户外发或测试收件按钮。 */
export function LeadNotifications() {
  const { tenantId } = useSession();
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const enabled = Form.useWatch("enabled", form);
  const [configured, setConfigured] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  const [selected, setSelected] = useState<Resource | null>(null);
  const jobs = useResource<Resource>("/tenant/lead-notifications");
  useEffect(() => {
    const controller = new AbortController();
    setLoaded(false);
    setError(null);
    api<{ enabled: boolean; email: string; version: number; smtp_configured: boolean }>(
      "/tenant/lead-settings",
      { tenantId, signal: controller.signal },
    )
      .then((data) => {
        if (controller.signal.aborted) return;
        form.setFieldsValue({ enabled: data.enabled, email: data.email, version: data.version });
        setConfigured(data.smtp_configured);
        setLoaded(true);
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      });
    return () => controller.abort();
  }, [tenantId, revision, form]);
  useEffect(() => {
    if (!jobs.items.some((item) => ["queued", "running"].includes(String(item.status)))) return;
    const timer = setInterval(jobs.reload, 3000);
    return () => clearInterval(timer);
  }, [jobs.items, jobs.reload]);
  /** save 只保存启停、邮箱与版本，配置状态不能作为SMTP投递或收件证据。 */
  async function save(values: Record<string, unknown>) {
    setBusy(true);
    setError(null);
    try {
      await api("/tenant/lead-settings", { method: "PUT", tenantId, bodyJSON: values });
      message.success("留言提醒配置已保存");
      setRevision((value) => value + 1);
      jobs.reload();
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  /** retry 请求重排确定失败任务，冷却与当前邮箱配置仍由服务端检查。 */
  async function retry(item: Resource) {
    try {
      await api(`/tenant/lead-notifications/${item.id}/retry`, {
        method: "POST",
        tenantId,
        bodyJSON: {},
      });
      jobs.reload();
      message.success("失败提醒已重新排队，尚未投递");
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  return (
    <section className="studio-lead-notifications">
      <Card
        className="studio-mail-settings"
        style={{ maxWidth: 800, marginBottom: 20 }}
        title={
          <>
            留言邮件提醒{" "}
            <Tag color={configured ? "green" : "default"}>
              发信服务{configured ? "已配置" : "未配置"}
            </Tag>
          </>
        }
      >
        <Alert
          type="info"
          showIcon
          title="访客提交离线留言后，给管理员发一封邮件提醒。默认关闭。"
          description="邮件提供留言编号和登录入口，具体内容与附件请登录后查看。此处只提醒新提交的离线留言；聊天消息的提示音由客服在右上角调整音量。"
          style={{ marginBottom: 16 }}
        />
        <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
        <Form form={form} layout="vertical" onFinish={save}>
          <Form.Item name="version" hidden style={{ display: "none" }}>
            <InputNumber />
          </Form.Item>
          <Form.Item
            name="enabled"
            label="新留言邮件提醒"
            valuePropName="checked"
            extra="开启并保存后，新收到的离线留言会排队发送邮件；不会为历史留言补发。需要平台已配置发信服务。"
          >
            <Switch />
          </Form.Item>
          <Form.Item
            name="email"
            label="管理员收件邮箱"
            rules={[
              { type: "email", message: "请填写有效邮箱" },
              { required: !!enabled, message: "开启提醒需填写收件邮箱" },
            ]}
            extra="填写负责跟进留言的管理员邮箱。更换邮箱后，尚未发送的旧提醒会停止，可在下方记录中查看原因。"
          >
            <Input maxLength={254} autoComplete="email" />
          </Form.Item>
          <Button
            type="primary"
            htmlType="submit"
            loading={busy}
            disabled={!loaded || (!!enabled && !configured)}
          >
            保存提醒设置
          </Button>
        </Form>
      </Card>
      <div className="table-card studio-table-surface">
        <Space style={{ marginBottom: 12 }}>
          <strong>真实通知记录</strong>
          <Button onClick={jobs.reload}>刷新记录</Button>
        </Space>
        <p className="form-hint">
          “排队中”表示等待发送；“邮件服务已受理”表示已交给发信服务，不等于已到收件箱。“失败”可等待一分钟后重试；“结果不确定”请先核对邮箱，系统不会自动重发。
        </p>
        <ErrorNotice error={jobs.error} retry={jobs.reload} />
        <Table
          rowKey="id"
          dataSource={jobs.items}
          loading={jobs.loading}
          scroll={{ x: 850 }}
          columns={[
            { title: "留言编号", dataIndex: "lead_id", ellipsis: true },
            { title: "收件邮箱", dataIndex: "recipient" },
            {
              title: "状态",
              dataIndex: "status",
              render: (value) => (
                <Tag
                  color={
                    value === "accepted" ? "green" : value === "unknown" ? "orange" : "default"
                  }
                >
                  {deliveryNames[String(value)] || String(value)}
                </Tag>
              ),
            },
            { title: "尝试次数", dataIndex: "attempts" },
            {
              title: "说明",
              dataIndex: "last_error",
              render: (value) => failureNames[String(value)] || String(value || "—"),
            },
            {
              title: "操作",
              render: (_, item) => (
                <Space>
                  <Button type="link" onClick={() => setSelected(item)}>
                    查看尝试记录
                  </Button>
                  {item.status === "failed" && (
                    <Popconfirm
                      title="按当前已配置邮箱重新排队？"
                      description="失败后需等待一分钟。结果不确定的通知不能重发。"
                      onConfirm={() => retry(item)}
                    >
                      <Button type="link">重试失败提醒</Button>
                    </Popconfirm>
                  )}
                </Space>
              ),
            },
          ]}
        />
      </div>
      <Drawer
        rootClassName="studio-drawer"
        title="通知尝试记录"
        open={!!selected}
        onClose={() => setSelected(null)}
        width={650}
      >
        <p className="muted">
          这里记录每次发送的处理结果；邮件是否进入收件箱，仍需到收件邮箱核对。
        </p>
        <Table
          rowKey={(item) => String(item.attempt)}
          dataSource={(selected?.history as Record<string, unknown>[]) || []}
          columns={[
            { title: "尝试", dataIndex: "attempt" },
            {
              title: "结果",
              dataIndex: "status",
              render: (value) => deliveryNames[String(value)] || String(value),
            },
            {
              title: "说明",
              dataIndex: "failure_kind",
              render: (value) => failureNames[String(value)] || String(value || "—"),
            },
            {
              title: "时间",
              dataIndex: "at",
              render: (value) => new Date(String(value)).toLocaleString("zh-CN"),
            },
          ]}
        />
      </Drawer>
    </section>
  );
}
