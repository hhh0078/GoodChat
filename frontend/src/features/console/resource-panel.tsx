/** 文件作用说明：提供真实接口驱动的通用资源列表、表单编辑与归档交互。 */
"use client";
import { useId, useMemo, useState, type ReactNode } from "react";
import {
  App,
  Button,
  Drawer,
  Form,
  Input,
  InputNumber,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
} from "antd";
import { PlusOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { api, errorMessage } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import type { Resource } from "@/lib/types";
import { EmptyData, ErrorNotice } from "@/components/page-state";
import filterStyles from "@/components/filter-toolbar.module.css";

export type FieldDefinition = {
  name: string;
  label: string;
  type?: "text" | "json" | "textarea" | "select" | "multi" | "number" | "switch";
  required?: boolean;
  options?: { label: string; value: string }[];
  hint?: string;
  initial?: unknown;
  min?: number;
  max?: number;
};
type Props = {
  title: string;
  path: string;
  fields: FieldDefinition[];
  columns?: ColumnsType<Resource>;
  platform?: boolean;
  help?: string;
  onChanged?: () => void;
  canEdit?: (item: Resource) => boolean;
  searchFields?: { value: string; label: string }[];
  tools?: ReactNode;
  canCreate?: boolean;
  tableMinWidth?: number;
};
/** ResourcePanel 使用统一状态维护资源的查询、新增、编辑与归档。 */
export function ResourcePanel({
  title,
  path,
  fields,
  columns,
  platform = false,
  help,
  onChanged,
  canEdit,
  searchFields,
  tools,
  canCreate = true,
  tableMinWidth = 640,
}: Props) {
  const [query, setQuery] = useState("");
  const [filters, setFilters] = useState<Record<string, string>>({});
  const [submittedFilters, setSubmittedFilters] = useState<Record<string, string>>({});
  const filterId = useId();
  const resource = useResource<Resource>(
    path + (searchFields ? `?${new URLSearchParams(submittedFilters)}` : ""),
    platform,
  );
  const [editing, setEditing] = useState<Resource | null | undefined>();
  const [saving, setSaving] = useState(false);
  const [submitError, setSubmitError] = useState<unknown>(null);
  const [form] = Form.useForm();
  const { message } = App.useApp();
  const visible = useMemo(
    () =>
      searchFields
        ? resource.items
        : resource.items.filter((item) =>
            JSON.stringify(item).toLowerCase().includes(query.toLowerCase()),
          ),
    [resource.items, query, searchFields],
  );
  /** openEditor 初始化字段默认值，保留已有字段内容。 */
  function openEditor(item: Resource | null) {
    setSubmitError(null);
    form.resetFields();
    const defaults = Object.fromEntries(fields.map((field) => [field.name, field.initial]));
    const initial = { ...defaults, ...item };
    for (const field of fields)
      if (field.type === "json" && typeof initial[field.name] !== "string")
        initial[field.name] = JSON.stringify(initial[field.name] ?? {}, null, 2);
    form.setFieldsValue(initial);
    setEditing(item);
  }
  /** save 真实提交资源，失败保留表单并展示后端原因。 */
  async function save(values: Record<string, unknown>) {
    setSaving(true);
    setSubmitError(null);
    try {
      const body = { ...values };
      for (const field of fields) {
        if (field.type === "json") {
          try {
            body[field.name] = JSON.parse(String(values[field.name] || "{}"));
          } catch {
            throw new Error(`${field.label}必须是合法JSON`);
          }
        } else if (editing?.id && field.name.endsWith("_id") && body[field.name] === undefined)
          body[field.name] = null;
      }
      await api(`${path}${editing?.id ? `/${editing.id}` : ""}`, {
        method: editing?.id ? "PATCH" : "POST",
        tenantId: platform ? undefined : resource.tenantId,
        bodyJSON: body,
        idempotent: true,
      });
      message.success(editing ? "修改已保存" : "已创建");
      setEditing(undefined);
      resource.reload();
      onChanged?.();
    } catch (problem) {
      setSubmitError(problem);
    } finally {
      setSaving(false);
    }
  }
  /** archive 提交服务端归档，不能以本地删除代替保存。 */
  async function archive(item: Resource) {
    try {
      await api(`${path}/${item.id}`, {
        method: "DELETE",
        tenantId: platform ? undefined : resource.tenantId,
      });
      message.success("已归档");
      resource.reload();
      onChanged?.();
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  const tableColumns: ColumnsType<Resource> = [
    ...(columns || [{ title: "名称", dataIndex: "name", key: "name" }]),
    {
      title: "操作",
      key: "action",
      width: 150,
      render: (_, item) =>
        canEdit && !canEdit(item) ? (
          <span className="muted">仅可使用</span>
        ) : (
          <Space>
            <Button type="link" size="small" onClick={() => openEditor(item)}>
              编辑
            </Button>
            <Popconfirm
              title={`归档这条${title}记录？`}
              description="已有引用和历史以服务端校验结果为准。"
              onConfirm={() => archive(item)}
            >
              <Button type="link" danger size="small">
                归档
              </Button>
            </Popconfirm>
          </Space>
        ),
    },
  ];
  return (
    <section className="console-resource-panel">
      <form
        className={filterStyles.toolbar}
        aria-label={`${title}筛选`}
        onSubmit={(event) => {
          event.preventDefault();
          setSubmittedFilters({ ...filters });
        }}
      >
        {searchFields ? (
          <>
            {searchFields.map((field) => (
              <label
                className={filterStyles.field}
                htmlFor={`${filterId}-${field.value}`}
                key={field.value}
              >
                <span>{field.label}</span>
                <Input
                  id={`${filterId}-${field.value}`}
                  aria-label={`筛选${field.label}`}
                  variant="borderless"
                  allowClear
                  maxLength={200}
                  value={filters[field.value] || ""}
                  onChange={(event) =>
                    setFilters((previous) => ({
                      ...previous,
                      [field.value]: event.target.value,
                    }))
                  }
                />
              </label>
            ))}
            <Button type="primary" htmlType="submit">
              搜索
            </Button>
            <Button
              onClick={() => {
                setFilters({});
                setSubmittedFilters({});
              }}
            >
              重置
            </Button>
          </>
        ) : (
          <Input
            className={filterStyles.search}
            aria-label={`搜索${title}`}
            placeholder={`搜索${title}`}
            allowClear
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        )}
        <div className={filterStyles.actions}>
          {canCreate && (
            <Button type="primary" icon={<PlusOutlined />} onClick={() => openEditor(null)}>
              新增{title}
            </Button>
          )}
          {tools}
        </div>
      </form>
      {help && (
        <p className="muted" style={{ fontSize: 13 }}>
          {help}
        </p>
      )}
      <ErrorNotice error={resource.error} retry={resource.reload} />
      <Table
        rowKey="id"
        columns={tableColumns}
        dataSource={visible}
        loading={resource.loading}
        scroll={{ x: tableMinWidth }}
        locale={{ emptyText: <EmptyData text={`还没有${title}，可以先创建一条。`} /> }}
        pagination={{
          pageSize: 10,
          showSizeChanger: false,
          showTotal: (total) => `${total} 条记录`,
        }}
      />
      <Drawer
        rootClassName="console-drawer"
        title={`${editing ? "编辑" : "新增"}${title}`}
        open={editing !== undefined}
        onClose={() => setEditing(undefined)}
        width={480}
        destroyOnHidden
      >
        <ErrorNotice error={submitError} />
        <Form form={form} layout="vertical" onFinish={save} requiredMark={false}>
          {fields.map((field) => (
            <Form.Item
              name={field.name}
              label={field.label}
              key={field.name}
              valuePropName={field.type === "switch" ? "checked" : "value"}
              rules={field.required ? [{ required: true, message: `请填写${field.label}` }] : []}
              extra={field.hint}
            >
              {field.type === "textarea" || field.type === "json" ? (
                <Input.TextArea rows={4} maxLength={6000} />
              ) : field.type === "number" ? (
                <InputNumber
                  min={field.min ?? 0}
                  max={field.max ?? 86400}
                  precision={0}
                  style={{ width: "100%" }}
                />
              ) : field.type === "select" || field.type === "multi" ? (
                <Select
                  allowClear
                  mode={field.type === "multi" ? "multiple" : undefined}
                  options={field.options}
                  placeholder={`请选择${field.label}`}
                />
              ) : field.type === "switch" ? (
                <Switch />
              ) : (
                <Input maxLength={200} />
              )}
            </Form.Item>
          ))}
          <Space>
            <Button type="primary" htmlType="submit" loading={saving}>
              保存
            </Button>
            <Button onClick={() => setEditing(undefined)}>取消</Button>
          </Space>
        </Form>
      </Drawer>
    </section>
  );
}
