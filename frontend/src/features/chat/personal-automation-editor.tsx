/** 文件作用说明：编辑客服本人的富欢迎语和关键词规则，媒体上传沿用受控附件接口。 */
"use client";
import { useEffect, useRef, useState } from "react";
import {
  App,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Upload,
} from "antd";
import { PlusOutlined, UploadOutlined } from "@ant-design/icons";
import { api, errorMessage } from "@/lib/api";
import type { PersonalKeyword, PersonalWelcome, WelcomeContent } from "@/lib/personal-replies";
import type { QuickReply } from "@/lib/quick-replies";
import { AttachmentView } from "./attachment";

type EditorProps = {
  tenantId: string;
  onClose: () => void;
  onSaved: () => void;
} & (
  | { kind: "welcome"; item: PersonalWelcome | null }
  | { kind: "keyword"; item: PersonalKeyword | null }
);
type RuleForm = {
  name: string;
  enabled: boolean;
  scenario: PersonalWelcome["scenario"];
  contents: WelcomeContent[];
  match_mode: PersonalKeyword["match_mode"];
  pattern: string;
  reply: string;
  priority: number;
  cooldown_seconds: number;
};

/** PersonalAutomationEditor 仅提交个人规则字段，卸载后取消上传并阻止旧请求更新界面。 */
export function PersonalAutomationEditor(props: EditorProps) {
  const { tenantId, kind, item, onClose, onSaved } = props;
  const { message } = App.useApp();
  const [form] = Form.useForm<RuleForm>();
  const contents = Form.useWatch("contents", form) as WelcomeContent[] | undefined;
  const [busy, setBusy] = useState(false);
  const [uploading, setUploading] = useState<number | null>(null);
  const [attachments, setAttachments] = useState<
    Record<number, NonNullable<QuickReply["attachment"]>>
  >({});
  const mounted = useRef(true),
    locked = useRef(false),
    request = useRef<AbortController | null>(null);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      request.current?.abort();
    };
  }, []);
  /** save 保留版本用于冲突检查，成功持久化后才关闭编辑器。 */
  async function save(values: RuleForm) {
    if (locked.current || uploading !== null) return;
    locked.current = true;
    setBusy(true);
    const controller = new AbortController();
    request.current = controller;
    try {
      const fields =
        kind === "welcome"
          ? {
              name: values.name,
              enabled: values.enabled,
              scenario: values.scenario,
              contents: values.contents.map((content) => ({
                type: content.type,
                content: content.content || "",
                delay_seconds: content.delay_seconds,
                ...(content.type === "image" ? { attachment_id: content.attachment_id } : {}),
                ...(content.type === "options" ? { options: content.options || [] } : {}),
              })),
            }
          : {
              name: values.name,
              enabled: values.enabled,
              match_mode: values.match_mode,
              pattern: values.pattern,
              reply: values.reply,
              priority: values.priority,
              cooldown_seconds: values.cooldown_seconds,
            };
      await api(
        `/me/${kind === "welcome" ? "welcome-rules" : "keyword-rules"}${item ? `/${item.id}` : ""}`,
        {
          tenantId,
          method: item ? "PATCH" : "POST",
          signal: controller.signal,
          bodyJSON: { ...fields, ...(item ? { version: item.version } : {}) },
        },
      );
      if (mounted.current) {
        message.success("个人规则已保存");
        onSaved();
      }
    } catch (problem) {
      if (!controller.signal.aborted && mounted.current) message.error(errorMessage(problem));
    } finally {
      locked.current = false;
      if (mounted.current) setBusy(false);
    }
  }
  /** uploadImage 仅接受真实图片并锁定列表位置，避免上传结束后写入其他欢迎内容。 */
  async function uploadImage(file: File, index: number) {
    if (locked.current || (request.current && uploading !== null)) return;
    if (!file.size || file.size > 10 * 1024 * 1024) {
      message.error("请选择不超过10MB的图片");
      return;
    }
    const controller = new AbortController();
    request.current = controller;
    setUploading(index);
    try {
      const body = new FormData();
      body.append("file", file);
      const attachment = await api<NonNullable<QuickReply["attachment"]>>("/attachments", {
        tenantId,
        method: "POST",
        body,
        signal: controller.signal,
      });
      if (!attachment?.content_type?.startsWith("image/")) throw new Error("请选择实际图片文件");
      if (!controller.signal.aborted && mounted.current) {
        form.setFieldValue(["contents", index, "attachment_id"], attachment.id);
        setAttachments((current) => ({ ...current, [index]: attachment }));
      }
    } catch (problem) {
      if (!controller.signal.aborted && mounted.current) message.error(errorMessage(problem));
    } finally {
      if (request.current === controller) request.current = null;
      if (mounted.current) setUploading(null);
    }
  }
  return (
    <Modal
      title={`${item ? "编辑" : "新增"}我的${kind === "welcome" ? "欢迎语" : "关键词回复"}`}
      open
      width={680}
      onCancel={busy || uploading !== null ? undefined : onClose}
      mask={{ closable: !busy && uploading === null }}
      footer={
        <Space>
          <Button disabled={busy || uploading !== null} onClick={onClose}>
            取消
          </Button>
          <Button
            type="primary"
            loading={busy}
            disabled={uploading !== null}
            onClick={() => form.submit()}
          >
            保存
          </Button>
        </Space>
      }
    >
      <Form
        form={form}
        layout="vertical"
        disabled={busy || uploading !== null}
        onFinish={save}
        initialValues={
          item || {
            name: "",
            enabled: true,
            scenario: "all",
            contents: [{ type: "text", content: "", delay_seconds: 0 }],
            match_mode: "contains",
            pattern: "",
            reply: "",
            priority: 0,
            cooldown_seconds: 60,
          }
        }
      >
        <Form.Item
          name="name"
          label="规则名称"
          rules={[{ required: true, whitespace: true, message: "请输入规则名称" }]}
        >
          <Input maxLength={100} />
        </Form.Item>
        <Form.Item
          name="enabled"
          label="启用这条个人规则"
          valuePropName="checked"
          extra="只影响你自己的规则，不修改团队设置。"
        >
          <Switch />
        </Form.Item>
        {kind === "welcome" ? (
          <>
            <Form.Item
              name="scenario"
              label="生效时段"
              extra="营业时间沿用团队设置；每个时段最多一组，具体时段优先于全部时段。"
            >
              <Select
                options={[
                  { value: "all", label: "全部时段" },
                  { value: "business", label: "营业中" },
                  { value: "outside", label: "非营业" },
                ]}
              />
            </Form.Item>
            <p className="personal-reply-hint">
              本会话首次接单时追加个人欢迎语。延迟从接单时计算；转接后尚未发送的个人欢迎语会取消。
            </p>
            <Form.List name="contents">
              {(fields, { add, remove }) => (
                <>
                  {fields.map((field, index) => (
                    <Card
                      key={field.key}
                      size="small"
                      className="personal-welcome-content"
                      title={`欢迎内容 ${index + 1}`}
                      extra={
                        <Button
                          size="small"
                          disabled={fields.length <= 1 || busy || uploading !== null}
                          onClick={() => {
                            setAttachments({});
                            remove(field.name);
                          }}
                        >
                          移除
                        </Button>
                      }
                    >
                      <div className="personal-rule-fields">
                        <Form.Item name={[field.name, "type"]} label="内容类型">
                          <Select
                            options={[
                              { value: "text", label: "文字" },
                              { value: "image", label: "图片" },
                              { value: "options", label: "可点击选项" },
                            ]}
                            onChange={() => {
                              form.setFieldValue(["contents", index, "attachment_id"], undefined);
                              form.setFieldValue(["contents", index, "options"], undefined);
                              setAttachments((current) => {
                                const next = { ...current };
                                delete next[index];
                                return next;
                              });
                            }}
                          />
                        </Form.Item>
                        <Form.Item
                          name={[field.name, "delay_seconds"]}
                          label="接单后延迟（秒）"
                          rules={[{ required: true, message: "请输入延迟秒数" }]}
                        >
                          <InputNumber min={0} max={3600} precision={0} />
                        </Form.Item>
                      </div>
                      <Form.Item
                        name={[field.name, "content"]}
                        label={
                          contents?.[index]?.type === "image" ? "图片说明（选填）" : "欢迎文字"
                        }
                        rules={
                          contents?.[index]?.type === "text"
                            ? [{ required: true, whitespace: true, message: "请输入欢迎文字" }]
                            : []
                        }
                      >
                        <Input.TextArea rows={2} maxLength={2000} />
                      </Form.Item>
                      {contents?.[index]?.type === "image" && (
                        <>
                          <Form.Item
                            name={[field.name, "attachment_id"]}
                            label="欢迎图片"
                            rules={[{ required: true, message: "请上传欢迎图片" }]}
                            getValueProps={(value) => ({
                              value: value ? "已选择图片，保存后生效" : "",
                            })}
                          >
                            <Input readOnly placeholder="请先上传欢迎图片" />
                          </Form.Item>
                          <Upload
                            accept="image/png,image/jpeg,image/gif,image/webp"
                            showUploadList={false}
                            disabled={busy || uploading !== null}
                            beforeUpload={(file) => {
                              void uploadImage(file, index);
                              return false;
                            }}
                          >
                            <Button
                              icon={<UploadOutlined />}
                              loading={uploading === index}
                              disabled={busy || uploading !== null}
                            >
                              选择并上传图片
                            </Button>
                          </Upload>
                          {contents[index].attachment_id && (
                            <div className="personal-reply-media">
                              <AttachmentView
                                attachment={
                                  attachments[index] || {
                                    id: contents[index].attachment_id!,
                                    file_name: "欢迎图片",
                                    content_type: "image/png",
                                  }
                                }
                                identity={{ tenantId, kind: "agent", actorId: "" }}
                                messageType="image"
                              />
                            </div>
                          )}
                        </>
                      )}
                      {contents?.[index]?.type === "options" && (
                        <Form.List
                          name={[field.name, "options"]}
                          rules={[
                            {
                              validator: async (_, values) => {
                                if (!values?.length || values.length > 8)
                                  throw new Error("请配置1至8个选项");
                              },
                            },
                          ]}
                        >
                          {(options, actions, { errors }) => (
                            <>
                              {options.map((option) => (
                                <div key={option.key} className="personal-option-fields">
                                  <Form.Item name={[option.name, "id"]} hidden>
                                    <Input />
                                  </Form.Item>
                                  <Form.Item
                                    name={[option.name, "label"]}
                                    label="按钮文案"
                                    rules={[
                                      {
                                        required: true,
                                        whitespace: true,
                                        message: "请输入按钮文案",
                                      },
                                    ]}
                                  >
                                    <Input maxLength={80} />
                                  </Form.Item>
                                  <Form.Item
                                    name={[option.name, "value"]}
                                    label="点击后发送"
                                    rules={[
                                      {
                                        required: true,
                                        whitespace: true,
                                        message: "请输入发送内容",
                                      },
                                    ]}
                                  >
                                    <Input maxLength={1000} />
                                  </Form.Item>
                                  <Button
                                    danger
                                    onClick={() => actions.remove(option.name)}
                                    disabled={busy || uploading !== null}
                                  >
                                    移除选项
                                  </Button>
                                </div>
                              ))}
                              <Button
                                size="small"
                                disabled={options.length >= 8 || busy || uploading !== null}
                                onClick={() =>
                                  actions.add({ id: crypto.randomUUID(), label: "", value: "" })
                                }
                              >
                                添加选项
                              </Button>
                              <Form.ErrorList errors={errors} />
                            </>
                          )}
                        </Form.List>
                      )}
                    </Card>
                  ))}
                  <Button
                    icon={<PlusOutlined />}
                    disabled={fields.length >= 12 || busy || uploading !== null}
                    onClick={() => add({ type: "text", content: "", delay_seconds: 0 })}
                  >
                    添加欢迎内容
                  </Button>
                </>
              )}
            </Form.List>
          </>
        ) : (
          <>
            <Form.Item name="match_mode" label="匹配方式">
              <Select
                options={[
                  { value: "contains", label: "包含关键词" },
                  { value: "exact", label: "完全匹配" },
                ]}
              />
            </Form.Item>
            <Form.Item
              name="pattern"
              label="关键词"
              rules={[{ required: true, whitespace: true, message: "请输入关键词" }]}
              extra="只匹配访客新发来的文字消息。"
            >
              <Input maxLength={2000} />
            </Form.Item>
            <Form.Item
              name="reply"
              label="回复内容"
              rules={[{ required: true, whitespace: true, message: "请输入回复内容" }]}
            >
              <Input.TextArea rows={4} maxLength={2000} />
            </Form.Item>
            <div className="personal-rule-fields">
              <Form.Item
                name="priority"
                label="优先级"
                extra="数值越大越优先；相同时团队规则优先。"
              >
                <InputNumber min={-2147483648} max={2147483647} precision={0} />
              </Form.Item>
              <Form.Item
                name="cooldown_seconds"
                label="同会话冷却（秒）"
                extra="同一会话再次回复前的等待时间，0表示不限制。"
              >
                <InputNumber min={0} max={86400} precision={0} />
              </Form.Item>
            </div>
            <p className="personal-reply-hint">
              仅在你当前接待的会话生效。每条访客消息最多自动回复一次，按个人和团队规则的优先级选择。
            </p>
          </>
        )}
      </Form>
    </Modal>
  );
}
