/** 文件作用说明：在统一弹框维护个人及公共富话术，媒体通过现有受控附件接口上传。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { App, Button, Form, Input, Modal, Popconfirm, Select, Space } from "antd";
import { DeleteOutlined, PlusOutlined, UploadOutlined } from "@ant-design/icons";
import { api, errorMessage } from "@/lib/api";
import type { QuickReply } from "@/lib/quick-replies";
import { AttachmentView } from "./attachment";
type ReplyKind = "text" | "image" | "audio" | "file" | "options";
type ReplyForm = {
  title: string;
  content: string;
  kind: ReplyKind;
  scope: "personal" | "public";
  attachment_id: string;
  options: { id: string; label: string; value: string }[];
};

/** QuickReplyEditor 保存当前版本，取消或切换话术会废止尚未完成的上传。 */
export function QuickReplyEditor({
  tenantId,
  manages,
  items,
  item,
  onSelect,
  onClose,
  onSaved,
}: {
  tenantId: string;
  manages: boolean;
  items: QuickReply[];
  item: QuickReply | null;
  onSelect: (item: QuickReply | null) => void;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { message } = App.useApp();
  const [form] = Form.useForm<ReplyForm>();
  const kind = Form.useWatch("kind", form) || "text";
  const [busy, setBusy] = useState(false),
    [uploading, setUploading] = useState(false);
  const [attachment, setAttachment] = useState<QuickReply["attachment"]>();
  const uploadRequest = useRef<AbortController | null>(null),
    input = useRef<HTMLInputElement | null>(null),
    mounted = useRef(true),
    saving = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      uploadRequest.current?.abort();
    };
  }, []);
  useEffect(() => {
    uploadRequest.current?.abort();
    uploadRequest.current = null;
    setUploading(false);
    setAttachment(item?.attachment);
    form.setFieldsValue(
      item
        ? {
            title: item.title,
            content: item.content,
            scope: item.scope,
            kind: item.options?.length ? "options" : item.message_type,
            attachment_id: item.attachment_id || "",
            options: item.options || [],
          }
        : {
            title: "",
            content: "",
            scope: "personal",
            kind: "text",
            attachment_id: "",
            options: [],
          },
    );
  }, [item, form]);
  /** upload 校验实际返回媒体类型，失败时保留原附件与表单。 */
  async function upload(file?: File) {
    if (!file || uploadRequest.current || busy) return;
    if (!file.size || file.size > 20 * 1024 * 1024) {
      message.error("请选择不超过20MB的有效文件");
      return;
    }
    const controller = new AbortController();
    uploadRequest.current = controller;
    setUploading(true);
    try {
      const body = new FormData();
      body.append("file", file);
      const uploaded = await api<NonNullable<QuickReply["attachment"]>>("/attachments", {
        tenantId,
        method: "POST",
        body,
        signal: controller.signal,
      });
      if (controller.signal.aborted || !mounted.current) return;
      const actual = uploaded.content_type;
      if (
        (kind === "image" && !actual.startsWith("image/")) ||
        (kind === "audio" && !actual.startsWith("audio/") && actual !== "video/webm")
      ) {
        message.error("文件实际类型与所选消息类型不符，请重新选择");
        return;
      }
      setAttachment(uploaded);
      form.setFieldValue("attachment_id", uploaded.id);
    } catch (problem) {
      if (!controller.signal.aborted && mounted.current) message.error(errorMessage(problem));
    } finally {
      if (uploadRequest.current === controller) uploadRequest.current = null;
      if (!controller.signal.aborted && mounted.current) setUploading(false);
    }
  }
  /** save 只提交可维护字段，版本冲突时保留当前输入供重新核对。 */
  async function save() {
    if (saving.current) return;
    saving.current = true;
    try {
      const values = await form.validateFields();
      setBusy(true);
      await api(`/tenant/quick-replies${item ? `/${item.id}` : ""}`, {
        tenantId,
        method: item ? "PATCH" : "POST",
        bodyJSON: {
          title: values.title,
          content: values.content || "",
          scope: values.scope,
          message_type: values.kind === "options" ? "text" : values.kind,
          attachment_id: ["image", "audio", "file"].includes(values.kind)
            ? values.attachment_id
            : "",
          options: values.kind === "options" ? values.options || [] : [],
          ...(item ? { version: item.version } : {}),
        },
      });
      if (mounted.current) onSaved();
    } catch (problem) {
      if (mounted.current && !(problem && typeof problem === "object" && "errorFields" in problem))
        message.error(errorMessage(problem));
    } finally {
      saving.current = false;
      if (mounted.current) setBusy(false);
    }
  }
  /** remove 仅停用话术，不删除已经保存的消息或附件。 */
  async function remove() {
    if (!item) return;
    setBusy(true);
    try {
      await api(`/tenant/quick-replies/${item.id}`, {
        tenantId,
        method: "DELETE",
        bodyJSON: { version: item.version },
      });
      if (mounted.current) onSaved();
    } catch (problem) {
      if (mounted.current) message.error(errorMessage(problem));
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  return (
    <Modal
      title="快捷回复"
      open
      onCancel={busy ? undefined : onClose}
      destroyOnHidden
      footer={
        <Space>
          {item && (manages || item.scope === "personal") && (
            <Popconfirm title="删除这条快捷回复？" onConfirm={remove}>
              <Button danger disabled={busy || uploading}>
                删除
              </Button>
            </Popconfirm>
          )}
          <Button onClick={onClose} disabled={busy}>
            取消
          </Button>
          <Button type="primary" onClick={() => void save()} loading={busy} disabled={uploading}>
            保存
          </Button>
        </Space>
      }
    >
      <Select
        aria-label="管理快捷回复"
        value={item?.id || "new"}
        disabled={busy || uploading}
        style={{ width: "100%", marginBottom: 16 }}
        options={[
          { value: "new", label: "新增快捷回复" },
          ...items
            .filter((reply) => manages || reply.scope === "personal")
            .map((reply) => ({
              value: reply.id,
              label: `${reply.scope === "personal" ? "个人" : "公共"} · ${reply.title}`,
            })),
        ]}
        onChange={(id) => onSelect(items.find((reply) => reply.id === id) || null)}
      />
      <Form form={form} layout="vertical" disabled={busy}>
        <Form.Item
          name="title"
          label="标题"
          rules={[{ required: true, whitespace: true, message: "请输入标题" }]}
        >
          <Input maxLength={100} />
        </Form.Item>
        <Form.Item name="kind" label="消息内容">
          <Select
            disabled={uploading}
            options={[
              { value: "text", label: "文字" },
              { value: "image", label: "图片" },
              { value: "audio", label: "语音" },
              { value: "options", label: "选项" },
              { value: "file", label: "文件或视频" },
            ]}
            onChange={() => {
              setAttachment(undefined);
              form.setFieldValue("attachment_id", "");
            }}
          />
        </Form.Item>
        <Form.Item
          name="content"
          label={kind === "text" ? "回复内容" : "说明文字"}
          rules={
            kind === "text" ? [{ required: true, whitespace: true, message: "请输入回复内容" }] : []
          }
        >
          <Input.TextArea autoSize={{ minRows: 2, maxRows: 6 }} maxLength={10000} />
        </Form.Item>
        {["image", "audio", "file"].includes(kind) && (
          <>
            <Form.Item
              name="attachment_id"
              hidden
              rules={[{ required: true, message: "请先上传媒体文件" }]}
            >
              <Input />
            </Form.Item>
            <Form.Item label={kind === "image" ? "图片" : kind === "audio" ? "语音文件" : "附件"}>
              <input
                hidden
                ref={input}
                type="file"
                aria-label="快捷回复媒体文件"
                accept={
                  kind === "image"
                    ? "image/png,image/jpeg,image/gif,image/webp"
                    : kind === "audio"
                      ? "audio/*,video/webm"
                      : undefined
                }
                onChange={(event) => {
                  const file = event.target.files?.[0];
                  event.target.value = "";
                  void upload(file);
                }}
              />
              <Button
                icon={<UploadOutlined />}
                loading={uploading}
                onClick={() => input.current?.click()}
              >
                选择并上传
              </Button>
              {attachment && (
                <div className="quick-reply-media-preview">
                  <AttachmentView
                    attachment={attachment}
                    identity={{ tenantId, kind: "agent", actorId: "" }}
                    messageType={kind}
                  />
                </div>
              )}
            </Form.Item>
          </>
        )}
        {kind === "options" && (
          <Form.List
            name="options"
            rules={[
              {
                validator: async (_, values) => {
                  if (!values?.length || values.length > 8) throw new Error("请添加1至8个选项");
                },
              },
            ]}
          >
            {(fields, { add, remove }, { errors }) => (
              <>
                {fields.map((field) => (
                  <div className="quick-reply-option-editor" key={field.key}>
                    <Form.Item name={[field.name, "id"]} hidden>
                      <Input />
                    </Form.Item>
                    <Space align="start">
                      <Form.Item
                        name={[field.name, "label"]}
                        label="选项标题"
                        rules={[{ required: true, whitespace: true, message: "请输入标题" }]}
                      >
                        <Input maxLength={80} />
                      </Form.Item>
                      <Form.Item
                        name={[field.name, "value"]}
                        label="点击后发送"
                        rules={[{ required: true, whitespace: true, message: "请输入发送内容" }]}
                      >
                        <Input maxLength={1000} />
                      </Form.Item>
                      <Button
                        aria-label="删除选项"
                        type="text"
                        icon={<DeleteOutlined />}
                        onClick={() => remove(field.name)}
                      />
                    </Space>
                  </div>
                ))}
                <Button
                  icon={<PlusOutlined />}
                  onClick={() => add({ id: crypto.randomUUID(), label: "", value: "" })}
                  disabled={fields.length >= 8}
                >
                  添加选项
                </Button>
                <Form.ErrorList errors={errors} />
              </>
            )}
          </Form.List>
        )}
        <Form.Item name="scope" label="话术范围">
          <Select
            options={[
              { value: "personal", label: "我的话术" },
              ...(manages ? [{ value: "public", label: "公共话术" }] : []),
            ]}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}
