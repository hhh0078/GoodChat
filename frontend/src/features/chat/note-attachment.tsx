/** 文件作用说明：下载兼容旧内部备注的私有附件，权限由客户备注专用接口实时核验。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { App, Button } from "antd";
import { DownloadOutlined } from "@ant-design/icons";
import { errorMessage } from "@/lib/api";
type NoteFile = {
  file_name: string;
  content_type: string;
  size_bytes?: number;
  download_path: string;
};
/** NoteAttachment 只向当前客户备注固定同源路径发送租户头，不复用旧会话的宽泛附件权限。 */
export function NoteAttachment({
  attachment,
  contactId,
  noteId,
  tenantId,
}: {
  attachment: NoteFile;
  contactId: string;
  noteId: string;
  tenantId: string;
}) {
  const { message } = App.useApp();
  const [busy, setBusy] = useState(false);
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);
  /** download 取得完整私有正文后才创建下载链接，失败或失权保持错误提示。 */
  async function download() {
    if (busy) return;
    const expected = `/tenant/contacts/${contactId}/notes/${noteId}/attachment`;
    if (attachment.download_path !== expected) {
      message.error("备注附件下载地址无效，请刷新客户资料。");
      return;
    }
    setBusy(true);
    const active = new AbortController();
    controller.current = active;
    let url = "";
    try {
      const response = await fetch(`/api/v1${expected}`, {
        credentials: "same-origin",
        headers: { "X-Tenant-ID": tenantId },
        cache: "no-store",
        signal: AbortSignal.any([active.signal, AbortSignal.timeout(30000)]),
      });
      if (!response.ok) {
        const error = await response.json().catch(() => null);
        throw new Error(error?.message || "无权读取此附件，附件可能已撤回。");
      }
      const blob = await response.blob();
      if (active.signal.aborted) return;
      url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = attachment.file_name || "备注附件";
      document.body.append(link);
      link.click();
      link.remove();
    } catch (problem) {
      if (!active.signal.aborted) message.error(errorMessage(problem));
    } finally {
      if (url) {
        const downloadedURL = url;
        setTimeout(() => URL.revokeObjectURL(downloadedURL), 1000);
      }
      if (!active.signal.aborted) setBusy(false);
    }
  }
  return (
    <Button
      className="note-attachment"
      size="small"
      icon={<DownloadOutlined />}
      loading={busy}
      onClick={() => void download()}
      title={attachment.file_name}
    >
      {attachment.file_name || "备注附件"}
    </Button>
  );
}
