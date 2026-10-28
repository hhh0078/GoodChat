/** 文件作用说明：让租户管理员保存未读重复提醒周期与约1秒提示音，试听由明确点击触发。 */
"use client";
import { useEffect, useState } from "react";
import { App, Button, Form, InputNumber, Select, Space } from "antd";
import { SoundOutlined } from "@ant-design/icons";
import { api, errorMessage } from "@/lib/api";
import { useSession } from "@/features/auth/session";
import { ErrorNotice } from "@/components/page-state";
import { notificationSounds } from "@/lib/notification-sounds";
import { useNotificationAudio } from "@/features/chat/use-notification-audio";
type Settings = { repeat_unread_seconds: number; notification_sound_id: string };

/** TenantReminders 只写本表单的两项租户配置，失败保留选择，不覆盖其他欢迎语或个人音量。 */
export function TenantReminders() {
  const { tenantId } = useSession();
  const { message } = App.useApp();
  const [form] = Form.useForm<Settings>();
  const [loaded, setLoaded] = useState(false),
    [saving, setSaving] = useState(false);
  const [error, setError] = useState<unknown>(null),
    [revision, setRevision] = useState(0);
  const audio = useNotificationAudio();
  useEffect(() => {
    const controller = new AbortController();
    setLoaded(false);
    setError(null);
    form.resetFields();
    api<Settings>("/tenant/chat-settings", { tenantId, signal: controller.signal })
      .then((settings) => {
        if (!controller.signal.aborted) {
          form.setFieldsValue({
            repeat_unread_seconds: settings.repeat_unread_seconds ?? 10,
            notification_sound_id: settings.notification_sound_id || "gentle",
          });
          setLoaded(true);
        }
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      });
    return () => controller.abort();
  }, [tenantId, revision, form]);
  /** save 仅在服务端确认保存后报告成功。 */
  async function save(values: Settings) {
    setSaving(true);
    setError(null);
    try {
      const saved = await api<Settings>("/tenant/chat-settings", {
        tenantId,
        method: "PATCH",
        bodyJSON: values,
      });
      form.setFieldsValue(saved);
      message.success("接待提醒已保存");
    } catch (problem) {
      setError(problem);
    } finally {
      setSaving(false);
    }
  }
  /** preview 试听当前选中提示音，长度由受控1秒音频文件决定。 */
  async function preview() {
    const played = await audio.play(form.getFieldValue("notification_sound_id"), 0.6, true);
    if (!played) message.warning("提示音未能播放，请检查浏览器声音权限");
  }
  return (
    <section className="tenant-reminder-settings">
      <h2>接待提醒</h2>
      <p className="muted">
        新咨询或访客消息未查看时，按以下间隔持续提醒对应客服；实际打开并读完、转接或结束后停止。
      </p>
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      <Form form={form} layout="vertical" onFinish={save} style={{ maxWidth: 560 }}>
        <Form.Item
          name="repeat_unread_seconds"
          label="未读重复提醒间隔（秒）"
          rules={[
            { required: true, type: "integer", min: 1, max: 3600, message: "请输入1至3600秒整数" },
          ]}
          extra="默认10秒。没有明确接待客服的排队咨询不会重复打扰所有成员。"
        >
          <InputNumber min={1} max={3600} precision={0} disabled={!loaded} />
        </Form.Item>
        <Form.Item label="提示音 · 每段约1秒">
          <Space wrap>
            <Form.Item name="notification_sound_id" noStyle rules={[{ required: true }]}>
              <Select
                aria-label="租户提示音"
                style={{ width: 190 }}
                disabled={!loaded}
                options={notificationSounds.map((item) => ({ value: item.id, label: item.name }))}
              />
            </Form.Item>
            <Button icon={<SoundOutlined />} disabled={!loaded} onClick={() => void preview()}>
              试听
            </Button>
          </Space>
        </Form.Item>
        <p className="form-hint">
          客服和访客使用所选音色。每位客服仍可调整自己的音量或静音；浏览器需先有一次点击或输入才允许播放。
        </p>
        <Button type="primary" htmlType="submit" disabled={!loaded} loading={saving}>
          保存接待提醒
        </Button>
      </Form>
    </section>
  );
}
