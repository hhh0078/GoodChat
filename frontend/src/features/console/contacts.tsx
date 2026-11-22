/** 文件作用说明：维护客户分类、标签、自定义字段与资料，并读取访问轨迹及执行保留历史的封禁。 */
"use client";
import { showRequestError } from "@/lib/request-error";
import { EmptyData, ErrorNotice, PageTitle } from "@/components/page-state";
import { useSession } from "@/features/auth/session";
import { useFormats } from "@/i18n/use-formats";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import type { Resource } from "@/lib/types";
import { useResource } from "@/lib/use-resource";
import { channelTags, customerFields, displayValue } from "@/lib/workspace-display";
import {
  App,
  Avatar,
  Button,
  Drawer,
  Dropdown,
  Form,
  Input,
  Popconfirm,
  Space,
  Table,
  Tag,
} from "antd";
import { useEffect, useState } from "react";
import { ResourcePanel } from "./resource-panel";
/** ContactsPage 独立整理客户档案，分组与分配仍在客服业务组中维护。 */
export function ContactsPage() {
  const { currentPreferences, numberFormatter } = useFormats();

  /** contactTime 格式化真实建档与访问时间，缺值保留占位而不伪造时间。 */
  function contactTime(value: unknown): string {
    return typeof value === "string" && Number.isFinite(new Date(value).getTime())
      ? new Date(value).toLocaleString(currentPreferences().locale, {
          timeZone: currentPreferences().timezone,
          ...{
            month: "2-digit",
            day: "2-digit",
            hour: "2-digit",
            minute: "2-digit",
          },
        })
      : "—";
  }

  const tr = useTranslator();
  const categories = useResource<Resource>("/tenant/categories");
  const tags = useResource<Resource>("/tenant/tags");
  const tagCategories = useResource<Resource>("/tenant/tag-categories");
  const { tenantId, current } = useSession();
  const manages = current.memberships.some(
    (member) => member.tenant_id === tenantId && ["owner", "admin"].includes(member.role),
  );
  const [management, setManagement] = useState("");
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
    api<{
      items: Resource[];
    }>(`/tenant/contacts/${selected.id}/visits`, {
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
      message.success(enabled ? tr("客户已封禁，聊天历史保留") : tr("封禁已解除"));
    } catch (problem) {
      showRequestError(problem);
    }
  }
  const panels = [
    {
      key: "contacts",
      label: tr("客户档案"),
      children: (
        <ResourcePanel
          key={revision}
          title={tr("客户")}
          path="/tenant/contacts"
          tableMinWidth={1020}
          canCreate={manages}
          canEdit={() => manages}
          searchFields={[
            { value: "name", label: tr("名称") },
            { value: "phone", label: tr("电话") },
            { value: "channel", label: tr("渠道") },
            { value: "extra", label: tr("资料") },
            { value: "tag", label: tr("标签") },
            { value: "category", label: tr("分类") },
          ]}
          tools={
            manages && (
              <Dropdown
                trigger={["click"]}
                menu={{
                  items: [
                    { key: "categories", label: tr("客户分类") },
                    { key: "tags", label: tr("客户标签") },
                    { key: "tag-categories", label: tr("标签分类") },
                    { key: "fields", label: tr("自定义字段") },
                  ],
                  onClick: ({ key }) => setManagement(key),
                }}
              >
                <Button>{tr("资料管理")}</Button>
              </Dropdown>
            )
          }
          columns={[
            {
              title: tr("客户"),
              dataIndex: "display_name",
              width: 220,
              render: (value, row) => (
                <Space align="start" size={10}>
                  <Avatar
                    size={38}
                    src={typeof row.avatar_url === "string" ? row.avatar_url : undefined}
                  >
                    {String(value || tr("访")).slice(0, 1)}
                  </Avatar>
                  <div className="contact-list-identity">
                    <strong>{String(value || tr("访客"))}</strong>
                    <span>{displayValue(row.phone)}</span>
                    {row.remark_name ? <small>{String(row.remark_name)}</small> : null}
                    {row.blocked ? <Tag color="red">{tr("已封禁")}</Tag> : null}
                  </div>
                </Space>
              ),
            },
            {
              title: tr("客户信息"),
              width: 320,
              render: (_, row) => (
                <div className="contact-list-information">
                  <div>
                    {row.category_id ? (
                      <Tag>
                        {categories.items.find((item) => item.id === row.category_id)?.name ||
                          tr("未分类")}
                      </Tag>
                    ) : null}
                    {channelTags(row.last_source).map((channel) => (
                      <Tag color="blue" key={channel}>
                        {channel}
                      </Tag>
                    ))}
                    {Array.isArray(row.tags)
                      ? row.tags.map((tag: Resource) => (
                          <Tag key={tag.id}>{String(tag.name || "")}</Tag>
                        ))
                      : null}
                  </div>
                  <div>
                    {Object.entries(customerFields(row.attributes, row.last_context))
                      .filter(([key]) => key !== "page_url")
                      .map(([key, value]) => (
                        <Tag key={key} title={`${key}：${displayValue(value)}`}>
                          {displayValue(value)}
                        </Tag>
                      ))}
                  </div>
                </div>
              ),
            },
            {
              title: tr("时间与访问"),
              width: 185,
              render: (_, row) => (
                <div className="contact-list-times">
                  <span>
                    {tr("建档 ")}
                    {contactTime(row.created_at)}
                  </span>
                  <span>
                    {tr("最近 ")}
                    {contactTime(row.last_visit_at)}
                  </span>
                  <small>
                    {tr("访问 ")}
                    {String(row.visit_count ?? 0)}
                    {tr(" 次")}
                  </small>
                </div>
              ),
            },
            {
              title: tr("访问与封禁"),
              width: 130,
              render: (_, item) => (
                <Button
                  type="link"
                  size="small"
                  onClick={() => {
                    setSelected(item);
                    setReason("");
                  }}
                >
                  {tr("查看客户 ")}
                </Button>
              ),
            },
          ]}
          fields={[
            { name: "display_name", label: tr("客户名称"), required: true },
            { name: "remark_name", label: tr("备注称呼") },
            { name: "phone", label: tr("联系电话") },
            {
              name: "category_id",
              label: tr("客户分类"),
              type: "select",
              options: categories.items.map((item) => ({
                label: item.name || item.id,
                value: item.id,
              })),
            },
            {
              name: "tag_ids",
              label: tr("客户标签"),
              type: "multi",
              options: tags.items.map((item) => ({
                label: item.name || item.id,
                value: item.id,
              })),
            },
            { name: "notes", label: tr("内部备注"), type: "textarea" },
            {
              name: "attributes",
              label: tr("已登记自定义资料 JSON"),
              type: "json",
              initial: {},
              hint: tr("字段必须已在自定义字段中登记，类型与选项由服务端校验。"),
            },
          ]}
        />
      ),
    },
    {
      key: "categories",
      label: tr("客户分类"),
      children: (
        <ResourcePanel
          title={tr("客户分类")}
          path="/tenant/categories"
          onChanged={categories.reload}
          fields={[{ name: "name", label: tr("分类名称"), required: true }]}
        />
      ),
    },
    {
      key: "tags",
      label: tr("客户标签"),
      children: (
        <ResourcePanel
          title={tr("客户标签")}
          path="/tenant/tags"
          onChanged={tags.reload}
          columns={[
            {
              title: tr("标签名称"),
              dataIndex: "name",
              render: (value) => <Tag>{String(value || "—")}</Tag>,
            },
            {
              title: tr("标签分类"),
              dataIndex: "category_id",
              render: (value) =>
                tagCategories.items.find((item) => item.id === value)?.name || tr("未分类"),
            },
            {
              title: tr("颜色"),
              dataIndex: "color",
              render: (value) => <code className="console-field-key">{String(value || "—")}</code>,
            },
          ]}
          fields={[
            { name: "name", label: tr("标签名称"), required: true },
            {
              name: "category_id",
              label: tr("标签分类"),
              type: "select",
              options: tagCategories.items.map((item) => ({
                label: item.name || item.id,
                value: item.id,
              })),
            },
            { name: "color", label: tr("标签颜色"), initial: "#23684e" },
          ]}
        />
      ),
    },
    {
      key: "tag-categories",
      label: tr("标签分类"),
      children: (
        <ResourcePanel
          title={tr("标签分类")}
          path="/tenant/tag-categories"
          onChanged={tagCategories.reload}
          fields={[{ name: "name", label: tr("分类名称"), required: true }]}
        />
      ),
    },
    {
      key: "fields",
      label: tr("自定义字段"),
      children: (
        <ResourcePanel
          title={tr("自定义字段")}
          path="/tenant/custom-fields"
          columns={[
            { title: tr("显示名称"), dataIndex: "name" },
            {
              title: tr("字段键名"),
              dataIndex: "field_key",
              render: (value) => <code className="console-field-key">{String(value || "—")}</code>,
            },
            {
              title: tr("字段类型"),
              dataIndex: "field_type",
              render: (value) =>
                (
                  ({
                    text: tr("文本"),
                    number: tr("数字"),
                    boolean: tr("开关"),
                    date: tr("日期"),
                    select: tr("单选"),
                    multiselect: tr("多选"),
                  }) as Record<string, string>
                )[String(value)] || String(value || "—"),
            },
          ]}
          fields={[
            { name: "name", label: tr("显示名称"), required: true },
            {
              name: "field_key",
              label: tr("字段键名"),
              required: true,
              hint: tr("例如 order_id，供业务属性传入使用。"),
            },
            {
              name: "field_type",
              label: tr("字段类型"),
              type: "select",
              initial: "text",
              required: true,
              options: [
                { label: tr("文本"), value: "text" },
                { label: tr("数字"), value: "number" },
                { label: tr("开关"), value: "boolean" },
                { label: tr("日期"), value: "date" },
                { label: tr("单选"), value: "select" },
                { label: tr("多选"), value: "multiselect" },
              ],
            },
            {
              name: "options",
              label: tr("选择项 JSON 数组"),
              type: "json",
              initial: [],
              hint: tr('单选与多选示例：["售前","售后"]'),
            },
          ]}
        />
      ),
    },
  ];
  return (
    <section className="console-screen console-contacts">
      <PageTitle
        title={tr("客户资料")}
        description={tr("按名称、电话、渠道等条件组合搜索客户。")}
      />
      <div className="table-card">
        {panels[0].children}
        <Drawer
          rootClassName="console-drawer"
          title={panels.find((panel) => panel.key === management)?.label}
          open={!!management}
          onClose={() => setManagement("")}
          width={860}
        >
          {panels.find((panel) => panel.key === management)?.children}
        </Drawer>
      </div>
      <Drawer
        rootClassName="console-drawer"
        title={tr("客户访问与封禁管理")}
        open={!!selected}
        onClose={() => setSelected(null)}
        width={680}
      >
        <Space>
          <strong>{String(selected?.display_name || tr("访客"))}</strong>
          <Tag color={selected?.blocked ? "red" : "green"}>
            {selected?.blocked ? tr("已封禁") : tr("正常")}
          </Tag>
        </Space>
        <p className="muted">
          {tr("联系电话：")}
          {String(selected?.phone || tr("未填写"))}
        </p>
        {selected?.notes ? (
          <div className="console-customer-note">
            <strong>{tr("内部备注")}</strong>
            <p>{String(selected.notes)}</p>
          </div>
        ) : null}
        <Form layout="vertical">
          <Form.Item label={tr("封禁原因")}>
            <Input.TextArea
              rows={3}
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              maxLength={500}
            />
          </Form.Item>
          {selected?.blocked ? (
            <Popconfirm title={tr("解除该客户封禁？")} onConfirm={() => block(false)}>
              <Button>{tr("解除封禁")}</Button>
            </Popconfirm>
          ) : (
            <Popconfirm
              title={tr("封禁该客户？")}
              description={tr("会终止其当前咨询并撤销访客身份，历史记录保留。")}
              onConfirm={() => block(true)}
            >
              <Button danger>{tr("封禁客户")}</Button>
            </Popconfirm>
          )}
        </Form>
        <h3 style={{ marginTop: 28 }}>{tr("访问轨迹")}</h3>
        <ErrorNotice error={error} />
        <Table
          rowKey="id"
          dataSource={visits}
          size="small"
          locale={{ emptyText: <EmptyData text={tr("暂无访问轨迹")} /> }}
          columns={[
            { title: tr("入口"), dataIndex: "access_link_id", ellipsis: true },
            {
              title: tr("来源"),
              dataIndex: "tracking",
              render: (value) => String(value?.channel_code || value?.utm_source || tr("未标记")),
            },
            {
              title: tr("访问时间"),
              dataIndex: "created_at",
              render: (value) =>
                new Date(String(value)).toLocaleString(currentPreferences().locale, {
                  timeZone: currentPreferences().timezone,
                }),
            },
          ]}
        />
      </Drawer>
    </section>
  );
}
