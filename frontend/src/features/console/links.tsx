/** 文件作用说明：管理接入链接、生成二维码和参数化链接，并调用真实入口接口保存。 */
"use client";
import { useState } from "react";
import {
  App,
  Button,
  Checkbox,
  Drawer,
  Form,
  Input,
  Tabs,
  Popconfirm,
  QRCode,
  Select,
  Space,
  Table,
  Tag,
} from "antd";
import { CopyOutlined, PlusOutlined, QrcodeOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { api, errorMessage } from "@/lib/api";
import { entryURL } from "@/lib/urls";
import { useResource } from "@/lib/use-resource";
import type { AccessLink, Member, Resource } from "@/lib/types";
import { PageTitle, ErrorNotice, EmptyData } from "@/components/page-state";

const parameterExamples: Record<string, string> = {
  姓名: "张三",
  手机号: "13800000000",
  头像: "https://example.com/avatar.png",
  性别: "男",
  extra: '{"有无房产":"有","是否逾期":"无"}',
};

/** LinksPage 为独立业务入口维护路由范围并生成分享与嵌入内容。 */
export function LinksPage() {
  const links = useResource<AccessLink>("/tenant/access-links");
  const groups = useResource<Resource>("/tenant/groups");
  const pools = useResource<Resource>("/tenant/routing-pools");
  const skills = useResource<Resource>("/tenant/routing-skills");
  const members = useResource<Member>("/tenant/members");
  const [editing, setEditing] = useState<AccessLink | null | undefined>();
  const [sharing, setSharing] = useState<AccessLink | null>(null);
  const [parameterKeys, setParameterKeys] = useState<string[]>([]);
  const [parameterValues, setParameterValues] = useState(parameterExamples);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [form] = Form.useForm();
  const { message } = App.useApp();
  /** link 通过当前站点生成标准公开聊天地址。 */
  function link(item: AccessLink) {
    const profile = Object.fromEntries(parameterKeys.map((key) => [key, parameterValues[key]]));
    return typeof window === "undefined"
      ? `/c/${item.entry_key}`
      : entryURL(window.location.origin, item.entry_key, profile);
  }
  let parameterError = "";
  if (parameterKeys.includes("extra")) {
    try {
      const extra = JSON.parse(parameterValues.extra);
      if (!extra || typeof extra !== "object" || Array.isArray(extra))
        parameterError = "extra 必须是 JSON 对象";
    } catch {
      parameterError = "extra 必须是合法 JSON 对象";
    }
  }
  if (!parameterError && sharing && new TextEncoder().encode(link(sharing)).length > 2048)
    parameterError = "链接超过2 KiB，请减少资料，或改用POST初始化接口传递较大的JSON。";
  /** edit 将已有入口或初始配置填入真实保存表单。 */
  function edit(item: AccessLink | null) {
    setError(null);
    form.resetFields();
    form.setFieldsValue(
      item
        ? {
            ...item,
            reception_mode: item.reception_mode || "inherit",
            channel_tags: item.channel_tags || (item.channel_code ? [item.channel_code] : []),
          }
        : {
            status: "active",
            agent_mode: "prefer",
            reception_mode: "auto",
            channel_tags: [],
            required_skill_ids: [],
            optional_skill_ids: [],
          },
    );
    setEditing(item);
  }
  /** save 创建或修改接入链接，失败时保留未保存内容。 */
  async function save(values: Record<string, unknown>) {
    setSaving(true);
    setError(null);
    try {
      await api(`/tenant/access-links${editing ? `/${editing.id}` : ""}`, {
        method: editing ? "PATCH" : "POST",
        tenantId: links.tenantId,
        bodyJSON: values,
        idempotent: true,
      });
      message.success("接入链接已保存");
      setEditing(undefined);
      links.reload();
    } catch (problem) {
      setError(problem);
    } finally {
      setSaving(false);
    }
  }
  /** copy 只有系统剪贴板写入成功后才反馈已复制。 */
  async function copy(value: string) {
    try {
      if (parameterError) {
        message.warning(parameterError);
        return;
      }
      await navigator.clipboard.writeText(value);
      message.success("已复制");
    } catch {
      message.warning("浏览器未允许复制，请在分享窗口手动选择链接。");
    }
  }
  /** archive 归档入口，服务端决定已有咨询的后续行为。 */
  async function archive(item: AccessLink) {
    try {
      await api(`/tenant/access-links/${item.id}`, { method: "DELETE", tenantId: links.tenantId });
      message.success("入口已归档");
      links.reload();
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  const columns: ColumnsType<AccessLink> = [
    {
      title: "入口名称",
      dataIndex: "name",
      render: (value, row) => (
        <>
          <strong>{value}</strong>
          <div className="console-entry-key" title={row.entry_key}>
            {row.entry_key}
          </div>
        </>
      ),
    },
    {
      title: "客服业务组",
      dataIndex: "service_group_id",
      render: (value) => groups.items.find((item) => item.id === value)?.name || "默认范围",
    },
    {
      title: "分配池",
      dataIndex: "routing_pool_id",
      render: (value) => pools.items.find((item) => item.id === value)?.name || "默认分配",
    },
    {
      title: "渠道标识",
      dataIndex: "channel_code",
      render: (value, row) => (
        <Space size={[0, 4]} wrap>
          {Array.from(new Set([value, ...(row.channel_tags || [])].filter(Boolean))).map((tag) => (
            <Tag key={tag}>{tag}</Tag>
          ))}
        </Space>
      ),
    },
    {
      title: "状态",
      dataIndex: "status",
      render: (value) => (
        <Tag color={value === "active" ? "green" : "default"}>
          {value === "active" ? "启用" : "已停用"}
        </Tag>
      ),
    },
    {
      title: "操作",
      width: 230,
      render: (_, row) => (
        <Space>
          <Button type="link" size="small" onClick={() => edit(row)}>
            配置
          </Button>
          <Button type="link" size="small" onClick={() => setSharing(row)}>
            分享
          </Button>
          <Button type="link" size="small" onClick={() => setSharing(row)}>
            访问与参数
          </Button>
          <Popconfirm title="归档该入口？" onConfirm={() => archive(row)}>
            <Button type="link" danger size="small">
              归档
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];
  return (
    <section className="console-screen console-links">
      <PageTitle
        title="接入链接"
        description="不同入口分别配置接待范围、来源和品牌页面，电脑与手机都能打开。"
        action={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => edit(null)}>
            创建接入链接
          </Button>
        }
      />
      <div className="table-card">
        <ErrorNotice error={links.error} retry={links.reload} />
        <Table
          rowKey="id"
          columns={columns}
          dataSource={links.items}
          loading={links.loading}
          scroll={{ x: 950 }}
          locale={{ emptyText: <EmptyData text="创建第一个链接，开始接待客户。" /> }}
        />
      </div>
      <Drawer
        rootClassName="console-drawer"
        title={editing ? "配置接入链接" : "创建接入链接"}
        open={editing !== undefined}
        onClose={() => setEditing(undefined)}
        width={500}
        destroyOnHidden
      >
        <ErrorNotice error={error} />
        <Form form={form} layout="vertical" onFinish={save}>
          <Form.Item name="name" label="入口名称" rules={[{ required: true, whitespace: true }]}>
            <Input placeholder="例如：官网售前、订单售后" maxLength={100} />
          </Form.Item>
          <Form.Item name="service_group_id" label="客服业务组">
            <Select
              allowClear
              placeholder="使用默认业务组"
              options={groups.items.map((item) => ({
                label: item.name || item.id,
                value: item.id,
              }))}
            />
          </Form.Item>
          <Form.Item name="routing_pool_id" label="分配池">
            <Select
              allowClear
              placeholder="使用默认分配规则"
              options={pools.items.map((item) => ({ label: item.name || item.id, value: item.id }))}
            />
          </Form.Item>
          <Form.Item
            name="required_skill_ids"
            label="入口附加必备技能"
            extra="与分配池必备技能叠加；更换入口或公开参数不能放宽分配池要求。"
          >
            <Select
              mode="multiple"
              allowClear
              options={skills.items.map((item) => ({
                label: item.name || item.id,
                value: item.id,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="optional_skill_ids"
            label="入口附加可选技能"
            extra="与池的可选技能共同排序，必备要求优先。"
          >
            <Select
              mode="multiple"
              allowClear
              options={skills.items.map((item) => ({
                label: item.name || item.id,
                value: item.id,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="reception_mode"
            label="接待方式"
            extra="仅影响此链接；自动分配仍遵守客服组、技能与接待容量限制。"
          >
            <Select
              options={[
                { label: "自动分配", value: "auto" },
                { label: "客服自行领取", value: "manual" },
                ...(editing?.reception_mode === "inherit"
                  ? [{ label: "沿用分配池的接待方式", value: "inherit" }]
                  : []),
              ]}
            />
          </Form.Item>
          <Form.Item name="agent_id" label="优先指定客服">
            <Select
              allowClear
              options={members.items
                .filter((item) => item.can_serve && !item.disabled)
                .map((item) => ({ label: item.display_name || item.email, value: item.id }))}
            />
          </Form.Item>
          <Form.Item name="agent_mode" label="指定客服不可用时">
            <Select
              options={[
                { label: "优先指定客服，允许规则内兜底", value: "prefer" },
                { label: "严格等待指定客服", value: "strict" },
              ]}
            />
          </Form.Item>
          <Form.Item
            name="channel_code"
            label="主渠道标识"
            extra="用于来源筛选与统计，已有来源标识保持兼容。"
          >
            <Input placeholder="例如：官网、广告A" maxLength={100} />
          </Form.Item>
          <Form.Item
            name="channel_tags"
            label="渠道标签"
            extra="可输入多个标签并按回车确认，展示在客户姓名旁。"
          >
            <Select
              mode="tags"
              maxCount={20}
              tokenSeparators={[",", "，"]}
              placeholder="例如：官网、售前、广告A"
            />
          </Form.Item>
          <Form.Item name="status" label="入口状态">
            <Select
              options={[
                { label: "启用", value: "active" },
                { label: "停用", value: "disabled" },
              ]}
            />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={saving}>
            保存入口
          </Button>
        </Form>
      </Drawer>
      <Drawer
        rootClassName="console-drawer console-share-drawer"
        title={
          <Space>
            <QrcodeOutlined />
            分享接入链接
          </Space>
        }
        open={!!sharing}
        onClose={() => setSharing(null)}
        width={620}
        mask={false}
        destroyOnHidden
      >
        <Tabs
          defaultActiveKey="link"
          items={[
            {
              key: "link",
              label: "链接",
              children: (
                <Form layout="vertical">
                  <Form.Item
                    label="勾选要拼接的资料参数"
                    extra="资料参数始终接收，中文字段更易读；phone、name、avatar_url、sex 等英文写法也兼容。"
                  >
                    <Checkbox.Group
                      value={parameterKeys}
                      onChange={(keys) => setParameterKeys(keys as string[])}
                      options={Object.keys(parameterExamples).map((key) => ({
                        label: key === "extra" ? "额外资料（extra JSON）" : key,
                        value: key,
                      }))}
                    />
                  </Form.Item>
                  {parameterKeys.map((key) => (
                    <Form.Item
                      key={key}
                      label={key === "extra" ? "额外资料 extra" : key}
                      validateStatus={key === "extra" && parameterError ? "error" : undefined}
                      help={key === "extra" ? parameterError || undefined : undefined}
                    >
                      {key === "性别" ? (
                        <Select
                          value={parameterValues[key]}
                          onChange={(value) =>
                            setParameterValues((previous) => ({ ...previous, [key]: value }))
                          }
                          options={["男", "女", "未知"].map((value) => ({ value, label: value }))}
                        />
                      ) : key === "extra" ? (
                        <Input.TextArea
                          rows={3}
                          value={parameterValues[key]}
                          onChange={(event) =>
                            setParameterValues((previous) => ({
                              ...previous,
                              [key]: event.target.value,
                            }))
                          }
                        />
                      ) : (
                        <Input
                          value={parameterValues[key]}
                          onChange={(event) =>
                            setParameterValues((previous) => ({
                              ...previous,
                              [key]: event.target.value,
                            }))
                          }
                        />
                      )}
                    </Form.Item>
                  ))}
                  <p className="form-hint">
                    性别支持“男、女、未知”，也支持
                    sex=man、sex=woman、sex=unknown。字段不能满足需求时，可在链接后添加 extra=
                    {parameterExamples.extra}
                    。也可直接传入自定义字段；客服右侧客户资料会逐行显示。复制时会自动编码中文、JSON与特殊字符。
                  </p>
                  {parameterError && (
                    <p role="alert" className="form-hint">
                      {parameterError}
                    </p>
                  )}
                  <Form.Item label="聊天链接">
                    <Input
                      readOnly
                      value={sharing ? link(sharing) : ""}
                      addonAfter={
                        <Button
                          type="text"
                          size="small"
                          icon={<CopyOutlined />}
                          aria-label="复制链接"
                          onClick={() => sharing && void copy(link(sharing))}
                        />
                      }
                    />
                  </Form.Item>
                  <Button
                    type="primary"
                    href={sharing && !parameterError ? link(sharing) : undefined}
                    target="_blank"
                    disabled={!!parameterError}
                  >
                    打开此链接
                  </Button>
                  <p className="form-hint">
                    访客通过此链接即可发起咨询；公开参数仅为展示资料，不用于恢复身份。
                  </p>
                  <section className="console-share-qr">
                    <h3>预览二维码</h3>
                    {sharing && !parameterError && <QRCode value={link(sharing)} />}
                  </section>
                </Form>
              ),
            },
            {
              key: "qr",
              label: "二维码",
              children: (
                <div className="console-share-qr">
                  {sharing && !parameterError && <QRCode value={link(sharing)} />}
                  <p className="form-hint">可用手机扫描，打开对应咨询入口。</p>
                </div>
              ),
            },
            {
              key: "embed",
              label: "嵌入",
              children: (
                <Form layout="vertical">
                  <Form.Item label="网页嵌入示例">
                    <Input.TextArea
                      readOnly
                      rows={6}
                      value={
                        sharing
                          ? `<iframe src="${link(sharing)}" title="在线客服" style="width:100%;height:680px;border:0"></iframe>`
                          : ""
                      }
                    />
                  </Form.Item>
                </Form>
              ),
            },
          ]}
        />
        <p className="form-hint">
          姓名、手机号和业务 JSON 可按接入文档传入。公开参数只用于展示与上下文，不授予历史会话权限。
        </p>
      </Drawer>
    </section>
  );
}
