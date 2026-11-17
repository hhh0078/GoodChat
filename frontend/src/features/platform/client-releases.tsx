/** 文件作用说明：让平台管理员维护客户端版本、说明和各系统下载地址。 */
"use client";
import { ErrorNotice } from "@/components/page-state";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import type { Resource } from "@/lib/types";
import { useResource } from "@/lib/use-resource";
import { App, Button, Drawer, Form, Input, Switch, Table, Tag } from "antd";
import { useState } from "react";
/** ClientReleases 复用现有平台权限和组件库，未发布版本不会触发强制更新。 */
export function ClientReleases() {
  const tr = useTranslator();
  const releases = useResource<Resource>("/platform/client-releases", true);
  const [open, setOpen] = useState(false),
    [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState<Resource | null>(null),
    [error, setError] = useState<unknown>(null);
  const [form] = Form.useForm();
  const { message } = App.useApp();
  /** edit 打开已有版本或空白草稿，APK可保留为空。 */
  function edit(item: Resource | null) {
    setEditing(item);
    form.resetFields();
    form.setFieldsValue(item || { published: false });
    setError(null);
    setOpen(true);
  }
  /** save 等服务端确认后更新列表，发布高于当前版本时客户端将强制更新。 */
  async function save(values: Record<string, unknown>) {
    setBusy(true);
    try {
      await api(`/platform/client-releases${editing ? `/${editing.id}` : ""}`, {
        method: editing ? "PATCH" : "POST",
        bodyJSON: values,
      });
      setOpen(false);
      releases.reload();
      message.success(tr("客户端版本已保存"));
    } catch (problem) {
      setError(problem);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="studio-platform-section">
      <div className="studio-panel-heading">
        <h2>{tr("客户端版本")}</h2>
        <p>
          {tr(
            "维护下载页与客户端启动检查。已发布且高于客户端当前版本、同时高于0.0.1的版本将强制更新。 ",
          )}
        </p>
        <Button type="primary" onClick={() => edit(null)}>
          {tr("添加版本 ")}
        </Button>
      </div>
      <ErrorNotice error={releases.error} retry={releases.reload} />
      <Table
        rowKey="id"
        loading={releases.loading}
        dataSource={releases.items}
        columns={[
          { title: tr("版本号"), dataIndex: "version" },
          { title: tr("说明"), dataIndex: "description", ellipsis: true },
          {
            title: tr("状态"),
            render: (_, row) => (
              <Tag color={row.published ? "success" : "default"}>
                {row.published ? tr("已发布") : tr("草稿")}
              </Tag>
            ),
          },
          {
            title: tr("系统"),
            render: (_, row) =>
              [row.win_url && "Windows", row.mac_url && "Mac", row.apk_url && "Android"]
                .filter(Boolean)
                .join(" / ") || tr("未配置"),
          },
          {
            title: tr("操作"),
            render: (_, row) => (
              <Button type="link" onClick={() => edit(row)}>
                {tr("编辑 ")}
              </Button>
            ),
          },
        ]}
      />
      <Drawer
        title={editing ? tr("编辑客户端版本") : tr("添加客户端版本")}
        open={open}
        onClose={() => setOpen(false)}
        size={540}
      >
        <ErrorNotice error={error} />
        <Form form={form} layout="vertical" onFinish={save}>
          <Form.Item
            label={tr("版本号")}
            name="version"
            rules={[
              {
                required: true,
                pattern: /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/,
                message: tr("例如1.0.0"),
              },
            ]}
          >
            <Input placeholder="1.0.0" />
          </Form.Item>
          <Form.Item label={tr("更新说明")} name="description">
            <Input.TextArea rows={5} maxLength={5000} />
          </Form.Item>
          {[
            ["win_url", tr("Windows下载地址")],
            ["mac_url", tr("Mac下载地址")],
            ["apk_url", tr("APK下载地址（暂可不填）")],
          ].map(([name, label]) => (
            <Form.Item
              key={name}
              name={name}
              label={label}
              rules={[{ type: "url", message: tr("请填写完整HTTPS地址") }]}
            >
              <Input placeholder="https://…" />
            </Form.Item>
          ))}
          <Form.Item label={tr("公开发布")} name="published" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>
            {tr("保存版本 ")}
          </Button>
        </Form>
      </Drawer>
    </section>
  );
}
