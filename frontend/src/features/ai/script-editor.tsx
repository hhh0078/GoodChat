/** 文件作用说明：编辑AI话术的使用条件及有序富媒体块，在当前文本光标处插入服务器媒体。 */
"use client";
import { showRequestError } from "@/lib/request-error";
import { useSession } from "@/features/auth/session";
import { AttachmentView } from "@/features/chat/attachment";
import { languageNames } from "@/i18n/preferences";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import { insertMediaBlock } from "@/lib/ai-content";
import { useResource } from "@/lib/use-resource";
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  DeleteOutlined,
  PictureOutlined,
  AudioOutlined,
  VideoCameraOutlined,
} from "@ant-design/icons";
import { App, Button, Form, Input, Modal, Select, Space, Switch } from "antd";
import { useRef, useState } from "react";
import { AIMediaPicker } from "./media";
import { aiLanguages, type AIBlock, type AIContent, type AIMedia } from "./types";

/** AIScriptEditor 只提交稳定媒体编号，服务器重新验证租户和素材状态。 */
export function AIScriptEditor({
  item,
  onClose,
  onSaved,
}: {
  item: AIContent | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const tr = useTranslator();
  const { message } = App.useApp();
  const { current, tenantId } = useSession();
  const member = current.memberships.find((m) => m.tenant_id === tenantId);
  const [form] = Form.useForm();
  const [blocks, setBlocks] = useState<AIBlock[]>(item?.blocks || [{ type: "text", text: "" }]);
  const [busy, setBusy] = useState(false);
  const [picking, setPicking] = useState("");
  const media = useResource<AIMedia>("/tenant/ai/media");
  const [added, setAdded] = useState<AIMedia[]>([]);
  const cursor = useRef({
    index: blocks.length - 1,
    start: blocks.at(-1)?.text?.length || 0,
    end: blocks.at(-1)?.text?.length || 0,
  });
  /** save 验证描述及块数量后持久保存，失败保留当前输入。 */
  async function save() {
    try {
      const values = await form.validateFields();
      const valid = blocks.filter((b) => b.type !== "text" || b.text?.trim());
      if (!valid.length || valid.length > 12) throw new Error(tr("话术需要1到12个内容块"));
      setBusy(true);
      await api(`/tenant/ai/scripts${item ? `/${item.id}` : ""}`, {
        method: item ? "PATCH" : "POST",
        tenantId,
        bodyJSON: { ...values, blocks: valid, version: item?.version || 0 },
      });
      onSaved();
    } catch (error) {
      if (!(error && typeof error === "object" && "errorFields" in error)) showRequestError(error);
    } finally {
      setBusy(false);
    }
  }
  /** insert 保存光标前后文字，并在选区处插入媒体块。 */
  function insert(asset: AIMedia) {
    const type = asset.attachment.content_type.split("/")[0] as AIBlock["type"];
    setBlocks((old) =>
      insertMediaBlock(old, cursor.current.index, cursor.current.start, cursor.current.end, {
        type,
        media_id: asset.id,
      }),
    );
    setAdded((old) => [...old, asset]);
    cursor.current.index = -1;
  }
  /** move 通过按钮调整发送顺序，为拖动提供键盘替代。 */
  function move(index: number, offset: number) {
    setBlocks((old) => {
      const next = [...old];
      [next[index], next[index + offset]] = [next[index + offset], next[index]];
      return next;
    });
    cursor.current.index = -1;
  }
  return (
    <Modal
      open
      width={780}
      title={item ? tr("编辑AI话术") : tr("新增AI话术")}
      onCancel={busy ? undefined : onClose}
      onOk={() => void save()}
      confirmLoading={busy}
      cancelText={tr("取消")}
      okText={tr("保存")}
    >
      <Form
        form={form}
        layout="vertical"
        disabled={busy}
        initialValues={{
          title: item?.title || "",
          description: item?.description || "",
          language: item?.language || "zh-CN",
          enabled: item?.enabled ?? true,
        }}
      >
        <Form.Item name="title" label={tr("标题")} rules={[{ required: true, whitespace: true }]}>
          <Input maxLength={100} />
        </Form.Item>
        <Form.Item
          name="description"
          label={tr("使用描述")}
          extra={tr("说明什么时候应该使用，例如：用户询问安装步骤时发送这段演示视频。")}
          rules={[{ required: true, whitespace: true }]}
        >
          <Input.TextArea rows={2} maxLength={400} />
        </Form.Item>
        <Form.Item name="language" label={tr("适用语言")}>
          <Select options={aiLanguages.map((value) => ({ value, label: languageNames[value] }))} />
        </Form.Item>
        <div className="ai-blocks">
          {blocks.map((block, index) => {
            const asset = [...media.items, ...added].find((m) => m.id === block.media_id);
            return (
              <div className="ai-block" key={index}>
                <div className="ai-toolbar">
                  <span>
                    {index + 1} ·{" "}
                    {block.type === "text"
                      ? tr("文字")
                      : block.type === "image"
                        ? tr("图片")
                        : block.type === "video"
                          ? tr("视频")
                          : tr("语音")}
                  </span>
                  <Space>
                    <Button
                      aria-label={tr("上移")}
                      size="small"
                      icon={<ArrowUpOutlined />}
                      disabled={busy || index === 0}
                      onClick={() => move(index, -1)}
                    />
                    <Button
                      aria-label={tr("下移")}
                      size="small"
                      icon={<ArrowDownOutlined />}
                      disabled={busy || index === blocks.length - 1}
                      onClick={() => move(index, 1)}
                    />
                    <Button
                      aria-label={tr("删除")}
                      size="small"
                      icon={<DeleteOutlined />}
                      disabled={busy}
                      onClick={() => {
                        setBlocks((old) => old.filter((_, i) => i !== index));
                        cursor.current.index = -1;
                      }}
                    />
                  </Space>
                </div>
                {block.type === "text" ? (
                  <Input.TextArea
                    aria-label={tr("话术正文")}
                    rows={3}
                    value={block.text}
                    disabled={busy}
                    onChange={(e) => {
                      const text = e.target.value;
                      setBlocks((old) =>
                        old.map((b, i) => (i === index ? { type: "text", text } : b)),
                      );
                    }}
                    onSelect={(e) => {
                      cursor.current = {
                        index,
                        start: e.currentTarget.selectionStart,
                        end: e.currentTarget.selectionEnd,
                      };
                    }}
                  />
                ) : asset ? (
                  <AttachmentView
                    attachment={asset.attachment}
                    messageType={block.type === "video" ? "file" : undefined}
                    identity={{ kind: "agent", actorId: member?.id || "", tenantId }}
                  />
                ) : (
                  <span>{tr("媒体不可用或已停用")}</span>
                )}
              </div>
            );
          })}
        </div>
        <div className="ai-editor-tools">
          <Space wrap>
            <Button
              disabled={busy || blocks.length >= 12}
              onClick={() => {
                setBlocks((old) => [...old, { type: "text", text: "" }]);
                cursor.current.index = -1;
              }}
            >
              {tr("添加文字")}
            </Button>
            <Button
              disabled={busy || blocks.length >= 12}
              icon={<PictureOutlined />}
              onClick={() => setPicking("image")}
            >
              {tr("图片")}
            </Button>
            <Button
              disabled={busy || blocks.length >= 12}
              icon={<VideoCameraOutlined />}
              onClick={() => setPicking("video")}
            >
              {tr("视频")}
            </Button>
            <Button
              disabled={busy || blocks.length >= 12}
              icon={<AudioOutlined />}
              onClick={() => setPicking("audio")}
            >
              {tr("语音")}
            </Button>
            <Button disabled={busy || blocks.length >= 12} onClick={() => setPicking("all")}>
              {tr("媒体库")}
            </Button>
          </Space>
        </div>
        <Form.Item name="enabled" label={tr("启用")} valuePropName="checked">
          <Switch />
        </Form.Item>
      </Form>
      {picking && (
        <AIMediaPicker
          type={picking === "all" ? undefined : picking}
          onClose={() => setPicking("")}
          onSelect={insert}
        />
      )}
    </Modal>
  );
}
