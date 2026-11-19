/** 文件作用说明：配置每个接入链接的JS悬浮窗、展示时段和品牌按钮，并提供可复制代码。 */
"use client";
import { ErrorNotice } from "@/components/page-state";
import { useTranslator } from "@/i18n/use-translator";
import { api, errorMessage } from "@/lib/api";
import type { AccessLink } from "@/lib/types";
import { CustomerServiceOutlined } from "@ant-design/icons";
import {
  App,
  Button,
  Checkbox,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Spin,
  Switch,
  Tabs,
} from "antd";
import { useEffect, useState } from "react";

type Widget = {
  version: number;
  enabled: boolean;
  label: string;
  logo_url: string;
  color: string;
  side: "left" | "right";
  horizontal_offset: number;
  bottom_offset: number;
  width: number;
  height: number;
  show_delay_seconds: number;
  auto_open_delay_seconds: number;
  schedule_enabled: boolean;
  start: string;
  end: string;
  weekdays: number[];
};

/** widgetCode 生成仅包含公开入口编号的外部脚本，不携带租户凭据或登录信息。 */
export function widgetCode(origin: string, entryKey: string): string {
  return `<script src="${origin}/api/v1/widget/${encodeURIComponent(entryKey)}.js" async></script>`;
}

/** LinkWidgetSettings 通过独立版本保存悬浮窗，避免覆盖链接的接待或装修设置。 */
export function LinkWidgetSettings({ link, tenantId }: { link: AccessLink; tenantId: string }) {
  const tr = useTranslator();
  const { message } = App.useApp();
  const [form] = Form.useForm<Widget>();
  const [current, setCurrent] = useState<Widget | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const values = Form.useWatch([], form) as Widget | undefined;
  useEffect(() => {
    const controller = new AbortController();
    setCurrent(null);
    setError(null);
    api<Widget>(`/tenant/access-links/${link.id}/widget`, { tenantId, signal: controller.signal })
      .then((value) => {
        if (!controller.signal.aborted) {
          setCurrent(value);
          form.setFieldsValue(value);
        }
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      });
    return () => controller.abort();
  }, [link.id, tenantId, form, revision]);
  /** save 校验并保存所有页签中的字段，发生版本冲突时保留当前编辑内容。 */
  async function save(value: Widget) {
    if (!current) return;
    setBusy(true);
    setError(null);
    try {
      const result = await api<Widget>(`/tenant/access-links/${link.id}/widget`, {
        method: "PUT",
        tenantId,
        bodyJSON: { ...current, ...value, version: current.version },
      });
      setCurrent(result);
      form.setFieldsValue(result);
      message.success(tr("已保存"));
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  /** copy 仅在浏览器确认复制成功后显示反馈。 */
  async function copy() {
    try {
      await navigator.clipboard.writeText(widgetCode(window.location.origin, link.entry_key));
      message.success(tr("已复制"));
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  const required = [{ required: true }];
  const days = [tr("周日"), tr("周一"), tr("周二"), tr("周三"), tr("周四"), tr("周五"), tr("周六")];
  return (
    <section className="console-widget-settings">
      <h3>{tr("悬浮窗")}</h3>
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      {!current ? (
        !error && <Spin />
      ) : (
        <Form form={form} layout="vertical" onFinish={save}>
          <Form.Item name="enabled" label={tr("启用")} valuePropName="checked">
            <Switch />
          </Form.Item>
          <Tabs
            items={[
              {
                key: "appearance",
                label: tr("外观"),
                forceRender: true,
                children: (
                  <>
                    <div
                      style={{
                        padding: 24,
                        marginBottom: 24,
                        background: "var(--color-bg-layout, #f3f5f9)",
                        borderRadius: 12,
                      }}
                    >
                      <span
                        style={{
                          display: "inline-flex",
                          alignItems: "center",
                          gap: 10,
                          background: values?.color || current.color,
                          color: "white",
                          borderRadius: 28,
                          padding: "10px 18px",
                          minHeight: 56,
                          maxWidth: "100%",
                        }}
                      >
                        {values?.logo_url && /^https:\/\//.test(values.logo_url) ? (
                          <img
                            src={values.logo_url}
                            alt=""
                            referrerPolicy="no-referrer"
                            style={{
                              width: 36,
                              height: 36,
                              objectFit: "contain",
                              borderRadius: "50%",
                              background: "white",
                            }}
                          />
                        ) : (
                          <CustomerServiceOutlined />
                        )}
                        <span
                          style={{
                            overflow: "hidden",
                            textOverflow: "ellipsis",
                            whiteSpace: "nowrap",
                          }}
                        >
                          {values?.label || current.label}
                        </span>
                      </span>
                    </div>
                    <Form.Item
                      name="label"
                      label={tr("按钮文字")}
                      rules={[...required, { max: 40, whitespace: true }]}
                    >
                      <Input maxLength={40} />
                    </Form.Item>
                    <Form.Item
                      name="logo_url"
                      label={tr("收起图标地址")}
                      extra={tr("填写 HTTPS 图片地址，留空使用默认客服图标。")}
                      rules={[
                        {
                          validator: async (_, value) => {
                            if (!value) return;
                            const url = new URL(value);
                            if (url.protocol !== "https:" || url.username || url.password)
                              throw new Error(tr("请输入HTTPS地址"));
                          },
                        },
                      ]}
                    >
                      <Input maxLength={2048} placeholder="https://…" />
                    </Form.Item>
                    <Form.Item
                      name="color"
                      label={tr("主题色")}
                      rules={[...required, { pattern: /^#[0-9a-fA-F]{6}$/ }]}
                    >
                      <Input type="color" style={{ width: 96, height: 40 }} />
                    </Form.Item>
                    <Form.Item name="side" label={tr("悬浮位置")} rules={required}>
                      <Select
                        options={[
                          { value: "right", label: tr("右侧") },
                          { value: "left", label: tr("左侧") },
                        ]}
                      />
                    </Form.Item>
                    <Space align="start" wrap>
                      <Form.Item
                        name="horizontal_offset"
                        label={tr("水平边距（像素）")}
                        rules={required}
                      >
                        <InputNumber min={8} max={200} precision={0} />
                      </Form.Item>
                      <Form.Item
                        name="bottom_offset"
                        label={tr("底部边距（像素）")}
                        rules={required}
                      >
                        <InputNumber min={8} max={200} precision={0} />
                      </Form.Item>
                    </Space>
                    <Space align="start" wrap>
                      <Form.Item name="width" label={tr("窗口宽度（像素）")} rules={required}>
                        <InputNumber min={320} max={800} precision={0} />
                      </Form.Item>
                      <Form.Item name="height" label={tr("窗口高度（像素）")} rules={required}>
                        <InputNumber min={400} max={1000} precision={0} />
                      </Form.Item>
                    </Space>
                    <p className="form-hint">
                      {tr("手机自动适配屏幕；访客可以放大、还原或收起窗口，收起不会中断会话。")}
                    </p>
                  </>
                ),
              },
              {
                key: "display",
                label: tr("展示时间"),
                forceRender: true,
                children: (
                  <>
                    <Form.Item
                      name="show_delay_seconds"
                      label={tr("按钮出现延时（秒）")}
                      rules={required}
                    >
                      <InputNumber min={0} max={3600} precision={0} />
                    </Form.Item>
                    <Form.Item
                      name="auto_open_delay_seconds"
                      label={tr("自动展开延时（秒）")}
                      extra={tr(
                        "-1 表示不自动展开；0 表示按钮出现后立即展开。每个标签页只自动展开一次。",
                      )}
                      rules={required}
                    >
                      <InputNumber min={-1} max={3600} precision={0} />
                    </Form.Item>
                    <Form.Item
                      name="schedule_enabled"
                      label={tr("限制展示时段")}
                      valuePropName="checked"
                    >
                      <Switch />
                    </Form.Item>
                    <Form.Item name="weekdays" label={tr("服务日期")} rules={required}>
                      <Checkbox.Group options={days.map((label, value) => ({ label, value }))} />
                    </Form.Item>
                    <Space align="start" wrap>
                      <Form.Item name="start" label={tr("开始时间")} rules={required}>
                        <Input type="time" required />
                      </Form.Item>
                      <Form.Item name="end" label={tr("结束时间")} rules={required}>
                        <Input type="time" required />
                      </Form.Item>
                    </Space>
                    <p className="form-hint">
                      {tr("时段按企业时区执行，支持跨午夜。时段外隐藏入口，已展开的咨询可以继续。")}
                    </p>
                  </>
                ),
              },
              {
                key: "code",
                label: tr("嵌入代码"),
                forceRender: true,
                children: (
                  <>
                    <p>
                      {tr("将这一行代码放入网站页面，保存配置后，访客下次加载即可使用最新设置。")}
                    </p>
                    <Input.TextArea
                      aria-label={tr("嵌入代码")}
                      rows={5}
                      readOnly
                      value={widgetCode(window.location.origin, link.entry_key)}
                    />
                    <Button style={{ marginTop: 16 }} onClick={() => void copy()}>
                      {tr("复制代码")}
                    </Button>
                    <p className="form-hint">
                      {tr(
                        "聊天页面仅在展开时加载。官网接入使用相同脚本，无需在页面中保存账号密码。",
                      )}
                    </p>
                  </>
                ),
              },
            ]}
          />
        </Form>
      )}
      <Button type="primary" loading={busy} disabled={!current} onClick={() => form.submit()}>
        {tr("保存")}
      </Button>
    </section>
  );
}
