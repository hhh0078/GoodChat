/** 文件作用说明：在客服输入框旁展示团队与个人快捷话术，支持个人维护和双击直接发送。 */
"use client";
import { useEffect, useState } from "react";
import { App, Button, Form, Input, Modal, Popconfirm, Select, Tag, Tooltip } from "antd";
import {
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  SendOutlined,
  SnippetsOutlined,
} from "@ant-design/icons";
import { api, errorMessage } from "@/lib/api";
import type { Resource } from "@/lib/types";

/** QuickReplies 复用后端权限维护公共和个人话术，直接发送始终沿用聊天幂等流程。 */
export function QuickReplies({
  tenantId,
  manages,
  disabled,
  onInsert,
  onSend,
}: {
  tenantId: string;
  manages: boolean;
  disabled: boolean;
  onInsert: (content: string) => void;
  onSend: (content: string) => void;
}) {
  const { message } = App.useApp();
  const [items, setItems] = useState<Resource[]>([]);
  const [scope, setScope] = useState("all");
  const [editing, setEditing] = useState<Resource | null | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const [form] = Form.useForm();
  useEffect(() => {
    const controller = new AbortController();
    api<{ items: Resource[] }>("/tenant/quick-replies", { tenantId, signal: controller.signal })
      .then((data) => setItems((data.items || []).filter((item) => item.enabled !== false)))
      .catch((problem) => {
        if (!controller.signal.aborted) message.error(errorMessage(problem));
      });
    return () => controller.abort();
  }, [tenantId, revision, message]);
  /** openEditor 设置本次编辑值，新话术默认仅自己可见。 */
  function openEditor(item: Resource | null) {
    setEditing(item);
    form.setFieldsValue(item || { title: "", content: "", scope: "personal" });
  }
  /** save 验证内容后保存，成功才关闭编辑器并刷新话术。 */
  async function save() {
    try {
      const values = await form.validateFields();
      setBusy(true);
      await api(`/tenant/quick-replies${editing ? `/${editing.id}` : ""}`, {
        tenantId,
        method: editing ? "PATCH" : "POST",
        bodyJSON: values,
      });
      setEditing(undefined);
      setRevision((value) => value + 1);
    } catch (problem) {
      if (!(problem && typeof problem === "object" && "errorFields" in problem))
        message.error(errorMessage(problem));
    } finally {
      setBusy(false);
    }
  }
  /** remove 停用话术，后端继续核验个人归属和公共维护权限。 */
  async function remove(item: Resource) {
    try {
      await api(`/tenant/quick-replies/${item.id}`, { tenantId, method: "DELETE" });
      setRevision((value) => value + 1);
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  const visible = items.filter((item) => scope === "all" || item.scope === scope);
  return (
    <aside className="composer-quick-replies" aria-label="快捷回复">
      <header>
        <strong>快捷回复</strong>
        <Tooltip title="添加个人话术">
          <Button
            type="text"
            size="small"
            icon={<PlusOutlined />}
            aria-label="添加快捷回复"
            onClick={() => openEditor(null)}
          />
        </Tooltip>
      </header>
      <Select
        size="small"
        aria-label="快捷回复范围"
        value={scope}
        onChange={setScope}
        options={[
          { value: "all", label: "全部话术" },
          { value: "public", label: "租户话术" },
          { value: "personal", label: "我的话术" },
        ]}
      />
      <div className="quick-reply-list">
        {visible.length ? (
          visible.map((item) => (
            <div className="quick-reply-item" key={item.id}>
              <button
                type="button"
                className="quick-reply-content"
                disabled={disabled}
                title={`${String(item.content || "")}\n双击直接发送，使用插入按钮追加到草稿`}
                onDoubleClick={() => onSend(String(item.content || ""))}
              >
                <span>
                  <Tag>{item.scope === "personal" ? "个人" : "租户"}</Tag>
                  <strong>{String(item.title || "话术")}</strong>
                </span>
                <p>{String(item.content || "")}</p>
              </button>
              <div className="quick-reply-actions">
                <Button
                  type="text"
                  size="small"
                  icon={<SnippetsOutlined />}
                  aria-label={`插入话术${String(item.title)}`}
                  title="追加到草稿"
                  disabled={disabled}
                  onClick={() => onInsert(String(item.content || ""))}
                />
                <Button
                  type="text"
                  size="small"
                  icon={<SendOutlined />}
                  aria-label={`发送话术${String(item.title)}`}
                  disabled={disabled}
                  onClick={() => onSend(String(item.content || ""))}
                />
                {(manages || item.scope === "personal") && (
                  <>
                    <Button
                      type="text"
                      size="small"
                      icon={<EditOutlined />}
                      aria-label={`编辑话术${String(item.title)}`}
                      onClick={() => openEditor(item)}
                    />
                    <Popconfirm title="删除这条快捷回复？" onConfirm={() => remove(item)}>
                      <Button
                        type="text"
                        size="small"
                        danger
                        icon={<DeleteOutlined />}
                        aria-label={`删除话术${String(item.title)}`}
                      />
                    </Popconfirm>
                  </>
                )}
              </div>
            </div>
          ))
        ) : (
          <p className="muted quick-reply-empty">暂无话术，可添加自己的常用回复。</p>
        )}
      </div>
      <small>双击发送 · 按钮插入</small>
      <Modal
        title={editing ? "编辑快捷回复" : "添加快捷回复"}
        open={editing !== undefined}
        onCancel={() => setEditing(undefined)}
        onOk={() => void save()}
        confirmLoading={busy}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="title"
            label="标题"
            rules={[{ required: true, whitespace: true, message: "请输入标题" }]}
          >
            <Input maxLength={100} />
          </Form.Item>
          <Form.Item
            name="content"
            label="回复内容"
            rules={[{ required: true, whitespace: true, message: "请输入内容" }]}
          >
            <Input.TextArea autoSize={{ minRows: 3, maxRows: 8 }} maxLength={10000} />
          </Form.Item>
          <Form.Item name="scope" label="话术范围">
            <Select
              options={[
                { value: "personal", label: "我的话术" },
                ...(manages ? [{ value: "public", label: "租户话术" }] : []),
              ]}
            />
          </Form.Item>
        </Form>
      </Modal>
    </aside>
  );
}
