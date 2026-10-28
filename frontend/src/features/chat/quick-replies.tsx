/** 文件作用说明：用紧凑纯文字标题展示个人和公共富话术，双击成功后按真实使用次数排序。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { App, Button, Tooltip } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import { api, errorMessage } from "@/lib/api";
import { groupQuickReplies, type QuickReply } from "@/lib/quick-replies";
import { QuickReplyEditor } from "./quick-reply-editor";
import "./quick-replies.css";
type QuickReplyProps = {
  tenantId: string;
  manages: boolean;
  disabled: boolean;
  onSend: (item: QuickReply) => Promise<boolean>;
};

/** QuickReplies 按租户重建内容，防止切换身份后继续维护旧话术。 */
export function QuickReplies(props: QuickReplyProps) {
  return <QuickReplyList key={props.tenantId} {...props} />;
}

/** QuickReplyList 只展示可发送标题和添加入口，维护动作集中在弹框中。 */
function QuickReplyList({ tenantId, manages, disabled, onSend }: QuickReplyProps) {
  const { message } = App.useApp();
  const [items, setItems] = useState<QuickReply[]>([]);
  const [editing, setEditing] = useState<QuickReply | null | undefined>();
  const [revision, setRevision] = useState(0);
  const [sending, setSending] = useState(false);
  const locked = useRef(false),
    mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    api<{ items: QuickReply[] }>("/tenant/quick-replies", { tenantId, signal: controller.signal })
      .then((data) => {
        if (!controller.signal.aborted) setItems(data.items || []);
      })
      .catch((problem) => {
        if (!controller.signal.aborted) message.error(errorMessage(problem));
      });
    return () => controller.abort();
  }, [tenantId, revision, message]);
  /** send 防止连续双击并发，操作后重读真实次数与版本，不在客户端猜测成功或累计次数。 */
  async function send(item: QuickReply) {
    if (disabled || locked.current) return;
    locked.current = true;
    setSending(true);
    try {
      await onSend(item);
    } catch (problem) {
      if (mounted.current) message.error(errorMessage(problem));
    } finally {
      locked.current = false;
      if (mounted.current) {
        setSending(false);
        setRevision((value) => value + 1);
      }
    }
  }
  const groups = groupQuickReplies(items);
  return (
    <aside className="composer-quick-replies compact-quick-replies" aria-label="快捷回复">
      <Tooltip title="添加和管理快捷回复">
        <Button
          type="text"
          size="small"
          className="quick-reply-add"
          icon={<PlusOutlined />}
          aria-label="添加快捷回复"
          onClick={() => setEditing(null)}
        />
      </Tooltip>
      <div className="quick-reply-list">
        {groups.personal.map((item) => (
          <button
            key={item.id}
            className="quick-reply-title"
            type="button"
            disabled={disabled || sending}
            onDoubleClick={() => void send(item)}
            title={item.content || item.title}
          >
            {item.title}
          </button>
        ))}
        {!!groups.personal.length && !!groups.shared.length && (
          <hr className="quick-reply-divider" />
        )}
        {groups.shared.map((item) => (
          <button
            key={item.id}
            className="quick-reply-title"
            type="button"
            disabled={disabled || sending}
            onDoubleClick={() => void send(item)}
            title={item.content || item.title}
          >
            {item.title}
          </button>
        ))}
      </div>
      {editing !== undefined && (
        <QuickReplyEditor
          tenantId={tenantId}
          manages={manages}
          items={items.filter((item) => item.enabled !== false)}
          item={editing}
          onSelect={setEditing}
          onClose={() => setEditing(undefined)}
          onSaved={() => {
            setEditing(undefined);
            setRevision((value) => value + 1);
          }}
        />
      )}
    </aside>
  );
}
