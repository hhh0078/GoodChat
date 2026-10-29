/** 文件作用说明：在客服工作台统一维护个人快捷回复、欢迎语和关键词，并展示已启用的公共只读内容。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { App, Button, Drawer, Empty, Popconfirm, Space, Spin, Switch, Tabs, Tag } from "antd";
import { LockOutlined, PlusOutlined } from "@ant-design/icons";
import { api, errorMessage } from "@/lib/api";
import { ErrorNotice } from "@/components/page-state";
import type { QuickReply } from "@/lib/quick-replies";
import {
  replySourceNames,
  welcomeScenarioNames,
  type PersonalKeyword,
  type PersonalRule,
  type PersonalWelcome,
} from "@/lib/personal-replies";
import { QuickReplyEditor } from "./quick-reply-editor";
import { PersonalAutomationEditor } from "./personal-automation-editor";
import { AttachmentView } from "./attachment";
import "./personal-reply-settings.css";

type SettingsProps = {
  tenantId: string;
  items: QuickReply[];
  onChanged: () => void;
  onClose: () => void;
};
type EditingRule =
  | { kind: "welcome"; item: PersonalWelcome | null }
  | { kind: "keyword"; item: PersonalKeyword | null };

/** usePersonalRules 读取当前身份的规则，切换身份或卸载时废止旧请求。 */
function usePersonalRules<T extends PersonalRule>(
  tenantId: string,
  path: string,
  revision: number,
) {
  const [items, setItems] = useState<T[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    api<{ items: T[] }>(path, { tenantId, signal: controller.signal })
      .then((data) => {
        if (!controller.signal.aborted) setItems(data?.items || []);
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [tenantId, path, revision]);
  return { items, loading, error };
}

/** PersonalReplySettings 按租户重新挂载，阻止上个身份的表单和请求残留。 */
export function PersonalReplySettings(props: SettingsProps) {
  return <ReplySettings key={props.tenantId} {...props} />;
}

/** ReplySettings 将管理动作限定在本人规则，公共项只展示完整内容与来源。 */
function ReplySettings({ tenantId, items, onChanged, onClose }: SettingsProps) {
  const { message } = App.useApp();
  const [revision, setRevision] = useState(0);
  const welcomes = usePersonalRules<PersonalWelcome>(tenantId, "/me/welcome-rules", revision);
  const keywords = usePersonalRules<PersonalKeyword>(tenantId, "/me/keyword-rules", revision);
  const [reply, setReply] = useState<QuickReply | null | undefined>();
  const [editing, setEditing] = useState<EditingRule | null>(null);
  const [busyId, setBusyId] = useState("");
  const mounted = useRef(true),
    locked = useRef(false),
    mutation = useRef<AbortController | null>(null);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      mutation.current?.abort();
    };
  }, []);
  const visibleReplies = items.filter((item) => item.enabled !== false);
  /** reload 重新获取服务端版本，不在客户端推算配置保存结果。 */
  function reload() {
    setRevision((value) => value + 1);
  }
  /** changeRule 只处理服务端明确允许编辑的本人条目，并携带版本防止覆盖。 */
  async function changeRule(
    kind: "welcome" | "keyword",
    item: PersonalWelcome | PersonalKeyword,
    remove: boolean,
    enabled?: boolean,
  ) {
    if (!item.editable || locked.current) return;
    locked.current = true;
    setBusyId(item.id);
    const controller = new AbortController();
    mutation.current = controller;
    try {
      await api(`/me/${kind === "welcome" ? "welcome-rules" : "keyword-rules"}/${item.id}`, {
        tenantId,
        method: remove ? "DELETE" : "PATCH",
        signal: controller.signal,
        bodyJSON: {
          version: item.version,
          ...(!remove && kind === "welcome"
            ? {
                name: item.name,
                scenario: (item as PersonalWelcome).scenario,
                contents: (item as PersonalWelcome).contents,
              }
            : {}),
          ...(remove ? {} : { enabled }),
        },
      });
      if (mounted.current) {
        message.success(remove ? "个人规则已删除" : "个人规则已更新");
        reload();
      }
    } catch (problem) {
      if (!controller.signal.aborted && mounted.current) message.error(errorMessage(problem));
    } finally {
      locked.current = false;
      if (mounted.current) setBusyId("");
    }
  }
  /** sourceTag 显示来源与只读状态，避免客服将团队内容误认为本人设置。 */
  function sourceTag(item: PersonalRule) {
    return (
      <Tag
        color={item.editable ? "blue" : undefined}
        icon={item.editable ? undefined : <LockOutlined />}
      >
        {replySourceNames[item.source_scope] || "公共"}
        {item.editable ? "" : " · 只读"}
      </Tag>
    );
  }
  /** ruleActions 仅本人规则显示编辑、删除和启停控件。 */
  function ruleActions(
    kind: "welcome" | "keyword",
    item: PersonalWelcome | PersonalKeyword,
    loading: boolean,
  ) {
    if (!item.editable) return <Tag>已启用</Tag>;
    return (
      <Space wrap size={4}>
        <Switch
          size="small"
          checked={item.enabled}
          loading={busyId === item.id}
          disabled={!!busyId || loading}
          checkedChildren="开"
          unCheckedChildren="关"
          aria-label={`${item.enabled ? "停用" : "启用"}${item.name}`}
          onChange={(enabled) => void changeRule(kind, item, false, enabled)}
        />
        <Button
          size="small"
          type="link"
          disabled={!!busyId || loading}
          onClick={() =>
            setEditing(
              kind === "welcome"
                ? { kind, item: item as PersonalWelcome }
                : { kind, item: item as PersonalKeyword },
            )
          }
        >
          编辑
        </Button>
        <Popconfirm
          title="删除这条个人规则？"
          description="已发送的消息会保留。"
          onConfirm={() => changeRule(kind, item, true)}
          disabled={!!busyId || loading}
        >
          <Button size="small" type="link" danger disabled={!!busyId || loading}>
            删除
          </Button>
        </Popconfirm>
      </Space>
    );
  }
  return (
    <Drawer
      rootClassName="personal-reply-settings"
      title="我的回复设置"
      open
      width="min(760px, 100vw)"
      onClose={busyId || editing || reply !== undefined ? undefined : onClose}
      destroyOnHidden
    >
      <p className="personal-reply-intro">
        管理自己的常用回复和自动回复。团队已启用的内容在这里显示为只读。
      </p>
      <Tabs
        items={[
          {
            key: "replies",
            label: "快捷回复",
            children: (
              <section aria-label="快捷回复管理">
                <div className="personal-reply-heading">
                  <p>双击快捷栏中的标题，向当前客户发送。</p>
                  <Button type="primary" icon={<PlusOutlined />} onClick={() => setReply(null)}>
                    新增快捷回复
                  </Button>
                </div>
                {!visibleReplies.length && (
                  <Empty
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                    description="还没有快捷回复，先添加一条常用话术。"
                  />
                )}
                <div className="personal-rule-list">
                  {visibleReplies.map((item) => (
                    <article className="personal-rule-card" key={item.id}>
                      <div className="personal-rule-heading">
                        <div>
                          <Tag
                            color={item.scope === "personal" ? "blue" : undefined}
                            icon={item.scope === "personal" ? undefined : <LockOutlined />}
                          >
                            {item.scope === "personal" ? "我的" : "团队 · 只读"}
                          </Tag>
                          <strong>{item.title}</strong>
                        </div>
                        {item.scope === "personal" && (
                          <Button type="link" size="small" onClick={() => setReply(item)}>
                            编辑
                          </Button>
                        )}
                      </div>
                      {item.content && <p className="personal-rule-content">{item.content}</p>}
                      {item.attachment && (
                        <div className="personal-reply-media">
                          <AttachmentView
                            attachment={item.attachment}
                            identity={{ tenantId, kind: "agent", actorId: "" }}
                            messageType={item.message_type}
                          />
                        </div>
                      )}
                      {!!item.options?.length && (
                        <div className="personal-rule-options">
                          {item.options.map((option) => (
                            <p key={option.id}>
                              <Tag>{option.label}</Tag>
                              <span>{option.value}</span>
                            </p>
                          ))}
                        </div>
                      )}
                    </article>
                  ))}
                </div>
              </section>
            ),
          },
          {
            key: "welcome",
            label: "欢迎语",
            children: (
              <section aria-label="欢迎语管理">
                <div className="personal-reply-heading">
                  <p>首次接单时追加个人欢迎，每个会话只触发一次。</p>
                  <Button
                    type="primary"
                    icon={<PlusOutlined />}
                    onClick={() => setEditing({ kind: "welcome", item: null })}
                  >
                    新增个人欢迎语
                  </Button>
                </div>
                <ErrorNotice error={welcomes.error} retry={reload} />
                {welcomes.loading && (
                  <div className="personal-reply-loading">
                    <Spin />
                  </div>
                )}
                {!welcomes.loading && !welcomes.error && !welcomes.items.length && (
                  <Empty
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                    description="还没有个人欢迎语或已启用的团队欢迎内容。"
                  />
                )}
                <div className="personal-rule-list">
                  {welcomes.items.map((item) => (
                    <article className="personal-rule-card" key={item.id}>
                      <div className="personal-rule-heading">
                        <div>
                          {sourceTag(item)}
                          <strong>{item.name}</strong>
                        </div>
                        {ruleActions("welcome", item, welcomes.loading)}
                      </div>
                      <p className="personal-rule-meta">
                        {welcomeScenarioNames[item.scenario] || "全部时段"}
                      </p>
                      {(item.contents || []).map((content, index) => (
                        <div className="personal-welcome-preview" key={index}>
                          <span className="personal-rule-meta">
                            {item.editable ? "接单后" : "创建咨询后"} {content.delay_seconds} 秒 ·{" "}
                            {content.type === "image"
                              ? "图片"
                              : content.type === "options"
                                ? "选项"
                                : "文字"}
                          </span>
                          {content.content && (
                            <p className="personal-rule-content">{content.content}</p>
                          )}
                          {content.type === "image" && content.attachment_id && (
                            <div className="personal-reply-media">
                              <AttachmentView
                                attachment={{
                                  id: content.attachment_id,
                                  file_name: "欢迎图片",
                                  content_type: "image/png",
                                }}
                                identity={{ tenantId, kind: "agent", actorId: "" }}
                                messageType="image"
                              />
                            </div>
                          )}
                          {!!content.options?.length && (
                            <div className="personal-rule-options">
                              {content.options.map((option) => (
                                <p key={option.id}>
                                  <Tag>{option.label}</Tag>
                                  <span>{option.value}</span>
                                </p>
                              ))}
                            </div>
                          )}
                        </div>
                      ))}
                    </article>
                  ))}
                </div>
              </section>
            ),
          },
          {
            key: "keywords",
            label: "关键词回复",
            children: (
              <section aria-label="关键词回复管理">
                <div className="personal-reply-heading">
                  <p>个人规则仅在你当前接待的会话中匹配。</p>
                  <Button
                    type="primary"
                    icon={<PlusOutlined />}
                    onClick={() => setEditing({ kind: "keyword", item: null })}
                  >
                    新增个人关键词
                  </Button>
                </div>
                <ErrorNotice error={keywords.error} retry={reload} />
                {keywords.loading && (
                  <div className="personal-reply-loading">
                    <Spin />
                  </div>
                )}
                {!keywords.loading && !keywords.error && !keywords.items.length && (
                  <Empty
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                    description="还没有个人关键词或已启用的团队关键词。"
                  />
                )}
                <div className="personal-rule-list">
                  {keywords.items.map((item) => (
                    <article className="personal-rule-card" key={item.id}>
                      <div className="personal-rule-heading">
                        <div>
                          {sourceTag(item)}
                          <strong>{item.name}</strong>
                        </div>
                        {ruleActions("keyword", item, keywords.loading)}
                      </div>
                      <p className="personal-rule-meta">
                        {item.match_mode === "exact" ? "完全匹配" : "包含匹配"} · 优先级{" "}
                        {item.priority} · 同会话冷却 {item.cooldown_seconds} 秒
                      </p>
                      <p className="personal-rule-pattern">关键词：{item.pattern}</p>
                      <p className="personal-rule-content">{item.reply}</p>
                    </article>
                  ))}
                </div>
              </section>
            ),
          },
        ]}
      />
      {reply !== undefined && (
        <QuickReplyEditor
          tenantId={tenantId}
          manages={false}
          items={visibleReplies.filter((item) => item.scope === "personal")}
          item={reply}
          onSelect={setReply}
          onClose={() => setReply(undefined)}
          onSaved={() => {
            setReply(undefined);
            onChanged();
          }}
        />
      )}
      {editing && (
        <PersonalAutomationEditor
          key={`${editing.kind}:${editing.item?.id || "new"}`}
          {...editing}
          tenantId={tenantId}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            reload();
          }}
        />
      )}
    </Drawer>
  );
}
