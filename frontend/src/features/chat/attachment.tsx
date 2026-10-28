/** 文件作用说明：以授权请求读取私有聊天附件，访客凭证始终留在请求头中。 */
"use client";
import { useEffect, useState } from "react";
import { Button, Image, Spin } from "antd";
import { DownloadOutlined } from "@ant-design/icons";
import { VoiceCard } from "./voice-card";
import type { ChatIdentity } from "./use-chat";
/** AttachmentView 下载私有附件生成临时 Blob 地址，卸载后释放。 */
export function AttachmentView({
  attachment,
  identity,
  messageType,
}: {
  attachment: { id: string; file_name: string; content_type: string };
  identity: ChatIdentity;
  messageType?: string;
}) {
  const [url, setURL] = useState("");
  const [failed, setFailed] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    let objectURL = "";
    setFailed(false);
    setURL("");
    fetch(`/api/v1/attachments/${attachment.id}`, {
      credentials: "same-origin",
      headers: {
        ...(identity.token ? { Authorization: `Bearer ${identity.token}` } : {}),
        ...(identity.tenantId ? { "X-Tenant-ID": identity.tenantId } : {}),
      },
      signal: controller.signal,
    })
      .then((response) => {
        if (!response.ok) throw new Error("无权读取附件或附件已撤回");
        return response.blob();
      })
      .then((blob) => {
        if (controller.signal.aborted) return;
        objectURL = URL.createObjectURL(blob);
        setURL(objectURL);
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailed(true);
      });
    return () => {
      controller.abort();
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [attachment.id, identity.token, identity.tenantId, revision]);
  if (failed)
    return (
      <Button size="small" onClick={() => setRevision((value) => value + 1)}>
        附件读取失败，点击重试
      </Button>
    );
  if (!url) return <Spin size="small" />;
  if (attachment.content_type.startsWith("image/"))
    return (
      <Image
        src={url}
        alt={attachment.file_name}
        style={{ maxWidth: 260, maxHeight: 260, objectFit: "contain" }}
      />
    );
  if (
    attachment.content_type.startsWith("audio/") ||
    (attachment.content_type === "video/webm" && messageType !== "file")
  )
    return <VoiceCard url={url} name={attachment.file_name} />;
  if (attachment.content_type.startsWith("video/"))
    return (
      <video
        className="message-video"
        src={url}
        controls
        playsInline
        preload="metadata"
        aria-label={attachment.file_name}
      />
    );
  return (
    <Button icon={<DownloadOutlined />} href={url} download={attachment.file_name}>
      {attachment.file_name}
    </Button>
  );
}
