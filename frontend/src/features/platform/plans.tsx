/** 文件作用说明：维护真实套餐版本、功能、价格、有效期和公开状态，以发布配置驱动官网。 */
"use client";
import { useState } from "react";
import { App, Button, Drawer, Form, Input, InputNumber, Space, Switch, Table, Tag } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import { api } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import { ErrorNotice, EmptyData } from "@/components/page-state";
import type { Resource } from "@/lib/types";
/** PlatformPlans 让平台管理员创建和调整真正公开可售的套餐。 */
export function PlatformPlans() {
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
        throw new Error("功能配置必须是合法 JSON 对象。");
      }
      if (
        !features ||
        typeof features !== "object" ||
        Array.isArray(features) ||
        Object.values(features).some((value) => typeof value !== "boolean")
      )
        throw new Error("功能配置必须是值为 true 或 false 的对象。");
      await api(`/platform/plans${editing?.id ? `/${editing.id}` : ""}`, {
        method: editing?.id ? "PATCH" : "POST",
        bodyJSON: { ...rest, features },
        idempotent: true,
      });
      message.success("套餐配置已保存");
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
        <h2>套餐版本与官网价格</h2>
        <p>维护版本价格、坐席、功能与有效期。</p>
      </div>
      <div className="resource-tools">
        <p className="muted">官网仅展示已启用并公开的套餐；修改价格与功能前请核对现有订阅政策。</p>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => edit(null)}>
          新增版本
        </Button>
      </div>
      <ErrorNotice error={plans.error} retry={plans.reload} />
      <Table
        rowKey="id"
        dataSource={plans.items}
        loading={plans.loading}
        scroll={{ x: 900 }}
        locale={{ emptyText: <EmptyData text="暂时没有套餐配置" /> }}
        columns={[
          {
            title: "版本",
            render: (_, row) => (
              <>
                <strong>{row.name}</strong>
                <div className="form-hint">{String(row.code || "")}</div>
              </>
            ),
          },
          {
            title: "价格",
            dataIndex: "price_amount",
            render: (value) => `¥${(Number(value) / 100).toLocaleString("zh-CN")}`,
          },
          { title: "客服坐席", dataIndex: "seat_limit" },
          {
            title: "有效期",
            dataIndex: "validity_days",
            render: (value) => (value === 0 ? "长期有效" : `${value} 天`),
          },
          {
            title: "公开状态",
            render: (_, row) => (
              <Space>
                <Tag color={row.is_active ? "blue" : "default"}>
                  {row.is_active ? "启用" : "停用"}
                </Tag>
                <Tag>{row.is_public ? "官网公开" : "内部"}</Tag>
              </Space>
            ),
          },
          {
            title: "操作",
            render: (_, row) => (
              <Button type="link" onClick={() => edit(row)}>
                编辑版本
              </Button>
            ),
          },
        ]}
      />
      <Drawer
        rootClassName="studio-drawer"
        title={editing ? "编辑套餐版本" : "新增套餐版本"}
        open={editing !== undefined}
        onClose={() => setEditing(undefined)}
        width={530}
        destroyOnHidden
      >
        <ErrorNotice error={error} />
        <Form layout="vertical" form={form} onFinish={save}>
          <Form.Item label="版本名称" name="name" rules={[{ required: true, whitespace: true }]}>
            <Input maxLength={80} />
          </Form.Item>
          <Form.Item
            label="版本编码"
            name="code"
            rules={[
              {
                required: true,
                pattern: /^[a-z][a-z0-9_-]{1,40}$/,
                message: "使用小写字母开头的2-41位字母、数字、横线或下划线",
              },
            ]}
          >
            <Input disabled={!!editing} />
          </Form.Item>
          <Form.Item label="说明" name="description">
            <Input.TextArea rows={3} maxLength={2000} />
          </Form.Item>
          <div className="two-columns">
            <Form.Item
              label="价格（分）"
              name="price_amount"
              rules={[{ required: true }]}
              extra="100 分 = 1 元，0 表示免费。"
            >
              <InputNumber
                min={0}
                precision={0}
                disabled={editing?.code === "free"}
                style={{ width: "100%" }}
              />
            </Form.Item>
            <Form.Item label="客服坐席上限" name="seat_limit" rules={[{ required: true }]}>
              <InputNumber
                min={1}
                precision={0}
                disabled={editing?.code === "free"}
                style={{ width: "100%" }}
              />
            </Form.Item>
            <Form.Item
              label="有效天数"
              name="validity_days"
              rules={[{ required: true }]}
              extra="0 表示长期有效。"
            >
              <InputNumber
                min={0}
                precision={0}
                disabled={editing?.code === "free"}
                style={{ width: "100%" }}
              />
            </Form.Item>
            <Form.Item label="展示顺序" name="sort_order">
              <InputNumber min={0} precision={0} style={{ width: "100%" }} />
            </Form.Item>
          </div>
          <Form.Item label="币种" name="currency">
            <Input readOnly />
          </Form.Item>
          <Form.Item
            label="功能配置 JSON"
            name="features_json"
            extra='例如 {"chat":true,"templates":true,"ai_design":false}'
          >
            <Input.TextArea className="code-editor" rows={8} />
          </Form.Item>
          <Space size={30}>
            <Form.Item label="启用版本" name="is_active" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item label="官网公开" name="is_public" valuePropName="checked">
              <Switch />
            </Form.Item>
          </Space>
          <div>
            <Button type="primary" htmlType="submit" loading={busy}>
              保存版本
            </Button>
          </div>
        </Form>
      </Drawer>
    </section>
  );
}
