/** 文件作用说明：在真实客服会话中接管或恢复AI，并确认发送持久辅助草稿。 */
"use client";
import { showRequestError } from "@/lib/request-error";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import { App, Button, Collapse, Space, Tag } from "antd";
import { useEffect, useState } from "react";
import type { ChatIdentity } from "@/features/chat/use-chat";
import { AIPlanPreview } from "./preview";
import { aiErrorMessage } from "./errors";
import type { AIJob } from "./types";
import "./customer-ai.css";
/** AIConversationPanel 定期读取服务端控制状态，任何按钮都需后端重新授权。 */
export function AIConversationPanel({
  id,
  identity,
  onSent,
  onEdit,
}: {
  id: string;
  identity: ChatIdentity;
  onSent: () => void;
  onEdit: (text: string) => void;
}) {
  const tr = useTranslator();
  const { message } = App.useApp();
  const [data, setData] = useState<{
    control: { ai_paused: boolean; error_code?: string };
    drafts: AIJob[];
  } | null>(null);
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    setData(null);
    const controller = new AbortController();
    /** load 轮询草稿和人工控制状态，离开当前会话立即取消。 */
    async function load() {
      try {
        const value = await api<NonNullable<typeof data>>(`/conversations/${id}/ai`, {
          tenantId: identity.tenantId,
          signal: controller.signal,
        });
        if (!controller.signal.aborted) setData(value);
      } catch {
        /* 常规会话读取失败由聊天主面板展示，避免轮询重复弹窗。 */
      }
    }
    void load();
    const timer = setInterval(load, 2500);
    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [id, identity.tenantId, revision]);
  /** action 后端完成状态切换或草稿发送后再更新界面。 */
  async function action(kind: "pause" | "resume" | "send", job?: string) {
    setBusy(true);
    try {
      await api(`/conversations/${id}/ai${kind === "send" ? `/drafts/${job}/send` : ""}`, {
        tenantId: identity.tenantId,
        method: "POST",
        bodyJSON: kind === "send" ? {} : { action: kind },
      });
      setRevision((v) => v + 1);
      onSent();
    } catch (e) {
      showRequestError(e);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="ai-chat-controls">
      <Space wrap>
        <Tag>{data?.control.ai_paused ? tr("AI已暂停") : tr("AI按机器人设置运行")}</Tag>
        {data?.control.ai_paused && data.control.error_code && (
          <Tag color="warning">{aiErrorMessage(data.control.error_code, tr)}</Tag>
        )}
        <Button
          size="small"
          disabled={busy || !data}
          onClick={() => void action(data?.control.ai_paused ? "resume" : "pause")}
        >
          {data?.control.ai_paused ? tr("恢复AI接待") : tr("人工接管")}
        </Button>
        <span className="ai-help">{tr("恢复后从下一条访客消息开始处理。")}</span>
      </Space>
      {!!data?.drafts?.length && (
        <Collapse
          items={data.drafts.map((job) => ({
            key: job.id,
            label: tr("AI辅助草稿"),
            children: (
              <>
                <AIPlanPreview plan={job.result} />
                {job.result.items?.some((item) => item.type === "text") && (
                  <Button
                    disabled={busy}
                    onClick={() =>
                      onEdit(
                        (job.result.items || [])
                          .filter((item) => item.type === "text")
                          .map((item) => item.text || "")
                          .join("\n\n"),
                      )
                    }
                  >
                    {tr("编辑文字")}
                  </Button>
                )}
                <Button loading={busy} type="primary" onClick={() => void action("send", job.id)}>
                  {tr("确认发送")}
                </Button>
              </>
            ),
          }))}
        />
      )}
    </div>
  );
}
