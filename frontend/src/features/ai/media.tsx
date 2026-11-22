/** 文件作用说明：在租户素材库上传、预览、筛选及选择媒体，复用授权附件读取组件。 */
"use client";
import { showRequestError } from "@/lib/request-error";
import { ErrorNotice } from "@/components/page-state";
import { useSession } from "@/features/auth/session";
import { AttachmentView } from "@/features/chat/attachment";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import { DeleteOutlined, EditOutlined, UploadOutlined } from "@ant-design/icons";
import { App, Button, Card, Empty, Input, Modal, Popconfirm, Select, Space, Spin, Tag } from "antd";
import { useEffect, useRef, useState } from "react";
import type { AIMedia } from "./types";

/** AIMediaLibrary 只列出主动导入的团队素材，不扩大聊天私有附件的可见范围。 */
export function AIMediaLibrary({
  onSelect,
  acceptType,
}: {
  onSelect?: (item: AIMedia) => void;
  acceptType?: string;
}) {
  const tr = useTranslator();
  const { message } = App.useApp();
  const { tenantId, current } = useSession();
  const data = useResource<AIMedia>("/tenant/ai/media");
  const member = current.memberships.find((m) => m.tenant_id === tenantId);
  const manages = member?.role === "owner" || member?.role === "admin";
  const [search, setSearch] = useState("");
  const [type, setType] = useState(acceptType || "all");
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState<AIMedia | null>(null);
  const [title, setTitle] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  /** upload 将文件保存到服务器后显式加入团队素材库，成功才允许插入话术。 */
  async function upload(file?: File) {
    if (!file || busy) return;
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    try {
      const body = new FormData();
      body.append("file", file);
      const attachment = await api<AIMedia["attachment"]>("/attachments", {
        method: "POST",
        tenantId,
        body,
        signal: controller.signal,
      });
      if (acceptType && !attachment.content_type.startsWith(`${acceptType}/`))
        throw new Error(tr("附件内容与消息类型不一致"));
      await api("/tenant/ai/media", {
        method: "POST",
        tenantId,
        bodyJSON: { attachment_id: attachment.id, title: file.name },
        signal: controller.signal,
      });
      if (!controller.signal.aborted) {
        data.reload();
        onSelect?.({
          id: attachment.id,
          title: file.name,
          editable: true,
          references: 0,
          attachment,
        });
      }
    } catch (error) {
      if (!controller.signal.aborted) showRequestError(error);
    } finally {
      if (!controller.signal.aborted) setBusy(false);
      if (input.current) input.current.value = "";
    }
  }
  /** remove 停用素材，历史消息中的原附件引用继续保留。 */
  async function remove(id: string) {
    try {
      await api(`/tenant/ai/media/${id}`, { method: "DELETE", tenantId });
      data.reload();
    } catch (error) {
      showRequestError(error);
    }
  }
  /** rename 只允许上传者修改素材名称，失败保留输入。 */
  async function rename() {
    if (!editing || !title.trim()) return;
    setBusy(true);
    try {
      await api(`/tenant/ai/media/${editing.id}`, {
        method: "PATCH",
        tenantId,
        bodyJSON: { title },
      });
      setEditing(null);
      data.reload();
    } catch (error) {
      showRequestError(error);
    } finally {
      setBusy(false);
    }
  }
  const items = data.items.filter(
    (item) =>
      item.title.toLowerCase().includes(search.toLowerCase()) &&
      (type === "all" || item.attachment.content_type.startsWith(`${type}/`)),
  );
  return (
    <div className="ai-resource">
      <ErrorNotice error={data.error} retry={data.reload} />
      <div className="ai-toolbar">
        <Space wrap>
          <Input
            aria-label={tr("搜索素材")}
            placeholder={tr("搜索素材")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            allowClear
          />
          <Select
            aria-label={tr("媒体类型")}
            value={type}
            onChange={setType}
            disabled={!!acceptType}
            options={[
              { value: "all", label: tr("全部") },
              { value: "image", label: tr("图片") },
              { value: "video", label: tr("视频") },
              { value: "audio", label: tr("语音") },
            ]}
          />
        </Space>
        <Button
          icon={<UploadOutlined />}
          type="primary"
          loading={busy}
          onClick={() => input.current?.click()}
        >
          {tr("上传媒体")}
        </Button>
      </div>
      <input
        hidden
        ref={input}
        type="file"
        accept={acceptType ? `${acceptType}/*` : "image/*,video/*,audio/*"}
        onChange={(e) => void upload(e.target.files?.[0])}
      />
      <p className="ai-help">
        {tr("支持图片、视频和语音。素材存储在服务器，历史消息中的媒体不会因停用而消失。")}
      </p>
      {data.loading ? (
        <Spin />
      ) : !items.length ? (
        <Empty />
      ) : (
        <div className="ai-media-grid">
          {items.map((item) => (
            <Card
              key={item.id}
              size="small"
              title={<span title={item.title}>{item.title}</span>}
              extra={<Tag>{Math.ceil(item.attachment.size_bytes / 1024)} KB</Tag>}
            >
              <div className="ai-media-preview">
                <AttachmentView
                  attachment={item.attachment}
                  messageType={
                    item.attachment.content_type.startsWith("video/") ? "file" : undefined
                  }
                  identity={{ kind: "agent", actorId: member?.id || "", tenantId }}
                />
              </div>
              <div className="ai-toolbar">
                <span>{tr("引用：{p0}", { p0: item.references })}</span>
                <Space>
                  {onSelect && (
                    <Button
                      type="primary"
                      size="small"
                      disabled={busy}
                      onClick={() => onSelect(item)}
                    >
                      {tr("选择")}
                    </Button>
                  )}
                  {item.editable && (
                    <Button
                      aria-label={tr("编辑")}
                      size="small"
                      icon={<EditOutlined />}
                      disabled={busy}
                      onClick={() => {
                        setEditing(item);
                        setTitle(item.title);
                      }}
                    />
                  )}
                  {(item.editable || manages) && (
                    <Popconfirm title={tr("停用这份素材？")} onConfirm={() => remove(item.id)}>
                      <Button aria-label={tr("停用")} icon={<DeleteOutlined />} size="small" />
                    </Popconfirm>
                  )}
                </Space>
              </div>
            </Card>
          ))}
        </div>
      )}
      {editing && (
        <Modal
          open
          title={tr("编辑")}
          onCancel={busy ? undefined : () => setEditing(null)}
          onOk={() => void rename()}
          confirmLoading={busy}
          okText={tr("保存")}
          cancelText={tr("取消")}
        >
          <Input
            aria-label={tr("名称")}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            maxLength={100}
          />
        </Modal>
      )}
    </div>
  );
}
/** AIMediaPicker 按每次弹窗生命周期隔离上传请求与光标回调。 */
export function AIMediaPicker({
  onClose,
  onSelect,
  type,
}: {
  onClose: () => void;
  onSelect: (item: AIMedia) => void;
  type?: string;
}) {
  const tr = useTranslator();
  return (
    <Modal open title={tr("媒体库")} width={850} footer={null} onCancel={onClose} destroyOnHidden>
      <AIMediaLibrary
        acceptType={type}
        onSelect={(item) => {
          onSelect(item);
          onClose();
        }}
      />
    </Modal>
  );
}
