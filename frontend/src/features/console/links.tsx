/** 文件作用说明：管理接入链接、生成二维码和参数化链接，并调用真实入口接口保存。 */
"use client";
import { useState } from "react";
import {
  App,
  Button,
  Drawer,
  Form,
  Input,
  Tabs,
  Popconfirm,
  QRCode,
  Select,
  Space,
  Switch,
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

/** LinksPage 为独立业务入口维护路由范围并生成分享与嵌入内容。 */
export function LinksPage() {
  const links = useResource<AccessLink>("/tenant/access-links");
  const groups = useResource<Resource>("/tenant/groups");
  const pools = useResource<Resource>("/tenant/routing-pools");
  const skills = useResource<Resource>("/tenant/routing-skills");
  const members = useResource<Member>("/tenant/members");
  const [editing, setEditing] = useState<AccessLink | null | undefined>();
  const [sharing, setSharing] = useState<AccessLink | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [form] = Form.useForm();
  const { message } = App.useApp();
  /** link 通过当前站点生成标准公开聊天地址。 */
  function link(item: AccessLink) {
    return typeof window === "undefined"
      ? `/c/${item.entry_key}`
      : entryURL(window.location.origin, item.entry_key);
  }
  /** edit 将已有入口或初始配置填入真实保存表单。 */
  function edit(item: AccessLink | null) {
    setError(null);
    form.resetFields();
    form.setFieldsValue(
      item || {
        status: "active",
        agent_mode: "prefer",
        allow_routing_override: false,
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
    { title: "来源", dataIndex: "channel_code", render: (value) => value || "—" },
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
          <Button type="link" size="small" href={`/c/${row.entry_key}`} target="_blank">
            访问
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
          <Form.Item name="channel_code" label="来源标识">
            <Input placeholder="例如 website、campaign-a" maxLength={100} />
          </Form.Item>
          <Form.Item name="status" label="入口状态">
            <Select
              options={[
                { label: "启用", value: "active" },
                { label: "停用", value: "disabled" },
              ]}
            />
          </Form.Item>
          <Form.Item
            name="allow_routing_override"
            label="允许参数选择接待范围"
            valuePropName="checked"
            extra="动态参数仍受服务端授权范围约束，不能扩大接待权限。"
          >
            <Switch />
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
        width={420}
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
                  <p className="form-hint">访客通过此链接即可发起咨询。</p>
                  <section className="console-share-qr">
                    <h3>预览二维码</h3>
                    {sharing && <QRCode value={link(sharing)} />}
                  </section>
                </Form>
              ),
            },
            {
              key: "qr",
              label: "二维码",
              children: (
                <div className="console-share-qr">
                  {sharing && <QRCode value={link(sharing)} />}
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
