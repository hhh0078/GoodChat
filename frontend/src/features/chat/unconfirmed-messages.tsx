/** 文件作用说明：在可信访客父页面呈现待确认消息并由用户手动以原编号重试，不向装修iframe暴露暂存。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { Alert, Button } from "antd";
import { errorMessage } from "@/lib/api";
import type { Outgoing } from "./use-chat";

/** UnconfirmedMessages 只在消息快照加载后显示原身份暂存，重试直接提交原完整消息体。 */
export function UnconfirmedMessages({
  items,
  loading,
  pendingError,
  onRetry,
}: {
  items: Outgoing[];
  loading: boolean;
  pendingError: unknown;
  onRetry: (body: Outgoing) => Promise<unknown>;
}) {
  const [retrying, setRetrying] = useState("");
  const [error, setError] = useState<unknown>(null);
  const lock = useRef(false);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  /** retry 仅由用户点击触发，服务器确认前保留原记录且不生成新的业务编号。 */
  async function retry(item: Outgoing) {
    if (lock.current) return;
    lock.current = true;
    setRetrying(item.client_message_id);
    setError(null);
    try {
      await onRetry(item);
    } catch (problem) {
      if (mounted.current) setError(problem);
    } finally {
      lock.current = false;
      if (mounted.current) setRetrying("");
    }
  }
  if (loading || !items.length) return null;
  return (
    <section
      aria-label="待确认消息恢复"
      style={{
        padding: "10px 12px",
        background: "#fff8ea",
        borderBottom: "1px solid #eed8a5",
        flexShrink: 0,
      }}
    >
      <strong style={{ display: "block", fontSize: 13 }}>
        有 {items.length} 条消息等待服务器确认
      </strong>
      <p style={{ fontSize: 12, color: "#795922", margin: "4px 0 8px" }}>
        这些内容保留在当前浏览器中。点击重试会使用原消息编号，服务器已收到的消息不会重复发送。
      </p>
      {(!!pendingError || !!error) && (
        <Alert
          type="error"
          showIcon
          title={errorMessage(error || pendingError)}
          style={{ marginBottom: 8 }}
        />
      )}
      <ol style={{ padding: 0, margin: 0, listStyle: "none", maxHeight: 160, overflowY: "auto" }}>
        {items.map((item) => (
          <li
            key={item.client_message_id}
            style={{
              display: "flex",
              gap: 10,
              alignItems: "center",
              padding: "6px 0",
              borderTop: "1px solid #eedfbd",
            }}
          >
            <div
              style={{
                flex: 1,
                minWidth: 0,
                fontSize: 12,
                overflowWrap: "anywhere",
                whiteSpace: "pre-wrap",
              }}
            >
              {item.content ||
                (
                  { image: "图片消息", audio: "语音消息", file: "文件消息" } as Record<
                    string,
                    string
                  >
                )[item.message_type] ||
                "消息"}
              {item.attachment_id && (
                <span style={{ display: "block", fontSize: 11, color: "#795922" }}>
                  附件已上传，等待消息确认
                </span>
              )}
            </div>
            <Button
              size="small"
              loading={retrying === item.client_message_id}
              disabled={!!retrying && retrying !== item.client_message_id}
              onClick={() => void retry(item)}
              aria-label={`重试待确认${item.message_type === "text" ? "文字" : "附件"}消息`}
            >
              重试发送
            </Button>
          </li>
        ))}
      </ol>
    </section>
  );
}
