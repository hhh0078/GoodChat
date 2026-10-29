/** 文件作用说明：呈现管理员的持续 AI 对话、图片参考、生成进度与隔离用户端预览。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { Alert, Button, Input, Modal, Select, Space, Spin, Tag, Tooltip } from "antd";
import {
  CloseOutlined,
  PictureOutlined,
  PlusOutlined,
  RobotOutlined,
  SendOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import { errorMessage } from "@/lib/api";
import { activeAssistantJob, type AssistantRequest } from "./state";
import { useAssistant } from "./use-assistant";
import { AssistantPreview } from "./preview";
import { AssistantSettings } from "./settings";
import { AssistantJobCard } from "./job-card";

/** AssistantPanel 使用持久对话接口，不把模型返回文本当成可执行页面注入后台。 */
export default function AssistantPanel({
  open,
  onClose,
  initial,
  tenantId,
  platformAdmin,
  tenantAdmin,
}: {
  open: boolean;
  onClose: () => void;
  initial: AssistantRequest;
  tenantId: string;
  platformAdmin: boolean;
  tenantAdmin: boolean;
}) {
  const state = useAssistant(tenantId, open && tenantAdmin, initial);
  const [settingsOpen, setSettingsOpen] = useState(initial.view === "configuration");
  const [mobileTab, setMobileTab] = useState("conversation");
  const [dragging, setDragging] = useState(false);
  const [candidateId, setCandidateId] = useState("");
  const fileInput = useRef<HTMLInputElement | null>(null);
  const messagesEnd = useRef<HTMLDivElement | null>(null);
  const active = activeAssistantJob(state.job);
  useEffect(() => {
    if (initial.view === "configuration") setSettingsOpen(true);
  }, [initial.nonce, initial.view]);
  useEffect(() => {
    messagesEnd.current?.scrollIntoView({ behavior: "auto", block: "end" });
  }, [state.thread?.messages?.length, state.job?.phase]);
  useEffect(() => {
    setCandidateId("");
  }, [state.thread?.id, state.job?.id]);
  const previewURL = state.job?.preview_url || state.thread?.preview_url;
  const templateId = candidateId || state.job?.template_id || state.thread?.template_id;
  const busy = state.sending || state.loading || active;
  const examples = [
    "为金融咨询设计可信、克制的客服页，顶部展示风险提醒，提供分期选项。",
    "设计电商售后客服页，温暖橙色，突出订单咨询和售后进度入口。",
    "请根据参考图片调整布局与配色，保留完整的发送、接收和附件功能。",
  ];

  return (
    <>
      <Modal
        className="assistant-modal"
        title={
          <div className="assistant-title">
            <span className="assistant-title-icon">
              <RobotOutlined />
            </span>
            <div>
              <strong>AI 模板助手</strong>
              <span>描述需求，边聊边预览你的用户端</span>
            </div>
          </div>
        }
        open={open}
        onCancel={onClose}
        footer={null}
        width="min(1540px, calc(100vw - 48px))"
        styles={{ body: { padding: 0 } }}
        style={{ top: 24 }}
        maskClosable={false}
      >
        <div className="assistant-commandbar">
          <Space wrap>
            <Button
              icon={<PlusOutlined />}
              disabled={active || state.sending || state.uploading || state.loading}
              onClick={() => state.newThread()}
            >
              新对话
            </Button>
            <Select
              aria-label="对话历史"
              placeholder="恢复历史对话"
              value={state.thread?.id || undefined}
              className="assistant-history-select"
              loading={state.loading}
              disabled={active || state.sending || state.uploading || state.loading}
              onChange={(value) => void state.selectThread(value)}
              options={state.threads.map((thread) => ({
                value: thread.id,
                label: thread.title || "未命名对话",
              }))}
              notFoundContent="暂无历史对话"
            />
            {platformAdmin && (
              <Select
                aria-label="AI 配置来源"
                value={state.source}
                disabled={!!state.thread || busy || state.uploading}
                onChange={(value) => state.newThread(value)}
                options={[
                  { value: "tenant", label: "本租户 AI" },
                  { value: "platform", label: "平台 AI" },
                ]}
              />
            )}
          </Space>
          <Button icon={<SettingOutlined />} onClick={() => setSettingsOpen(true)}>
            AI 配置
          </Button>
        </div>
        <div className="assistant-mobile-switch" role="tablist" aria-label="助手面板">
          <button
            role="tab"
            aria-selected={mobileTab === "conversation"}
            onClick={() => setMobileTab("conversation")}
          >
            对话
          </button>
          <button
            role="tab"
            aria-selected={mobileTab === "preview"}
            onClick={() => setMobileTab("preview")}
          >
            用户端预览
          </button>
        </div>
        {!tenantAdmin && (
          <Alert
            type="info"
            showIcon
            title="请切换到你管理的租户后创建模板"
            description="平台 AI 与官方 JS 配置仍可在右上角管理。"
          />
        )}
        <div className={`assistant-workspace show-${mobileTab}`}>
          <section
            className={`assistant-conversation${dragging ? " is-dragging" : ""}`}
            aria-label="AI 模板对话"
            onDragOver={(event) => {
              if (event.dataTransfer.types.includes("Files")) {
                event.preventDefault();
                setDragging(true);
              }
            }}
            onDragLeave={(event) => {
              if (!event.currentTarget.contains(event.relatedTarget as Node)) setDragging(false);
            }}
            onDrop={(event) => {
              event.preventDefault();
              setDragging(false);
              if (tenantAdmin) void state.uploadFiles(Array.from(event.dataTransfer.files));
            }}
          >
            <div className="assistant-dialogue" aria-busy={state.loading}>
              {state.loading ? (
                <div className="assistant-loading">
                  <Spin description="正在恢复对话" />
                </div>
              ) : !state.thread?.messages?.length ? (
                <div className="assistant-welcome">
                  <div className="assistant-welcome-icon">
                    <RobotOutlined />
                  </div>
                  <h2>
                    {initial.templateName
                      ? `一起完善「${initial.templateName}」`
                      : "你的客服页面，可以更有品牌感"}
                  </h2>
                  <p>说说行业、配色与页面内容，也可以上传参考图片。聊天核心功能由平台提供。</p>
                  <div className="assistant-examples">
                    {examples.map((example) => (
                      <button
                        key={example}
                        disabled={!tenantAdmin}
                        onClick={() => state.setDraft(example)}
                      >
                        {example}
                        <span aria-hidden="true">↗</span>
                      </button>
                    ))}
                  </div>
                </div>
              ) : (
                <div className="assistant-message-list" role="log" aria-label="AI 对话记录">
                  {state.thread.messages.map((item) => (
                    <article
                      key={item.id}
                      className={`assistant-message ${item.role === "user" ? "is-user" : "is-assistant"}`}
                    >
                      <strong>
                        {item.role === "user"
                          ? "你"
                          : item.role === "system"
                            ? "生成记录"
                            : "AI 模板助手"}
                      </strong>
                      {item.content && (
                        <div className="assistant-message-content">{item.content}</div>
                      )}
                      {!!item.assets?.length && (
                        <div className="assistant-reference-strip">
                          {item.assets.map((asset) => (
                            <img key={asset.id} src={asset.data_url} alt="此条消息的参考图片" />
                          ))}
                        </div>
                      )}
                      {!item.assets?.length && !!item.asset_ids?.length && (
                        <Tag>{item.asset_ids.length} 张参考图片</Tag>
                      )}
                      {item.created_at && (
                        <time dateTime={item.created_at}>
                          {new Date(item.created_at).toLocaleString("zh-CN", {
                            month: "numeric",
                            day: "numeric",
                            hour: "2-digit",
                            minute: "2-digit",
                          })}
                        </time>
                      )}
                    </article>
                  ))}
                </div>
              )}
              {state.job && (
                <AssistantJobCard
                  job={state.job}
                  tenantId={tenantId}
                  templateId={templateId}
                  title={state.thread?.title}
                  changing={state.changing}
                  candidateId={candidateId}
                  onCandidateSaved={setCandidateId}
                  onRetry={state.retry}
                  onCancel={state.cancel}
                />
              )}
              {!!state.error && (
                <Alert
                  type="error"
                  showIcon
                  title={errorMessage(state.error)}
                  action={
                    <Button size="small" onClick={() => void state.refresh()}>
                      刷新状态
                    </Button>
                  }
                />
              )}
              <div ref={messagesEnd} />
            </div>
            <div className="assistant-composer">
              {!!state.assets.length && (
                <div className="assistant-reference-strip">
                  {state.assets.map((asset, index) => (
                    <div key={asset.id} className="assistant-reference">
                      <img src={asset.data_url} alt={`待发送参考图片 ${index + 1}`} />
                      <Tooltip title="移除参考图片">
                        <button
                          aria-label={`移除参考图片 ${index + 1}`}
                          disabled={state.sending}
                          onClick={() =>
                            state.setAssets((items) => items.filter((item) => item.id !== asset.id))
                          }
                        >
                          <CloseOutlined />
                        </button>
                      </Tooltip>
                    </div>
                  ))}
                </div>
              )}
              <Input.TextArea
                aria-label="描述用户端模板需求"
                value={state.draft}
                onChange={(event) => state.setDraft(event.target.value)}
                placeholder="描述你想要的用户端，也可以粘贴或拖入参考图片…"
                autoSize={{ minRows: 3, maxRows: 7 }}
                maxLength={12000}
                disabled={!tenantAdmin || state.loading || state.sending || active}
                onPaste={(event) => {
                  const files = Array.from(event.clipboardData.files);
                  if (files.length) {
                    event.preventDefault();
                    void state.uploadFiles(files);
                  }
                }}
                onKeyDown={(event) => {
                  if (
                    (event.metaKey || event.ctrlKey) &&
                    event.key === "Enter" &&
                    !event.nativeEvent.isComposing
                  ) {
                    event.preventDefault();
                    void state.send();
                  }
                }}
              />
              <div className="assistant-composer-actions">
                <Space>
                  <Button
                    type="text"
                    icon={<PictureOutlined />}
                    aria-label="上传参考图片"
                    loading={state.uploading}
                    disabled={!tenantAdmin || busy || state.assets.length >= 4}
                    onClick={() => fileInput.current?.click()}
                  >
                    参考图片
                  </Button>
                  <span>{state.assets.length}/4</span>
                </Space>
                <Button
                  type="primary"
                  icon={<SendOutlined />}
                  disabled={
                    !tenantAdmin ||
                    !state.draft.trim() ||
                    state.uploading ||
                    active ||
                    state.loading
                  }
                  loading={state.sending}
                  onClick={() => void state.send()}
                >
                  发送
                </Button>
              </div>
              <input
                ref={fileInput}
                type="file"
                accept="image/png,image/jpeg,image/webp,image/gif"
                multiple
                hidden
                aria-label="选择参考图片"
                onChange={(event) => {
                  void state.uploadFiles(Array.from(event.target.files || []));
                  event.target.value = "";
                }}
              />
              <p className="assistant-composer-hint">
                支持粘贴、拖入图片 · ⌘ / Ctrl + Enter 发送 · 关闭窗口后可从历史恢复
              </p>
            </div>
            {dragging && <div className="assistant-drop-indicator">松开添加参考图片</div>}
          </section>
          <AssistantPreview
            url={previewURL}
            templateId={templateId}
            pageId={
              state.job?.applied === false ? undefined : state.job?.page_id || state.thread?.page_id
            }
          />
        </div>
      </Modal>
      <AssistantSettings
        open={settingsOpen && open}
        onClose={() => setSettingsOpen(false)}
        tenantId={tenantId}
        platformAdmin={platformAdmin}
        tenantAdmin={tenantAdmin}
      />
    </>
  );
}
