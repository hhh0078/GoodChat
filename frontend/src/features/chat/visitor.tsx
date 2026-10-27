/** 文件作用说明：读取多链接入口，建立隔离访客身份并连接真实会话，支持安全恢复与重新咨询。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { Alert, Avatar, Button, Form, Input, Result, Spin } from "antd";
import { useSearchParams } from "next/navigation";
import { canRestartVisit } from "@/lib/visitor-params";
import { parseVisitorParameters } from "@/lib/visitor-params";
import { api, ApiError } from "@/lib/api";
import type { Conversation, Resource } from "@/lib/types";
import { ErrorNotice } from "@/components/page-state";
import { DecoratedVisitor } from "./decorated-visitor";
import { VisitorActions } from "./visitor-actions";
import { ChatPanel, conversationStatus } from "./chat-panel";
type Entry = Resource & {
  entry_key: string;
  tenant_name: string;
  service_space_id: string;
  page_id?: string;
  pinned_release_id?: string;
  render_url?: string;
  release_id?: string;
  render_mode?: string;
};
type VisitorSession = {
  token: string;
  contact: Resource;
  conversation: Conversation;
  entry: Entry;
};
/** VisitorChat 只使用独立随机凭证恢复访客，展示资料不会成为身份凭证。 */
export function VisitorChat({ entryKey }: { entryKey: string }) {
  const search = useSearchParams();
  const [entry, setEntry] = useState<Entry | null>(null);
  const [session, setSession] = useState<VisitorSession | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [starting, setStarting] = useState(false);
  const [status, setStatus] = useState("");
  const activeEntry = useRef(entryKey);
  activeEntry.current = entryKey;
  const visitID = useRef("");
  const initialized = useRef(false);
  const [form] = Form.useForm();
  useEffect(() => {
    const controller = new AbortController();
    initialized.current = false;
    setLoading(true);
    setError(null);
    setSession(null);
    api<Entry>(`/entries/${encodeURIComponent(entryKey)}`, { signal: controller.signal })
      .then(async (data) => {
        if (controller.signal.aborted) return;
        setEntry(data);
        const token = localStorage.getItem(`goodchat.visitor.${data.service_space_id}`);
        if (!initialized.current) {
          initialized.current = true;
          await start({}, data, token || undefined);
        }
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
    // 查询参数只是展示资料；身份恢复只取本机独立凭证。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [entryKey]);
  /** start 建立一次访问，用稳定 visit_id 使重试不会重复触发进入回调。 */
  async function start(
    values: Record<string, string>,
    selected: Entry | null = entry,
    restoredToken?: string,
  ) {
    if (!selected) return;
    setStarting(true);
    setError(null);
    if (!visitID.current) visitID.current = crypto.randomUUID();
    try {
      const parsed = parseVisitorParameters(new URLSearchParams(search.toString()));
      const context = {
        page_url:
          search.get("page_url") ||
          (document.referrer
            ? new URL(document.referrer).origin + new URL(document.referrer).pathname
            : location.origin + location.pathname),
        ...parsed.context,
      };
      const result = await api<VisitorSession>("/visitor/session", {
        method: "POST",
        bodyJSON: {
          entry_key: entryKey,
          visit_id: visitID.current,
          visitor_token:
            restoredToken ||
            localStorage.getItem(`goodchat.visitor.${selected.service_space_id}`) ||
            undefined,
          routing: parsed.routing,
          profile: {
            ...parsed.profile,
            ...(values.display_name ? { display_name: values.display_name } : {}),
            ...(values.phone ? { phone: values.phone } : {}),
          },
          context,
          attributes: parsed.attributes,
          tracking: parsed.tracking,
          handoff_code: parsed.handoff_code,
        },
      });
      if (activeEntry.current !== selected.entry_key) return;
      localStorage.setItem(`goodchat.visitor.${selected.service_space_id}`, result.token);
      setSession(result);
      setStatus(result.conversation.status);
    } catch (problem) {
      if (activeEntry.current !== selected.entry_key) return;
      if (problem instanceof ApiError && problem.status === 401)
        localStorage.removeItem(`goodchat.visitor.${selected.service_space_id}`);
      setError(problem);
    } finally {
      if (activeEntry.current === selected.entry_key) setStarting(false);
    }
  }
  /** restartVisit 明确开始新访问，不凭旧visit_id恢复可能已经创建的访客身份。 */
  function restartVisit() {
    visitID.current = crypto.randomUUID();
    void start(form.getFieldsValue());
  }
  if (loading)
    return (
      <div className="console-loading">
        <Spin description="正在连接咨询入口" />
      </div>
    );
  if (!entry)
    return (
      <div className="visitor-shell">
        <Result
          status="warning"
          title="咨询入口暂时无法打开"
          subTitle={<ErrorNotice error={error} />}
          extra={<Button onClick={() => location.reload()}>重新加载</Button>}
        />
      </div>
    );
  return (
    <div className="visitor-shell">
      <div
        className={`visitor-card ${entry.render_url && entry.release_id ? "visitor-decorated" : "visitor-default"}`}
      >
        <header className="visitor-header">
          <div className="visitor-brand">
            <Avatar size={42}>{(entry.tenant_name || entry.name || "在线客服").slice(0, 1)}</Avatar>
            <div>
              <h1>{entry.tenant_name || entry.name || "在线客服"}</h1>
              <p>
                {session
                  ? `在线咨询 · ${conversationStatus[status] || "正在连接"}`
                  : "欢迎咨询，请留下称呼后开始沟通"}
              </p>
            </div>
          </div>
          {session && (
            <VisitorActions
              entryKey={entryKey}
              conversationId={session.conversation.id}
              token={session.token}
              status={status}
            />
          )}
        </header>
        <div style={{ padding: "0 20px" }}>
          <ErrorNotice error={error} />
          {canRestartVisit(error) && (
            <Alert
              type="warning"
              showIcon
              title="上一次访问身份未确认"
              description="不能通过旧访问编号取回身份。你可以重新开始一次新访问，原记录会保留。"
              action={
                <Button loading={starting} onClick={restartVisit}>
                  重新开始新访问
                </Button>
              }
              style={{ marginBottom: 16 }}
            />
          )}
        </div>
        {session ? (
          <>
            {entry.render_url && entry.release_id ? (
              <DecoratedVisitor
                id={session.conversation.id}
                key={`${session.conversation.id}.${entry.release_id}`}
                identity={{ token: session.token, actorId: session.contact.id, kind: "visitor" }}
                decoration={{ render_url: entry.render_url, release_id: entry.release_id }}
                onStatus={setStatus}
              />
            ) : (
              <ChatPanel
                id={session.conversation.id}
                key={session.conversation.id}
                identity={{ token: session.token, actorId: session.contact.id, kind: "visitor" }}
                onStatus={setStatus}
              />
            )}
            {status === "closed" && (
              <div style={{ padding: "10px 20px", textAlign: "center" }}>
                <Button
                  type="primary"
                  loading={starting}
                  onClick={() => {
                    visitID.current = crypto.randomUUID();
                    void start({});
                  }}
                >
                  发起新的咨询
                </Button>
              </div>
            )}
          </>
        ) : (
          <div className="visitor-start">
            <h2>我们可以为你做些什么？</h2>
            <p className="muted">请填写方便交流的称呼，联系电话选填。</p>
            <Form
              form={form}
              layout="vertical"
              onFinish={(values) => {
                if (!canRestartVisit(error)) void start(values);
              }}
            >
              <Form.Item name="display_name" label="你的称呼（选填）">
                <Input maxLength={80} autoComplete="nickname" />
              </Form.Item>
              <Form.Item name="phone" label="联系电话（选填）">
                <Input maxLength={32} autoComplete="tel" />
              </Form.Item>
              <Button
                type="primary"
                htmlType="submit"
                size="large"
                block
                loading={starting}
                disabled={canRestartVisit(error)}
              >
                开始咨询
              </Button>
            </Form>
            <Alert
              type="info"
              showIcon
              title="访客身份保存在当前浏览器。请勿把当前浏览器交给他人使用。"
              style={{ marginTop: 24 }}
            />
          </div>
        )}
        <footer className="visitor-footer">由 GoodChat 提供在线客服服务</footer>
      </div>
    </div>
  );
}
