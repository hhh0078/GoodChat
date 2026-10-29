/** 文件作用说明：配置多层级富欢迎语、内部图片和选项，并查询指定链接实际生效内容。 */
"use client";
import { useState } from "react";
import {
  Alert,
  App,
  Button,
  Card,
  DatePicker,
  Drawer,
  Form,
  Input,
  InputNumber,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Upload,
} from "antd";
import { api, errorMessage } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import { ErrorNotice, EmptyData } from "@/components/page-state";
import type { AccessLink, Resource } from "@/lib/types";
type WelcomeContent = {
  type: string;
  content: string;
  attachment_id?: string;
  delay_seconds: number;
  options?: { id: string; label: string; value: string }[];
};
type WelcomeRule = Resource & {
  scope_type: string;
  scope_id?: string;
  scenario: string;
  enabled: boolean;
  stop_on_agent: boolean;
  version: number;
  contents: WelcomeContent[];
};
const scopeNames: Record<string, string> = {
  tenant: "整个团队",
  space: "服务空间",
  link: "接入链接",
  legacy: "基础欢迎语",
};
const scenarioNames: Record<string, string> = {
  all: "全部",
  business: "营业中",
  outside: "非营业",
};
/** WelcomeRules 管理完整覆盖规则，所见的保存和预览均来自真实后端。 */
export function WelcomeRules() {
  const rules = useResource<WelcomeRule>("/tenant/welcome-rules");
  const links = useResource<AccessLink>("/tenant/access-links");
  const [form] = Form.useForm();
  const contents = Form.useWatch("contents", form) as WelcomeContent[] | undefined;
  const scopeType = Form.useWatch("scope_type", form);
  const [editing, setEditing] = useState<WelcomeRule | null>(null);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [uploading, setUploading] = useState<number | null>(null);
  const [previewLink, setPreviewLink] = useState("");
  const [previewAt, setPreviewAt] = useState("");
  const [preview, setPreview] = useState<Resource | null>(null);
  const [previewError, setPreviewError] = useState<unknown>(null);
  const { message } = App.useApp();
  const spaces = [
    ...new Map(
      links.items.map((item) => [
        item.service_space_id,
        { value: item.service_space_id, label: `${item.name || "默认入口"}所属空间` },
      ]),
    ).values(),
  ];
  /** editRule 只放入允许更新的字段，保留当前版本以防多人覆盖。 */
  function editRule(item?: WelcomeRule) {
    form.resetFields();
    setError(null);
    setEditing(item || null);
    form.setFieldsValue(
      item
        ? {
            name: item.name,
            scope_type: item.scope_type,
            scope_id: item.scope_id,
            scenario: item.scenario,
            enabled: item.enabled,
            stop_on_agent: item.stop_on_agent,
            contents: item.contents,
          }
        : {
            scope_type: "tenant",
            scenario: "all",
            enabled: true,
            stop_on_agent: true,
            contents: [
              { type: "text", content: "欢迎咨询，请告诉我们你需要什么帮助。", delay_seconds: 0 },
            ],
          },
    );
    setOpen(true);
  }
  /** saveRule 提交完整规则和当前版本，成功后才关闭编辑器。 */
  async function saveRule(values: Record<string, unknown>) {
    setBusy(true);
    setError(null);
    try {
      await api(`/tenant/welcome-rules${editing ? `/${editing.id}` : ""}`, {
        tenantId: rules.tenantId,
        method: editing ? "PATCH" : "POST",
        bodyJSON: {
          ...values,
          scope_id: values.scope_type === "tenant" ? "" : values.scope_id,
          ...(editing ? { version: editing.version } : {}),
        },
      });
      rules.reload();
      setOpen(false);
      message.success("欢迎规则已保存，新咨询按最终继承生效");
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  /** archiveRule 只归档当前版本，历史已发送欢迎消息不会删除。 */
  async function archiveRule(item: WelcomeRule) {
    try {
      await api(`/tenant/welcome-rules/${item.id}`, {
        tenantId: rules.tenantId,
        method: "DELETE",
        bodyJSON: { version: item.version },
      });
      rules.reload();
      message.success("欢迎规则已归档");
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  /** uploadImage 使用现有私有附件上传，经真实媒体检查后才写入欢迎内容。 */
  async function uploadImage(file: File, index: number) {
    setUploading(index);
    setError(null);
    try {
      const body = new FormData();
      body.append("file", file);
      const attachment = await api<{ id: string; content_type: string }>("/attachments", {
        method: "POST",
        tenantId: rules.tenantId,
        body,
      });
      if (!attachment.content_type.startsWith("image/")) throw new Error("请选择实际图片文件");
      form.setFieldValue(["contents", index, "attachment_id"], attachment.id);
      message.success("欢迎图片已上传，保存规则后生效");
    } catch (problem) {
      setError(problem);
    } finally {
      setUploading(null);
    }
  }
  /** showPreview 查询链接和时刻最终生效层级，不创建测试咨询或发送欢迎语。 */
  async function showPreview() {
    setPreviewError(null);
    setBusy(true);
    try {
      const query = new URLSearchParams({
        ...(previewLink ? { access_link_id: previewLink } : {}),
        ...(previewAt ? { at: previewAt } : {}),
      });
      setPreview(
        await api<Resource>(`/tenant/welcome-preview?${query}`, { tenantId: rules.tenantId }),
      );
    } catch (problem) {
      setPreviewError(problem);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="table-card studio-welcome-rules">
      <h2 className="studio-panel-title">高级欢迎规则</h2>
      <Alert
        type="info"
        showIcon
        title="需要图片、选项或不同入口的欢迎内容时，再添加高级规则。"
        description="每次新咨询只使用一组欢迎内容，按“接入链接 → 所属服务空间 → 整个团队 → 基础欢迎语”依次查找。同一范围内，“营业中/非营业”优先于“全部”。关闭或归档一条规则后，会继续查找其他适用规则；欢迎总开关在“服务与欢迎语”。"
        style={{ marginBottom: 16 }}
      />
      <Button type="primary" onClick={() => editRule()} style={{ marginBottom: 16 }}>
        新增欢迎规则
      </Button>
      <ErrorNotice error={rules.error} retry={rules.reload} />
      <ErrorNotice error={links.error} retry={links.reload} />
      <Table
        rowKey="id"
        loading={rules.loading}
        dataSource={rules.items}
        locale={{ emptyText: <EmptyData text="尚未配置高级欢迎规则，当前使用基础欢迎语。" /> }}
        columns={[
          { title: "名称", dataIndex: "name" },
          {
            title: "层级",
            dataIndex: "scope_type",
            render: (value) => scopeNames[String(value)] || String(value),
          },
          {
            title: "场景",
            dataIndex: "scenario",
            render: (value) => scenarioNames[String(value)] || String(value),
          },
          {
            title: "状态",
            render: (_, item) => (
              <Tag color={item.enabled ? "green" : "default"}>
                {item.enabled ? "启用" : "关闭"} · v{item.version}
              </Tag>
            ),
          },
          {
            title: "操作",
            render: (_, item) => (
              <Space>
                <Button type="link" onClick={() => editRule(item)}>
                  编辑
                </Button>
                <Popconfirm
                  title="归档当前规则并回退上层配置？"
                  onConfirm={() => archiveRule(item)}
                >
                  <Button danger type="link">
                    归档
                  </Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />
      <Card
        className="studio-welcome-preview"
        size="small"
        title="查看最终生效欢迎"
        style={{ marginTop: 16 }}
      >
        <p className="form-hint">
          选择一个接入链接和时间，查看访客当时会看到哪组欢迎内容。时间留空表示现在；查询只预览，不会创建咨询或发消息。
        </p>
        <Space wrap>
          <Select
            allowClear
            placeholder="默认入口"
            style={{ width: 230 }}
            options={links.items.map((item) => ({ label: item.name, value: item.id }))}
            onChange={(value) => setPreviewLink(value || "")}
          />
          <DatePicker
            showTime
            onChange={(value) => setPreviewAt(value?.toISOString() || "")}
            placeholder="当前时刻（可选）"
          />
          <Button onClick={() => void showPreview()} loading={busy}>
            查询生效内容
          </Button>
        </Space>
        <ErrorNotice error={previewError} />
        {preview && (
          <div style={{ marginTop: 14 }}>
            <p>
              {preview.enabled ? "欢迎总开关已启用" : "欢迎总开关已关闭，本次不会发送"} ·{" "}
              {preview.business_open ? "营业场景" : "非营业场景"} · 时区 {String(preview.timezone)}{" "}
              · 来源 {scopeNames[String(preview.source)] || String(preview.source)}
            </p>
            {((preview.contents as WelcomeContent[]) || []).map((item, index) => (
              <p key={index}>
                <Tag>
                  {item.type === "image" ? "图片" : item.type === "options" ? "选项" : "文字"}
                </Tag>
                会话创建后 {item.delay_seconds} 秒：{item.content || "图片内容"}
                {item.options?.map((option) => (
                  <Tag key={option.id}>
                    {option.label} → {option.value}
                  </Tag>
                ))}
              </p>
            ))}
          </div>
        )}
      </Card>
      <Drawer
        rootClassName="studio-drawer"
        title={editing ? "编辑欢迎规则" : "新增欢迎规则"}
        open={open}
        onClose={() => setOpen(false)}
        width={720}
      >
        <ErrorNotice error={error} />
        <Form form={form} layout="vertical" onFinish={saveRule}>
          <Form.Item
            name="name"
            label="规则名称"
            rules={[{ required: true, whitespace: true }]}
            extra="仅供团队管理时辨认，例如“官网白天欢迎”，不会展示给访客。"
          >
            <Input maxLength={100} />
          </Form.Item>
          <Space align="start" wrap>
            <Form.Item
              name="scope_type"
              label="适用范围"
              rules={[{ required: true }]}
              extra="整个团队作为通用内容；服务空间或接入链接可单独设置。"
              style={{ maxWidth: 240 }}
            >
              <Select
                style={{ width: 150 }}
                onChange={() => form.setFieldValue("scope_id", undefined)}
                options={[
                  { label: "整个团队", value: "tenant" },
                  { label: "服务空间", value: "space" },
                  { label: "接入链接", value: "link" },
                ]}
              />
            </Form.Item>
            {scopeType !== "tenant" && (
              <Form.Item
                name="scope_id"
                label="选择空间或链接"
                rules={[{ required: true }]}
                extra="仅从所选入口进入的新咨询使用这条规则。"
                style={{ maxWidth: 260 }}
              >
                <Select
                  style={{ width: 260 }}
                  options={
                    scopeType === "link"
                      ? links.items.map((item) => ({ label: item.name, value: item.id }))
                      : spaces
                  }
                />
              </Form.Item>
            )}
            <Form.Item
              name="scenario"
              label="生效时间"
              rules={[{ required: true }]}
              extra="“全部”不限时段；另两项按“营业与评价策略”的营业时间判断。"
              style={{ maxWidth: 240 }}
            >
              <Select
                style={{ width: 150 }}
                options={[
                  { label: "全部", value: "all" },
                  { label: "营业中", value: "business" },
                  { label: "非营业", value: "outside" },
                ]}
              />
            </Form.Item>
          </Space>
          <Space align="start" wrap>
            <Form.Item
              name="enabled"
              label="启用这条规则"
              valuePropName="checked"
              extra="关闭后保留内容，暂不参与匹配。"
              style={{ maxWidth: 240 }}
            >
              <Switch />
            </Form.Item>
            <Form.Item
              name="stop_on_agent"
              label="人工接入后停止未发内容"
              valuePropName="checked"
              extra="客服接入后取消这组尚未发送的欢迎消息；关闭则继续按延迟发送。"
              style={{ maxWidth: 340 }}
            >
              <Switch />
            </Form.Item>
          </Space>
          <p className="form-hint">
            每组最多 12 条。每条延迟都从本次咨询创建时刻计算：两条分别填 5 和 10，就在第 5 秒和第 10
            秒发送，不是间隔 10 秒。相同延迟按列表顺序发送。
          </p>
          <Form.List name="contents">
            {(fields, { add, remove, move }) => (
              <>
                {fields.map((field, index) => (
                  <Card
                    key={field.key}
                    size="small"
                    title={`欢迎内容 ${index + 1}`}
                    style={{ marginBottom: 14 }}
                    extra={
                      <Space>
                        <Button
                          size="small"
                          disabled={index === 0}
                          onClick={() => move(index, index - 1)}
                        >
                          上移
                        </Button>
                        <Button
                          size="small"
                          disabled={fields.length <= 1}
                          onClick={() => remove(index)}
                        >
                          移除
                        </Button>
                      </Space>
                    }
                  >
                    <Space align="start" wrap>
                      <Form.Item
                        name={[field.name, "type"]}
                        label="类型"
                        rules={[{ required: true }]}
                        extra="可发送文字、图片或供访客点选的问题。"
                        style={{ maxWidth: 240 }}
                      >
                        <Select
                          style={{ width: 150 }}
                          onChange={() => {
                            form.setFieldValue(["contents", index, "options"], undefined);
                            form.setFieldValue(["contents", index, "attachment_id"], undefined);
                          }}
                          options={[
                            { label: "文字", value: "text" },
                            { label: "图片", value: "image" },
                            { label: "可点击选项", value: "options" },
                          ]}
                        />
                      </Form.Item>
                      <Form.Item
                        name={[field.name, "delay_seconds"]}
                        label="创建后延迟秒数"
                        rules={[{ required: true }]}
                        extra="0 表示尽快发送；最多 3600 秒（1 小时）。"
                        style={{ maxWidth: 240 }}
                      >
                        <InputNumber min={0} max={3600} precision={0} />
                      </Form.Item>
                    </Space>
                    <Form.Item
                      name={[field.name, "content"]}
                      label={contents?.[index]?.type === "image" ? "图片说明（选填）" : "欢迎文字"}
                      extra="访客在聊天里看到的内容，最多 2000 字。"
                    >
                      <Input.TextArea maxLength={2000} rows={2} />
                    </Form.Item>
                    {contents?.[index]?.type === "image" && (
                      <>
                        <Form.Item
                          name={[field.name, "attachment_id"]}
                          label="欢迎图片"
                          rules={[{ required: true, message: "请上传欢迎图片" }]}
                          extra="上传本地图片，最大 10MB；选择后还需保存整条规则才会生效。"
                          getValueProps={(value) => ({
                            value: value ? "已选择图片，保存后生效" : "",
                          })}
                        >
                          <Input readOnly placeholder="请先上传图片" />
                        </Form.Item>
                        <Upload
                          accept="image/*"
                          showUploadList={false}
                          beforeUpload={(file) => {
                            void uploadImage(file, index);
                            return false;
                          }}
                        >
                          <Button loading={uploading === index} disabled={uploading !== null}>
                            上传图片
                          </Button>
                        </Upload>
                      </>
                    )}
                    {contents?.[index]?.type === "options" && (
                      <Form.List name={[field.name, "options"]}>
                        {(options, actions) => (
                          <>
                            <p className="form-hint">
                              配置 1 至 8
                              个选项。“显示文案”是按钮上的短标题，例如“查看价格”；“实际发送内容”是访客点击后发给客服的话，例如“我想咨询价格”。
                            </p>
                            {options.map((option) => (
                              <Space key={option.key} align="baseline" wrap>
                                <Form.Item name={[option.name, "id"]} hidden>
                                  <Input />
                                </Form.Item>
                                <Form.Item
                                  name={[option.name, "label"]}
                                  label="显示文案"
                                  rules={[{ required: true, whitespace: true }]}
                                >
                                  <Input maxLength={80} />
                                </Form.Item>
                                <Form.Item
                                  name={[option.name, "value"]}
                                  label="实际发送内容"
                                  rules={[{ required: true, whitespace: true }]}
                                >
                                  <Input maxLength={1000} />
                                </Form.Item>
                                <Button onClick={() => actions.remove(option.name)}>
                                  移除选项
                                </Button>
                              </Space>
                            ))}
                            <Button
                              disabled={options.length >= 8}
                              onClick={() =>
                                actions.add({ id: crypto.randomUUID(), label: "", value: "" })
                              }
                            >
                              增加选项
                            </Button>
                          </>
                        )}
                      </Form.List>
                    )}
                  </Card>
                ))}
                <Button
                  disabled={fields.length >= 12}
                  onClick={() => add({ type: "text", content: "", delay_seconds: 0 })}
                >
                  增加欢迎内容
                </Button>
              </>
            )}
          </Form.List>
          <Button
            type="primary"
            htmlType="submit"
            loading={busy}
            disabled={uploading !== null}
            style={{ marginTop: 20 }}
          >
            保存欢迎规则
          </Button>
        </Form>
      </Drawer>
    </div>
  );
}
