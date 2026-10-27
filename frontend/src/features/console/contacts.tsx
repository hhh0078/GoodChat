/** 文件作用说明：维护客户分类、标签、自定义字段与资料，并读取访问轨迹及执行保留历史的封禁。 */
"use client";
import { useEffect, useState } from "react";
import {
  App,
  Avatar,
  Button,
  Drawer,
  Form,
  Input,
  Popconfirm,
  Space,
  Table,
  Tabs,
  Tag,
} from "antd";
import { PageTitle, ErrorNotice, EmptyData } from "@/components/page-state";
import { api, errorMessage } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import { useSession } from "@/features/auth/session";
import type { Resource } from "@/lib/types";
import { ResourcePanel } from "./resource-panel";
/** ContactsPage 独立整理客户档案，分组与分配仍在客服业务组中维护。 */
export function ContactsPage() {
  const categories = useResource<Resource>("/tenant/categories");
  const tags = useResource<Resource>("/tenant/tags");
  const tagCategories = useResource<Resource>("/tenant/tag-categories");
  const { tenantId } = useSession();
  const [selected, setSelected] = useState<Resource | null>(null);
  const [visits, setVisits] = useState<Resource[]>([]);
  const [error, setError] = useState<unknown>(null);
  const [reason, setReason] = useState("");
  const [revision, setRevision] = useState(0);
  const { message } = App.useApp();
  useEffect(() => {
    if (!selected) return;
    const controller = new AbortController();
    setVisits([]);
    setError(null);
    api<{ items: Resource[] }>(`/tenant/contacts/${selected.id}/visits`, {
      tenantId,
      signal: controller.signal,
    })
      .then((data) => setVisits(data.items || []))
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      });
    return () => controller.abort();
  }, [selected, tenantId]);
  /** block 提交封禁或解除，不删除历史聊天与访问轨迹。 */
  async function block(enabled: boolean) {
    if (!selected) return;
    try {
      await api(`/tenant/contacts/${selected.id}/block`, {
        method: enabled ? "POST" : "DELETE",
        tenantId,
        ...(enabled ? { bodyJSON: { reason } } : {}),
      });
      setSelected((previous) => (previous ? { ...previous, blocked: enabled } : null));
      setRevision((value) => value + 1);
      message.success(enabled ? "客户已封禁，聊天历史保留" : "封禁已解除");
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  return (
    <section className="console-screen console-contacts">
      <PageTitle
        title="客户资料"
        description="客户资料属于当前租户；公开手机号不作为历史会话访问凭证。"
      />
      <div className="table-card">
        <Tabs
          items={[
            {
              key: "contacts",
              label: "客户档案",
              children: (
                <ResourcePanel
                  key={revision}
                  title="客户"
                  path="/tenant/contacts"
                  columns={[
                    {
                      title: "客户名称",
                      dataIndex: "display_name",
                      render: (value) => (
                        <Space>
                          <Avatar size={32} className="console-person-avatar">
                            {String(value || "访").slice(0, 1)}
                          </Avatar>
                          <strong>{String(value || "访客")}</strong>
                        </Space>
                      ),
                    },
                    { title: "联系电话", dataIndex: "phone", render: (value) => value || "—" },
                    {
                      title: "分类",
                      dataIndex: "category_id",
                      render: (value) =>
                        categories.items.find((item) => item.id === value)?.name || "—",
                    },
                    {
                      title: "状态",
                      dataIndex: "blocked",
                      render: (value) => (
                        <Tag color={value ? "red" : "green"}>{value ? "已封禁" : "正常"}</Tag>
                      ),
                    },
                    {
                      title: "访问与封禁",
                      render: (_, item) => (
                        <Button
                          type="link"
                          size="small"
                          onClick={() => {
                            setSelected(item);
                            setReason("");
                          }}
                        >
                          查看客户
                        </Button>
                      ),
                    },
                  ]}
                  fields={[
                    { name: "display_name", label: "客户名称", required: true },
                    { name: "remark_name", label: "备注称呼" },
                    { name: "phone", label: "联系电话" },
                    {
                      name: "category_id",
                      label: "客户分类",
                      type: "select",
                      options: categories.items.map((item) => ({
                        label: item.name || item.id,
                        value: item.id,
                      })),
                    },
                    {
                      name: "tag_ids",
                      label: "客户标签",
                      type: "multi",
                      options: tags.items.map((item) => ({
                        label: item.name || item.id,
                        value: item.id,
                      })),
                    },
                    { name: "notes", label: "内部备注", type: "textarea" },
                    {
                      name: "attributes",
                      label: "已登记自定义资料 JSON",
                      type: "json",
                      initial: {},
                      hint: "字段必须已在自定义字段中登记，类型与选项由服务端校验。",
                    },
                  ]}
                />
              ),
            },
            {
              key: "categories",
              label: "客户分类",
              children: (
                <ResourcePanel
                  title="客户分类"
                  path="/tenant/categories"
                  onChanged={categories.reload}
                  fields={[{ name: "name", label: "分类名称", required: true }]}
                />
              ),
            },
            {
              key: "tags",
              label: "客户标签",
              children: (
                <ResourcePanel
                  title="客户标签"
                  path="/tenant/tags"
                  onChanged={tags.reload}
                  columns={[
                    {
                      title: "标签名称",
                      dataIndex: "name",
                      render: (value) => <Tag>{String(value || "—")}</Tag>,
                    },
                    {
                      title: "标签分类",
                      dataIndex: "category_id",
                      render: (value) =>
                        tagCategories.items.find((item) => item.id === value)?.name || "未分类",
                    },
                    {
                      title: "颜色",
                      dataIndex: "color",
                      render: (value) => (
                        <code className="console-field-key">{String(value || "—")}</code>
                      ),
                    },
                  ]}
                  fields={[
                    { name: "name", label: "标签名称", required: true },
                    {
                      name: "category_id",
                      label: "标签分类",
                      type: "select",
                      options: tagCategories.items.map((item) => ({
                        label: item.name || item.id,
                        value: item.id,
                      })),
                    },
                    { name: "color", label: "标签颜色", initial: "#23684e" },
                  ]}
                />
              ),
            },
            {
              key: "tag-categories",
              label: "标签分类",
              children: (
                <ResourcePanel
                  title="标签分类"
                  path="/tenant/tag-categories"
                  onChanged={tagCategories.reload}
                  fields={[{ name: "name", label: "分类名称", required: true }]}
                />
              ),
            },
            {
              key: "fields",
              label: "自定义字段",
              children: (
                <ResourcePanel
                  title="自定义字段"
                  path="/tenant/custom-fields"
                  columns={[
                    { title: "显示名称", dataIndex: "name" },
                    {
                      title: "字段键名",
                      dataIndex: "field_key",
                      render: (value) => (
                        <code className="console-field-key">{String(value || "—")}</code>
                      ),
                    },
                    {
                      title: "字段类型",
                      dataIndex: "field_type",
                      render: (value) =>
                        (
                          ({
                            text: "文本",
                            number: "数字",
                            boolean: "开关",
                            date: "日期",
                            select: "单选",
                            multiselect: "多选",
                          }) as Record<string, string>
                        )[String(value)] || String(value || "—"),
                    },
                  ]}
                  fields={[
                    { name: "name", label: "显示名称", required: true },
                    {
                      name: "field_key",
                      label: "字段键名",
                      required: true,
                      hint: "例如 order_id，供业务属性传入使用。",
                    },
                    {
                      name: "field_type",
                      label: "字段类型",
                      type: "select",
                      initial: "text",
                      required: true,
                      options: [
                        { label: "文本", value: "text" },
                        { label: "数字", value: "number" },
                        { label: "开关", value: "boolean" },
                        { label: "日期", value: "date" },
                        { label: "单选", value: "select" },
                        { label: "多选", value: "multiselect" },
                      ],
                    },
                    {
                      name: "options",
                      label: "选择项 JSON 数组",
                      type: "json",
                      initial: [],
                      hint: '单选与多选示例：["售前","售后"]',
                    },
                  ]}
                />
              ),
            },
          ]}
        />
      </div>
      <Drawer
        rootClassName="console-drawer"
        title="客户访问与封禁管理"
        open={!!selected}
        onClose={() => setSelected(null)}
        width={680}
      >
        <Space>
          <strong>{String(selected?.display_name || "访客")}</strong>
          <Tag color={selected?.blocked ? "red" : "green"}>
            {selected?.blocked ? "已封禁" : "正常"}
          </Tag>
        </Space>
        <p className="muted">联系电话：{String(selected?.phone || "未填写")}</p>
        {selected?.notes ? (
          <div className="console-customer-note">
            <strong>内部备注</strong>
            <p>{String(selected.notes)}</p>
          </div>
        ) : null}
        <Form layout="vertical">
          <Form.Item label="封禁原因">
            <Input.TextArea
              rows={3}
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              maxLength={500}
            />
          </Form.Item>
          {selected?.blocked ? (
            <Popconfirm title="解除该客户封禁？" onConfirm={() => block(false)}>
              <Button>解除封禁</Button>
            </Popconfirm>
          ) : (
            <Popconfirm
              title="封禁该客户？"
              description="会终止其当前咨询并撤销访客身份，历史记录保留。"
              onConfirm={() => block(true)}
            >
              <Button danger>封禁客户</Button>
            </Popconfirm>
          )}
        </Form>
        <h3 style={{ marginTop: 28 }}>访问轨迹</h3>
        <ErrorNotice error={error} />
        <Table
          rowKey="id"
          dataSource={visits}
          size="small"
          locale={{ emptyText: <EmptyData text="暂无访问轨迹" /> }}
          columns={[
            { title: "入口", dataIndex: "access_link_id", ellipsis: true },
            {
              title: "来源",
              dataIndex: "tracking",
              render: (value) => String(value?.channel_code || value?.utm_source || "未标记"),
            },
            {
              title: "访问时间",
              dataIndex: "created_at",
              render: (value) => new Date(String(value)).toLocaleString("zh-CN"),
            },
          ]}
        />
      </Drawer>
    </section>
  );
}
