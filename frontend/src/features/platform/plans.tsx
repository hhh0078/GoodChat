/** 文件作用说明：维护真实套餐版本、功能、价格、有效期和公开状态，以发布配置驱动官网。 */
"use client";
import { EmptyData, ErrorNotice } from "@/components/page-state";
import { ContentTranslations } from "@/i18n/content-translations";
import { useFormats } from "@/i18n/use-formats";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import type { Resource } from "@/lib/types";
import { useResource } from "@/lib/use-resource";
import { PlusOutlined } from "@ant-design/icons";
import { App, Button, Drawer, Form, Input, InputNumber, Space, Switch, Table, Tag } from "antd";
import { useState } from "react";
/** PlatformPlans 让平台管理员创建和调整真正公开可售的套餐。 */
export function PlatformPlans() {
  const { currentPreferences, numberFormatter } = useFormats();

  const tr = useTranslator();
  const plans = useResource<Resource>("/platform/plans", true);
  const [editing, setEditing] = useState<Resource | null | undefined>();
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [form] = Form.useForm();
  const { message } = App.useApp();
  /** edit 填入套餐配置，不生成未经确认的收费价格。 */
  function edit(plan: Resource | null) {
    setError(null);
    form.resetFields();
    form.setFieldsValue(
      plan
        ? { ...plan, features_json: JSON.stringify(plan.features || {}, null, 2) }
        : {
            price_amount: 0,
            currency: "CNY",
            validity_days: 0,
            seat_limit: 2,
            is_public: false,
            is_active: true,
            sort_order: 0,
            features_json: "{}",
          },
    );
    setEditing(plan);
  }
  /** save 校验功能 JSON 与金额后提交真实平台接口。 */
  async function save(values: Record<string, unknown>) {
    setBusy(true);
    setError(null);
    try {
      const { features_json, ...rest } = values;
      let features: unknown;
      try {
        features = JSON.parse(String(features_json || "{}"));
      } catch {
        throw new Error(tr("功能配置必须是合法 JSON 对象。"));
      }
      if (
        !features ||
        typeof features !== "object" ||
        Array.isArray(features) ||
        Object.values(features).some((value) => typeof value !== "boolean")
      )
        throw new Error(tr("功能配置必须是值为 true 或 false 的对象。"));
      await api(`/platform/plans${editing?.id ? `/${editing.id}` : ""}`, {
        method: editing?.id ? "PATCH" : "POST",
        bodyJSON: { ...rest, features },
        idempotent: true,
      });
      message.success(tr("套餐配置已保存"));
      setEditing(undefined);
      plans.reload();
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="studio-platform-section">
      <div className="studio-panel-heading">
        <h2>{tr("套餐版本与官网价格")}</h2>
        <p>{tr("维护版本价格、坐席、功能与有效期。")}</p>
      </div>
      <div className="resource-tools">
        <p className="muted">
          {tr("官网仅展示已启用并公开的套餐；修改价格与功能前请核对现有订阅政策。")}
        </p>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => edit(null)}>
          {tr("新增版本 ")}
        </Button>
      </div>
      <ErrorNotice error={plans.error} retry={plans.reload} />
      <Table
        rowKey="id"
        dataSource={plans.items}
        loading={plans.loading}
        scroll={{ x: 900 }}
        locale={{ emptyText: <EmptyData text={tr("暂时没有套餐配置")} /> }}
        columns={[
          {
            title: tr("版本"),
            render: (_, row) => (
              <>
                <strong>{row.name}</strong>
                <div className="form-hint">{String(row.code || "")}</div>
              </>
            ),
          },
          {
            title: tr("价格"),
            dataIndex: "price_amount",
            render: (value) =>
              `¥${(Number(value) / 100).toLocaleString(currentPreferences().locale)}`,
          },
          { title: tr("客服坐席"), dataIndex: "seat_limit" },
          {
            title: tr("有效期"),
            dataIndex: "validity_days",
            render: (value) =>
              value === 0 ? tr("长期有效") : tr("{p0} 天", { p0: String(value) }),
          },
          {
            title: tr("公开状态"),
            render: (_, row) => (
              <Space>
                <Tag color={row.is_active ? "blue" : "default"}>
                  {row.is_active ? tr("启用") : tr("停用")}
                </Tag>
                <Tag>{row.is_public ? tr("官网公开") : tr("内部")}</Tag>
              </Space>
            ),
          },
          {
            title: tr("操作"),
            render: (_, row) => (
              <Button type="link" onClick={() => edit(row)}>
                {tr("编辑版本 ")}
              </Button>
            ),
          },
        ]}
      />
      <Drawer
        rootClassName="studio-drawer"
        title={editing ? tr("编辑套餐版本") : tr("新增套餐版本")}
        open={editing !== undefined}
        onClose={() => setEditing(undefined)}
        width={530}
        destroyOnHidden
      >
        <ErrorNotice error={error} />
        <Form layout="vertical" form={form} onFinish={save}>
          <Form.Item
            label={tr("版本名称")}
            name="name"
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={80} />
          </Form.Item>
          <Form.Item
            label={tr("版本编码")}
            name="code"
            rules={[
              {
                required: true,
                pattern: /^[a-z][a-z0-9_-]{1,40}$/,
                message: tr("使用小写字母开头的2-41位字母、数字、横线或下划线"),
              },
            ]}
          >
            <Input disabled={!!editing} />
          </Form.Item>
          <Form.Item label={tr("说明")} name="description">
            <Input.TextArea rows={3} maxLength={2000} />
          </Form.Item>
          <ContentTranslations />
          <div className="two-columns">
            <Form.Item
              label={tr("价格（分）")}
              name="price_amount"
              rules={[{ required: true }]}
              extra={tr("100 分 = 1 元，0 表示免费。")}
            >
              <InputNumber
                min={0}
                precision={0}
                disabled={editing?.code === "free"}
                style={{ width: "100%" }}
              />
            </Form.Item>
            <Form.Item label={tr("客服坐席上限")} name="seat_limit" rules={[{ required: true }]}>
              <InputNumber
                min={1}
                precision={0}
                disabled={editing?.code === "free"}
                style={{ width: "100%" }}
              />
            </Form.Item>
            <Form.Item
              label={tr("有效天数")}
              name="validity_days"
              rules={[{ required: true }]}
              extra={tr("0 表示长期有效。")}
            >
              <InputNumber
                min={0}
                precision={0}
                disabled={editing?.code === "free"}
                style={{ width: "100%" }}
              />
            </Form.Item>
            <Form.Item label={tr("展示顺序")} name="sort_order">
              <InputNumber min={0} precision={0} style={{ width: "100%" }} />
            </Form.Item>
          </div>
          <Form.Item label={tr("币种")} name="currency">
            <Input readOnly />
          </Form.Item>
          <Form.Item
            label={tr("功能配置 JSON")}
            name="features_json"
            extra={tr('例如 {"chat":true,"templates":true,"ai_design":false}')}
          >
            <Input.TextArea className="code-editor" rows={8} />
          </Form.Item>
          <Space size={30}>
            <Form.Item label={tr("启用版本")} name="is_active" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item label={tr("官网公开")} name="is_public" valuePropName="checked">
              <Switch />
            </Form.Item>
          </Space>
          <div>
            <Button type="primary" htmlType="submit" loading={busy}>
              {tr("保存版本 ")}
            </Button>
          </div>
        </Form>
      </Drawer>
    </section>
  );
}
