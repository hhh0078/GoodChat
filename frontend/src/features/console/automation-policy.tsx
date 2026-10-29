/** 文件作用说明：配置真实营业欢迎场景、人工接入策略和访客评价期限，保存使用服务端版本。 */
"use client";
import { useEffect, useState } from "react";
import { Alert, App, Button, Form, Input, InputNumber, Select, Space, Switch, Tag } from "antd";
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
    <section className="studio-policy studio-settings-surface">
      <div className="studio-panel-heading">
        <h2>营业与评价策略</h2>
        <p>设置营业时间对应的欢迎内容，以及访客结束咨询后可以评价多久。</p>
      </div>
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      <Alert
        type="info"
        showIcon
        title={
          <>
            租户时区 <Tag>{timezone || "读取中"}</Tag>
          </>
        }
        description="以下时间都按此时区计算。营业时间用于选择“高级欢迎规则”中的营业中或非营业内容；是否能接待仍由客服在线状态和分配规则决定。"
        style={{ marginBottom: 20 }}
      />
      <Form form={form} layout="vertical" onFinish={save}>
        <h3 className="studio-form-section">营业时段</h3>
        <Form.Item name="version" hidden style={{ display: "none" }}>
          <InputNumber />
        </Form.Item>
        <Form.Item
          name="business_hours_enabled"
          label="按每周时段区分营业与非营业欢迎"
          valuePropName="checked"
          extra="开启后，只有下方时段内使用“营业中”规则，其余时间使用“非营业”规则。关闭后全天视为营业中；开启但未添加时段时，全天视为非营业。"
        >
          <Switch />
        </Form.Item>
        <p className="form-hint">
          例如周一 09:00 至 18:00，18:00 起进入非营业场景。夜班可填周一 22:00 至
          02:00，表示持续到周二凌晨 02:00；开始和结束时间不能相同。每天可添加多个时段。
        </p>
        <Form.List name="weekly_hours">
          {(fields, { add, remove }) => (
            <>
              {fields.map((field) => (
                <Space key={field.key} className="studio-hours-row" align="baseline" wrap>
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
          extra="开启后，客服接入咨询时，尚未发出的基础文字欢迎语会取消，避免人工接待后再发欢迎语。高级欢迎规则有各自的同名设置；已发送的消息会保留。"
          style={{ marginTop: 20 }}
        >
          <Switch />
        </Form.Item>
        <h3 className="studio-form-section">服务评价</h3>
        <Form.Item
          name="evaluation_enabled"
          label="开放服务评价"
          valuePropName="checked"
          extra="开启后，访客可在咨询结束后主动提交星级、意见和标签。关闭后不接受新评价或改评，已有评价仍保留。"
        >
          <Switch />
        </Form.Item>
        <Form.Item
          name="evaluation_window_hours"
          label="咨询结束后允许初评的小时数"
          rules={[{ required: true }]}
          extra="从本次咨询结束时开始计时。比如填写 24，访客可在结束后的 24 小时内首次评价；可设置 1 至 720 小时。"
        >
          <InputNumber min={1} max={720} precision={0} />
        </Form.Item>
        <Form.Item
          name="evaluation_allow_edit"
          label="允许访客期限内修改评价"
          valuePropName="checked"
          extra="开启后，访客可在下方期限内修改自己的评分、意见和标签；关闭后只允许首次评价。每次提交都会保留历史版本。"
        >
          <Switch />
        </Form.Item>
        <Form.Item
          name="evaluation_edit_window_hours"
          label="首次评价后允许改评的小时数"
          rules={[{ required: true }]}
          extra="从首次评价开始计时，后续修改不会重新计时，且不能超过初评截止时间。例如初评期限 24 小时，访客在结束后第 23 小时评价，即使此处填 24，也只剩 1 小时可改评。可设置 1 至 168 小时。"
        >
          <InputNumber min={1} max={168} precision={0} />
        </Form.Item>
        <Form.Item
          name="evaluation_tags"
          label="可选评价标签"
          extra="供访客评价时点选，例如“响应及时”“解决问题”。输入标签后按回车添加，最多 12 个，每个不超过 30 字；留空时仅填写星级和意见。"
        >
          <Select mode="tags" maxCount={12} tokenSeparators={[","]} />
        </Form.Item>
        <Button type="primary" htmlType="submit" disabled={!loaded} loading={busy}>
          保存策略
        </Button>
      </Form>
    </section>
  );
}
