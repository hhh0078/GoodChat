/** 文件作用说明：渲染访客可见消息、日期分组、附件、引用和已读状态，内部备注不进入对话流。 */
"use client";
import { useFormats } from "@/i18n/use-formats";
import { useTranslator } from "@/i18n/use-translator";
import { messageDisplay, type MessageDisplayContext } from "@/lib/message-display";
import type { ChatMessage } from "@/lib/types";
import { Avatar, Button, Popconfirm, Space } from "antd";
import { Fragment, useState } from "react";
import { MessageForm } from "./message-form";
import { AttachmentView } from "./attachment";
import type { ChatIdentity, Outgoing } from "./use-chat";
/** MessageHistory 只呈现已确认消息，所有发送与撤回动作仍由当前会话控制。 */
export function MessageHistory({
  messages,
  identity,
  canSend,
  sending,
  readSeq,
  displayContext,
  onReply,
  onWithdraw,
  onSend,
}: {
  messages: ChatMessage[];
  identity: ChatIdentity;
  canSend: boolean;
  sending: boolean;
  readSeq: string;
  displayContext?: MessageDisplayContext;
  onReply: (message: ChatMessage) => void;
  onWithdraw: (message: ChatMessage) => Promise<void>;
  onSend: (body: Outgoing) => Promise<unknown>;
}) {
  const { currentPreferences, numberFormatter } = useFormats();

  /** messageDateLabel 将真实消息时间转换为本地日期标题，异常时间不生成虚构分隔日期。 */
  function messageDateLabel(value?: string): string {
    if (!value) return "";
    const date = new Date(value);
    if (!Number.isFinite(date.getTime())) return "";
    return date.toLocaleDateString(currentPreferences().locale, {
      timeZone: currentPreferences().timezone,
      ...{
        year: "numeric",
        month: "long",
        day: "numeric",
      },
    });
  }

  const tr = useTranslator();
  const [selectedMessage, setSelectedMessage] = useState("");
  return (
    <>
      {messages.map((item, index) => {
        const display = messageDisplay(item, identity, displayContext);
        const own = display.own;
        const dateLabel = identity.kind === "agent" ? messageDateLabel(item.created_at) : "";
        const showDate =
          !!dateLabel && dateLabel !== messageDateLabel(messages[index - 1]?.created_at);
        return (
          <Fragment key={item.id}>
            {showDate && <div className="message-day-divider">{dateLabel}</div>}
            <div
              className={`message-row ${display.alignRight ? "own" : ""} ${item.audience === "internal" ? "internal" : ""}`}
              data-client-message-id={item.client_message_id}
              data-message-id={item.id}
            >
              <Avatar size={32} className="message-avatar" src={display.avatarURL || undefined}>
                {display.senderName.slice(0, 1)}
              </Avatar>
              <div className="message-meta">
                {item.audience === "internal" ? tr("内部备注 \u00B7 ") : ""}
                {display.senderName} · {display.sourceLabel && <>{display.sourceLabel} · </>}
                {item.created_at
                  ? new Date(item.created_at).toLocaleTimeString(currentPreferences().locale, {
                      timeZone: currentPreferences().timezone,
                      ...{
                        hour: "2-digit",
                        minute: "2-digit",
                      },
                    })
                  : ""}
              </div>
              <div
                className={`message-bubble ${identity.kind === "visitor" ? "is-selectable" : ""}`}
                tabIndex={identity.kind === "visitor" && !item.withdrawn_at ? 0 : undefined}
                aria-label={identity.kind === "visitor" ? tr("选择消息以引用或撤回") : undefined}
                onClick={() =>
                  identity.kind === "visitor" &&
                  setSelectedMessage((previous) => (previous === item.id ? "" : item.id))
                }
                onKeyDown={(event) => {
                  if (
                    identity.kind === "visitor" &&
                    event.target === event.currentTarget &&
                    (event.key === "Enter" || event.key === " ")
                  ) {
                    event.preventDefault();
                    setSelectedMessage((previous) => (previous === item.id ? "" : item.id));
                  }
                }}
              >
                {item.withdrawn_at ? (
                  <span className="muted">{tr("消息已撤回")}</span>
                ) : (
                  <>
                    {item.reply && (
                      <div
                        style={{
                          fontSize: 12,
                          borderLeft: "2px solid var(--line)",
                          paddingLeft: 10,
                          marginBottom: 8,
                          color: "var(--muted)",
                        }}
                      >
                        {tr("引用：")}
                        {item.reply.withdrawn ? tr("消息已撤回") : item.reply.content}
                      </div>
                    )}
                    {item.content}
                    {item.message_type === "form" && (
                      <MessageForm
                        item={item}
                        submission={messages.find((m) => m.form_request_id === item.id)}
                        canFill={identity.kind === "visitor" && canSend && !sending}
                        onSend={onSend}
                      />
                    )}
                    {item.attachment && (
                      <div
                        onClick={(event) => event.stopPropagation()}
                        onKeyDown={(event) => event.stopPropagation()}
                      >
                        <AttachmentView
                          attachment={item.attachment}
                          messageType={item.message_type}
                          identity={identity}
                        />
                      </div>
                    )}
                    {(item.sender_kind === "system" || item.sender_kind === "agent") &&
                      Array.isArray(item.options) &&
                      item.options.length > 0 && (
                        <div
                          className="message-options"
                          onClick={(event) => event.stopPropagation()}
                        >
                          {item.options.map((option) => (
                            <Button
                              key={option.id}
                              className="message-option"
                              disabled={
                                !canSend ||
                                identity.kind !== "visitor" ||
                                sending ||
                                messages.some(
                                  (message) =>
                                    message.client_message_id === option.client_message_id,
                                )
                              }
                              onClick={() =>
                                void onSend({
                                  client_message_id: option.client_message_id,
                                  content: option.value,
                                  message_type: "text",
                                  audience: "public",
                                })
                              }
                            >
                              {option.label}
                            </Button>
                          ))}
                        </div>
                      )}
                  </>
                )}
              </div>
              {!item.withdrawn_at && (
                <Space className="message-actions" size={10} style={{ fontSize: 11, marginTop: 4 }}>
                  {canSend && (identity.kind === "agent" || selectedMessage === item.id) && (
                    <Button
                      type="link"
                      size="small"
                      onClick={() => {
                        onReply(item);
                      }}
                    >
                      {tr("引用 ")}
                    </Button>
                  )}
                  {own && canSend && (identity.kind === "agent" || selectedMessage === item.id) && (
                    <Popconfirm title={tr("撤回这条消息？")} onConfirm={() => onWithdraw(item)}>
                      <Button type="link" size="small">
                        {tr("撤回 ")}
                      </Button>
                    </Popconfirm>
                  )}
                  {own && BigInt(readSeq) >= BigInt(item.seq || item.event_seq || 0) && (
                    <span className="muted">{tr("对方已读")}</span>
                  )}
                </Space>
              )}
            </div>
          </Fragment>
        );
      })}
    </>
  );
}
