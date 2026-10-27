/** 文件作用说明：配置真实营业欢迎场景、人工接入策略和访客评价期限，保存使用服务端版本。 */
"use client";
import { useEffect, useState } from "react";
import {
  Alert,
  App,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Switch,
  Tag,
} from "antd";
import { api } from "@/lib/api";
import { useSession } from "@/features/auth/session";
import { ErrorNotice } from "@/components/page-state";
/** AutomationPolicy 展示租户时区和完整策略，不把营业文案当作客服在线状态。 */
export function AutomationPolicy() {
  const { tenantId } = useSession();
  const [form] = Form.useForm();
  const [error, setError] = useState<unknown>(null);
  const [loaded, setLoaded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const [timezone, setTimezone] = useState("");
  const { message } = App.useApp();
  useEffect(() => {
    const controller = new AbortController();
    setLoaded(false);
    setError(null);
    api<Record<string, unknown>>("/tenant/automation-settings", {
      tenantId,
      signal: controller.signal,
    })
      .then((data) => {
        if (!controller.signal.aborted) {
          form.resetFields();
          form.setFieldsValue(data);
          setTimezone(String(data.timezone));
          setLoaded(true);
        }
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      });
    return () => controller.abort();
  }, [tenantId, revision, form]);
  /** save 使用已读版本保存完整配置，冲突时保留表单并提示重新读取。 */
  async function save(values: Record<string, unknown>) {
    setBusy(true);
    setError(null);
    try {
      const data = await api("/tenant/automation-settings", {
        method: "PUT",
        tenantId,
        bodyJSON: values,
      });
      form.setFieldsValue(data);
      message.success("营业欢迎与评价策略已保存");
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Card style={{ maxWidth: 900 }}>
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      <Alert
        type="info"
        showIcon
        title={
          <>
            租户时区 <Tag>{timezone || "读取中"}</Tag>
          </>
        }
        description="营业时段用于选择欢迎内容，不会伪造客服在线或停止真实接待。跨日时段以起始星期为准，结束时间不包含在内。"
        style={{ marginBottom: 20 }}
      />
      <Form form={form} layout="vertical" onFinish={save}>
        <Form.Item name="version" hidden>
          <InputNumber />
        </Form.Item>
        <Form.Item
          name="business_hours_enabled"
          label="按每周时段区分营业与非营业欢迎"
          valuePropName="checked"
          extra="关闭时按全天营业选择欢迎内容；开启且未设置时段时按非营业场景选择。"
        >
          <Switch />
        </Form.Item>
        <Form.List name="weekly_hours">
          {(fields, { add, remove }) => (
            <>
              {fields.map((field) => (
                <Space key={field.key} align="baseline" wrap>
                  <Form.Item
                    name={[field.name, "day"]}
                    label="起始星期"
                    rules={[{ required: true }]}
                  >
                    <Select
                      style={{ width: 130 }}
                      options={[
                        "星期日",
                        "星期一",
                        "星期二",
                        "星期三",
                        "星期四",
                        "星期五",
                        "星期六",
                      ].map((label, value) => ({ label, value }))}
                    />
                  </Form.Item>
                  <Form.Item
                    name={[field.name, "start"]}
                    label="开始"
                    rules={[
                      {
                        required: true,
                        pattern: /^([01]\d|2[0-3]):[0-5]\d$/,
                        message: "请填写HH:mm",
                      },
                    ]}
                  >
                    <Input placeholder="09:00" style={{ width: 100 }} />
                  </Form.Item>
                  <Form.Item
                    name={[field.name, "end"]}
                    label="结束"
                    rules={[
                      {
                        required: true,
                        pattern: /^([01]\d|2[0-3]):[0-5]\d$/,
                        message: "请填写HH:mm",
                      },
                    ]}
                  >
                    <Input placeholder="18:00" style={{ width: 100 }} />
                  </Form.Item>
                  <Button onClick={() => remove(field.name)}>移除时段</Button>
                </Space>
              ))}
              <Button onClick={() => add({ day: 1, start: "09:00", end: "18:00" })}>
                增加营业时段
              </Button>
            </>
          )}
        </Form.List>
        <Form.Item
          name="stop_welcome_on_agent"
          label="人工接入后停止未发送的基础欢迎内容"
          valuePropName="checked"
          extra="高级欢迎规则可以单独设置；已发送内容仍保留原始历史。"
          style={{ marginTop: 20 }}
        >
          <Switch />
        </Form.Item>
        <Form.Item name="evaluation_enabled" label="开放服务评价" valuePropName="checked">
          <Switch />
        </Form.Item>
        <Form.Item
          name="evaluation_window_hours"
          label="咨询结束后允许初评的小时数"
          rules={[{ required: true }]}
        >
          <InputNumber min={1} max={720} precision={0} />
        </Form.Item>
        <Form.Item
          name="evaluation_allow_edit"
          label="允许访客期限内修改评价"
          valuePropName="checked"
          extra="初评与每次改评都保留版本，管理员不能代评或直接改写。"
        >
          <Switch />
        </Form.Item>
        <Form.Item
          name="evaluation_edit_window_hours"
          label="首次评价后允许改评的小时数"
          rules={[{ required: true }]}
          extra="改评也不能超过咨询结束后的初评截止时间。"
        >
          <InputNumber min={1} max={168} precision={0} />
        </Form.Item>
        <Form.Item name="evaluation_tags" label="可选评价标签" extra="最多12个，每个不超过30字。">
          <Select mode="tags" maxCount={12} tokenSeparators={[","]} />
        </Form.Item>
        <Button type="primary" htmlType="submit" disabled={!loaded} loading={busy}>
          保存策略
        </Button>
      </Form>
    </Card>
  );
}
