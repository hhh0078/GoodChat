/** 文件作用说明：在统一弹框维护个人及公共富话术，媒体通过现有受控附件接口上传。 */
"use client";
import { useTranslator } from "@/i18n/use-translator";
import { api, errorMessage } from "@/lib/api";
import type { QuickReply } from "@/lib/quick-replies";
import { DeleteOutlined, PlusOutlined, UploadOutlined } from "@ant-design/icons";
import { App, Button, Form, Input, Modal, Popconfirm, Select, Space, Switch } from "antd";
import { useEffect, useRef, useState } from "react";
import { AttachmentView } from "./attachment";
type ReplyKind = "text" | "image" | "audio" | "file" | "options" | "form";
type ReplyForm = {
  title: string;
  content: string;
  kind: ReplyKind;
  scope: "personal" | "public";
  attachment_id: string;
  form_fields: import("@/lib/quick-replies").FormField[];
  options: {
    id: string;
    label: string;
    value: string;
  }[];
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
  const tr = useTranslator();
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
            form_fields: item.form_fields || [],
          }
        : {
            title: "",
            content: "",
            scope: "personal",
            kind: "text",
            attachment_id: "",
            options: [],
            form_fields: [],
          },
    );
  }, [item, form]);
  /** upload 校验实际返回媒体类型，失败时保留原附件与表单。 */
  async function upload(file?: File) {
    if (!file || uploadRequest.current || busy) return;
    if (!file.size || file.size > 20 * 1024 * 1024) {
      message.error(tr("请选择不超过20MB的有效文件"));
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
        message.error(tr("文件实际类型与所选消息类型不符，请重新选择"));
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
          form_fields: values.kind === "form" ? values.form_fields || [] : [],
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
      title={tr("快捷回复")}
      open
      onCancel={busy ? undefined : onClose}
      destroyOnHidden
      footer={
        <Space>
          {item && (manages || item.scope === "personal") && (
            <Popconfirm title={tr("删除这条快捷回复？")} onConfirm={remove}>
              <Button danger disabled={busy || uploading}>
                {tr("删除 ")}
              </Button>
            </Popconfirm>
          )}
          <Button onClick={onClose} disabled={busy}>
            {tr("取消 ")}
          </Button>
          <Button type="primary" onClick={() => void save()} loading={busy} disabled={uploading}>
            {tr("保存 ")}
          </Button>
        </Space>
      }
    >
      <Select
        aria-label={tr("管理快捷回复")}
        value={item?.id || "new"}
        disabled={busy || uploading}
        style={{ width: "100%", marginBottom: 16 }}
        options={[
          { value: "new", label: tr("新增快捷回复") },
          ...items
            .filter((reply) => manages || reply.scope === "personal")
            .map((reply) => ({
              value: reply.id,
              label: `${reply.scope === "personal" ? tr("个人") : tr("公共")} · ${reply.title}`,
            })),
        ]}
        onChange={(id) => onSelect(items.find((reply) => reply.id === id) || null)}
      />
      <Form form={form} layout="vertical" disabled={busy}>
        <Form.Item
          name="title"
          label={tr("标题")}
          rules={[{ required: true, whitespace: true, message: tr("请输入标题") }]}
          extra={
            kind === "form"
              ? undefined
              : tr(
                  "仅用于在快捷回复列表里识别，例如\u201C价格说明\u201D。双击标题时，会向当前客户发送下方实际内容，不会发送这个标题。",
                )
          }
        >
          <Input maxLength={100} />
        </Form.Item>
        <Form.Item
          name="kind"
          label={tr("消息内容")}
          extra={
            kind === "form"
              ? tr("发送后访客可在聊天记录填写并保存，提交结果会作为消息发送。")
              : tr(
                  "文字用于常用答复；图片用于海报或截图；语音使用已上传的音频；文件或视频用于资料；选项让访客点选后发送对应的问题。",
                )
          }
        >
          <Select
            disabled={uploading}
            options={[
              { value: "text", label: tr("文字") },
              { value: "form", label: tr("表单") },
              { value: "image", label: tr("图片") },
              { value: "audio", label: tr("语音") },
              { value: "options", label: tr("选项") },
              { value: "file", label: tr("文件或视频") },
            ]}
            onChange={() => {
              setAttachment(undefined);
              form.setFieldValue("attachment_id", "");
            }}
          />
        </Form.Item>
        <Form.Item
          name="content"
          label={kind === "text" ? tr("回复内容") : tr("说明文字")}
          rules={
            kind === "text"
              ? [{ required: true, whitespace: true, message: tr("请输入回复内容") }]
              : []
          }
          extra={
            kind === "form"
              ? tr("发送后访客可在聊天记录填写并保存，提交结果会作为消息发送。")
              : kind === "text"
                ? tr("双击发送时，客户会收到这里的完整文字。")
                : kind === "options"
                  ? tr("显示在选项按钮上方，例如\u201C请选择需要了解的内容\u201D；可留空。")
                  : tr("随媒体一起发送给客户的说明，可留空。")
          }
        >
          <Input.TextArea autoSize={{ minRows: 2, maxRows: 6 }} maxLength={10000} />
        </Form.Item>
        {["image", "audio", "file"].includes(kind) && (
          <>
            <Form.Item
              name="attachment_id"
              hidden
              rules={[{ required: true, message: tr("请先上传媒体文件") }]}
            >
              <Input />
            </Form.Item>
            <Form.Item
              label={kind === "image" ? tr("图片") : kind === "audio" ? tr("语音文件") : tr("附件")}
              extra={tr(
                "选择与消息类型对应的本地文件，上传完成后再保存。保存只建立话术，双击列表标题才会发送给当前客户。",
              )}
            >
              <input
                hidden
                ref={input}
                type="file"
                aria-label={tr("快捷回复媒体文件")}
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
                {tr("选择并上传 ")}
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
        {kind === "form" && (
          <Form.List
            name="form_fields"
            rules={[
              {
                validator: async (_, values) => {
                  if (!values?.length || values.length > 20)
                    throw new Error(tr("表单需要1至20个字段"));
                },
              },
            ]}
          >
            {(fields, { add, remove }, { errors }) => (
              <div className="quick-reply-form-editor">
                {fields.map((field) => (
                  <div key={field.key} className="quick-reply-form-field">
                    <Form.Item name={[field.name, "id"]} hidden>
                      <Input />
                    </Form.Item>
                    <Form.Item
                      name={[field.name, "label"]}
                      label={tr("字段名称")}
                      rules={[{ required: true, whitespace: true }]}
                    >
                      <Input maxLength={80} />
                    </Form.Item>
                    <Form.Item name={[field.name, "type"]} label={tr("输入类型")}>
                      <Select
                        options={[
                          { value: "text", label: tr("单行文本") },
                          { value: "textarea", label: tr("多行文本") },
                        ]}
                      />
                    </Form.Item>
                    <Form.Item
                      name={[field.name, "required"]}
                      label={tr("必填")}
                      valuePropName="checked"
                    >
                      <Switch />
                    </Form.Item>
                    <Button
                      type="text"
                      danger
                      aria-label={tr("删除字段")}
                      icon={<DeleteOutlined />}
                      onClick={() => remove(field.name)}
                    />
                  </div>
                ))}
                <Form.ErrorList errors={errors} />
                <Button
                  icon={<PlusOutlined />}
                  disabled={fields.length >= 20}
                  onClick={() =>
                    add({ id: crypto.randomUUID(), label: "", type: "text", required: false })
                  }
                >
                  {tr("添加字段")}
                </Button>
                <p className="muted">
                  {tr("发送后访客可在聊天记录填写并保存，提交结果会作为消息发送。")}
                </p>
              </div>
            )}
          </Form.List>
        )}
        {kind === "options" && (
          <Form.List
            name="options"
            rules={[
              {
                validator: async (_, values) => {
                  if (!values?.length || values.length > 8) throw new Error(tr("请添加1至8个选项"));
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
                        label={tr("选项标题")}
                        rules={[{ required: true, whitespace: true, message: tr("请输入标题") }]}
                        extra={tr("访客看到的按钮文字，例如\u201C查看价格\u201D。")}
                      >
                        <Input maxLength={80} />
                      </Form.Item>
                      <Form.Item
                        name={[field.name, "value"]}
                        label={tr("点击后发送")}
                        rules={[
                          { required: true, whitespace: true, message: tr("请输入发送内容") },
                        ]}
                        extra={tr("访客点击按钮后发给客服的话，例如\u201C我想咨询价格\u201D。")}
                      >
                        <Input maxLength={1000} />
                      </Form.Item>
                      <Button
                        aria-label={tr("删除选项")}
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
                  {tr("添加选项 ")}
                </Button>
                <Form.ErrorList errors={errors} />
              </>
            )}
          </Form.List>
        )}
        <Form.Item
          name="scope"
          label={tr("话术范围")}
          extra={tr(
            "\u201C我的话术\u201D仅自己可见和编辑；\u201C公共话术\u201D供团队所有客服使用，由租户管理员维护。",
          )}
        >
          <Select
            options={[
              { value: "personal", label: tr("我的话术") },
              ...(manages ? [{ value: "public", label: tr("公共话术") }] : []),
            ]}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}
