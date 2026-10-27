/** 文件作用说明：为所有装修页面提供可信父层附件入口，适配浏览器差异并保留原编号重试。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { Alert, App, Button, Space, Tooltip } from "antd";
import { PaperClipOutlined } from "@ant-design/icons";
import { errorMessage } from "@/lib/api";
type PendingAttachment = { file: File; requestId: string };

/** AttachmentSender 从系统通用入口选择文件，与装修SDK复用同一上传发送方法。 */
export function AttachmentSender({
  disabled,
  onSend,
}: {
  disabled: boolean;
  onSend: (file: File, name: string, requestId: string) => Promise<unknown>;
}) {
  const input = useRef<HTMLInputElement | null>(null);
  const pending = useRef<PendingAttachment | null>(null);
  const mounted = useRef(true);
  const sending = useRef(false);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const { message } = App.useApp();
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      pending.current = null;
    };
  }, []);
  /** sendAttachment 选择或重试同一附件，只在服务端确认消息后显示发送成功。 */
  async function sendAttachment(file?: File) {
    if (disabled || sending.current) return;
    if (file) pending.current = { file, requestId: `parent:${crypto.randomUUID()}` };
    const operation = pending.current;
    if (!operation) return;
    sending.current = true;
    setUploading(true);
    setError(null);
    try {
      await onSend(operation.file, operation.file.name, operation.requestId);
      if (!mounted.current) return;
      pending.current = null;
      message.success("附件已发送");
    } catch (problem) {
      if (mounted.current) setError(problem);
    } finally {
      sending.current = false;
      if (mounted.current) setUploading(false);
    }
  }
  return (
    <div style={{ padding: "8px 14px", borderTop: "1px solid #e7ece4", background: "#fafbf8" }}>
      <Space size={10} wrap>
        <input
          hidden
          type="file"
          ref={input}
          aria-label="系统附件文件"
          onChange={(event) => {
            const file = event.target.files?.[0];
            event.target.value = "";
            if (file) void sendAttachment(file);
          }}
        />
        <Tooltip title="页面内附件选择不可用时，可使用此系统通用入口">
          <Button
            size="small"
            icon={<PaperClipOutlined />}
            disabled={disabled || !!pending.current}
            loading={uploading}
            onClick={() => input.current?.click()}
          >
            发送附件
          </Button>
        </Tooltip>
        <span className="muted" style={{ fontSize: 11 }}>
          系统通用入口 · 最大20MB
        </span>
      </Space>
      {!!error && (
        <Alert
          style={{ marginTop: 8 }}
          type="error"
          title={errorMessage(error)}
          description="发送结果未确认，请先查看会话后再重试。"
          action={
            <Space>
              <Button
                size="small"
                disabled={disabled}
                loading={uploading}
                onClick={() => void sendAttachment()}
              >
                重试附件
              </Button>
              <Button
                size="small"
                disabled={uploading}
                onClick={() => {
                  pending.current = null;
                  setError(null);
                }}
              >
                暂不重试
              </Button>
            </Space>
          }
        />
      )}
    </div>
  );
}
