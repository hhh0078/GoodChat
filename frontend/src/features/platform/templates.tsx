/** 文件作用说明：由平台管理员新增、编辑和上下架公共装修模板，租户私有作品不会自动公开。 */
"use client";
import { useState } from "react";
import { Alert, App, Button, Drawer, Form, Input, Select, Space, Table, Tag } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import { api } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import type { Resource } from "@/lib/types";
import { ErrorNotice, EmptyData } from "@/components/page-state";
import { starterDesign, type Template } from "@/features/decoration/templates";
import { starterCodeDesign } from "@/features/decoration/starter-code";
import { TemplatePreviewButton } from "@/features/assistant/template-preview-modal";
import { platformTemplateDesign } from "./template-design";
import { StaticTemplateEditor } from "@/features/decoration/static-template-editor";
/** PlatformTemplates 维护公共设计的独立版本，明确上架后才能被租户选择。 */
export function PlatformTemplates() {
  const data = useResource<Template>("/platform/templates", true);
  const [editing, setEditing] = useState<Template | null | undefined>();
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [visual, setVisual] = useState<Template | null>(null);
  const [visualDirty, setVisualDirty] = useState(false);
  const [form] = Form.useForm();
  const mode = Form.useWatch("render_mode", form);
  const packaged = !!editing?.design.platform_package_id;
  const { message, modal } = App.useApp();
  /** closeVisual 在用户确认后才放弃尚未保存的元素与源码改动。 */
  function closeVisual() {
    if (visualDirty)
      modal.confirm({
        title: "关闭并放弃未保存的修改？",
        okText: "放弃修改",
        cancelText: "继续编辑",
        onOk: () => {
          setVisual(null);
          setVisualDirty(false);
        },
      });
    else setVisual(null);
  }
  /** remove 先读取最新来源引用，再由受控接口移除目录并安全回收无引用文件。 */
  async function remove(item: Template) {
    try {
      const usage = await api<{ source_copies: number; will_archive: boolean; version: number }>(
        `/platform/templates/${item.id}/usage`,
      );
      modal.confirm({
        title: `移除平台模板「${item.name}」？`,
        content: usage.will_archive
          ? `已有 ${usage.source_copies} 个私有副本，目录项将移除并保留必要来源历史；私有副本与已发布页面继续使用。`
          : "将移除目录项，并回收无其他模板或历史引用的文件。",
        okText: "移除模板",
        cancelText: "保留",
        okButtonProps: { danger: true },
        onOk: async () => {
          const result = await api<{ archived: boolean; deleted_files: number }>(
            `/platform/templates/${item.id}`,
            { method: "DELETE", bodyJSON: { version: usage.version } },
          );
          message.success(
            result.archived
              ? "已从平台目录移除，必要来源历史已保留"
              : `已移除模板并回收 ${result.deleted_files} 个无引用文件`,
          );
          data.reload();
        },
      });
    } catch (problem) {
      setError(problem);
    }
  }
  /** open 填充平台模板或准备新的基础初稿，尚未保存时不会公开。 */
  function open(item: Template | null) {
    setError(null);
    form.resetFields();
    form.setFieldsValue(
      item
        ? { ...item, design_json: JSON.stringify(item.design, null, 2), ...item.design }
        : {
            name: "",
            render_mode: "schema",
            status: "hidden",
            design_json: JSON.stringify(starterDesign(), null, 2),
            ...starterCodeDesign(),
          },
    );
    setEditing(item);
  }
  /** save 校验设计并以当前版本提交，上架变更由服务端生成不可变历史。 */
  async function save(values: Record<string, unknown>) {
    setBusy(true);
    setError(null);
    try {
      const design = platformTemplateDesign(values.render_mode, values, editing?.design);
      await api(`/platform/templates${editing ? `/${editing.id}` : ""}`, {
        method: editing ? "PATCH" : "POST",
        bodyJSON: {
          name: values.name,
          render_mode: packaged ? "code" : values.render_mode,
          status: values.status,
          design,
          ...(editing ? { version: editing.version } : {}),
        },
        idempotent: true,
      });
      message.success("平台模板已保存");
      setEditing(undefined);
      data.reload();
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="studio-platform-section">
      <div className="studio-panel-heading">
        <h2>公共装修模板</h2>
        <p>维护租户可复制使用的公共设计。</p>
      </div>
      <div className="resource-tools">
        <p className="muted">
          平台模板独立发布。租户复制后归本租户，后续平台更新不会覆盖私有模板。
        </p>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => open(null)}>
          新增平台模板
        </Button>
      </div>
      <ErrorNotice error={data.error} retry={data.reload} />
      <Table
        rowKey="id"
        dataSource={data.items}
        loading={data.loading}
        locale={{ emptyText: <EmptyData text="暂时没有公共模板" /> }}
        columns={[
          { title: "模板名称", dataIndex: "name" },
          {
            title: "方式",
            dataIndex: "render_mode",
            render: (value) => (value === "code" ? "代码装修" : "组件装修"),
          },
          { title: "版本", dataIndex: "version" },
          {
            title: "状态",
            dataIndex: "status",
            render: (value) => (
              <Tag color={value === "published" ? "blue" : "default"}>
                {value === "published" ? "已上架" : "隐藏"}
              </Tag>
            ),
          },
          {
            title: "操作",
            render: (_, row) => (
              <Space>
                <TemplatePreviewButton
                  path={`/platform/templates/${encodeURIComponent(row.id)}/preview`}
                  name={row.name || "未命名模板"}
                />
                <Button type="link" onClick={() => open(row)}>
                  编辑与上架
                </Button>
                {row.design.platform_package_id && (
                  <Button
                    type="link"
                    onClick={() => {
                      setVisualDirty(false);
                      setVisual(row);
                    }}
                  >
                    可视化编辑
                  </Button>
                )}
                <Button type="link" danger onClick={() => void remove(row)}>
                  移除
                </Button>
              </Space>
            ),
          },
        ]}
      />
      <Drawer
        rootClassName="platform-visual-editor-drawer"
        title={`平台模板编辑 · ${visual?.name || ""}`}
        open={!!visual}
        onClose={closeVisual}
        width="min(1600px, 98vw)"
        destroyOnHidden
      >
        {visual && (
          <StaticTemplateEditor
            target={{
              scope: "platform",
              id: visual.id,
              name: visual.name || "",
              version: visual.version,
              status: String(visual.status || "hidden"),
            }}
            onDirtyChange={setVisualDirty}
            onSaved={async () => {
              const result = await api<{ items: Template[] }>("/platform/templates");
              const updated = result.items.find((item) => item.id === visual.id);
              if (updated) setVisual((current) => (current?.id === updated.id ? updated : current));
              data.reload();
            }}
          />
        )}
      </Drawer>
      <Drawer
        rootClassName="studio-drawer"
        title={editing ? "编辑平台模板" : "新增平台模板"}
        open={editing !== undefined}
        onClose={() => setEditing(undefined)}
        width={760}
        destroyOnHidden
      >
        <ErrorNotice error={error} />
        <Form form={form} layout="vertical" onFinish={save}>
          <Form.Item name="name" label="模板名称" rules={[{ required: true, whitespace: true }]}>
            <Input maxLength={100} />
          </Form.Item>
          <Space wrap>
            <Form.Item name="render_mode" label="装修方式">
              <Select
                disabled={!!editing}
                style={{ width: 190 }}
                options={[
                  { label: "可视化组件设计", value: "schema" },
                  { label: "完整 HTML CSS JavaScript", value: "code" },
                ]}
              />
            </Form.Item>
            <Form.Item name="status" label="上架状态">
              <Select
                style={{ width: 190 }}
                options={[
                  { label: "隐藏，暂不上架", value: "hidden" },
                  { label: "上架，租户可选择", value: "published" },
                ]}
              />
            </Form.Item>
          </Space>
          {packaged ? (
            <Alert
              type="info"
              showIcon
              title="此模板使用独立的平台静态资源包"
              description="这里可以修改名称和上架状态。需要调整设计时，先复制到我的模板完成二创，再明确保存为新的平台模板。"
              style={{ marginBottom: 24 }}
            />
          ) : mode === "code" ? (
            <>
              <Form.Item name="html" label="HTML">
                <Input.TextArea className="code-editor" rows={8} />
              </Form.Item>
              <Form.Item name="css" label="CSS">
                <Input.TextArea className="code-editor" rows={6} />
              </Form.Item>
              <Form.Item name="javascript" label="JavaScript">
                <Input.TextArea className="code-editor" rows={10} />
              </Form.Item>
              <p className="form-hint">
                使用 window.GoodChat 受限 SDK 连接聊天，不上传外部脚本、form 或样式外链。
              </p>
            </>
          ) : (
            <Form.Item
              name="design_json"
              label="组件设计 JSON"
              extra="包含 theme、brand、blocks 与 custom_css。必须保留一个 chat 组件，最终以服务端校验为准。"
            >
              <Input.TextArea className="code-editor" rows={20} />
            </Form.Item>
          )}
          <Button type="primary" htmlType="submit" loading={busy}>
            保存平台模板
          </Button>
        </Form>
      </Drawer>
    </section>
  );
}
