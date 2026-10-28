/** 文件作用说明：读取多链接入口，建立隔离访客身份并连接真实会话，支持安全恢复与重新咨询。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { Alert, Avatar, Button, Form, Input, Result, Spin } from "antd";
import { useSearchParams } from "next/navigation";
import { canRestartVisit } from "@/lib/visitor-params";
import { parseVisitorParameters } from "@/lib/visitor-params";
import {
  forgetVisitorSessionCache,
  mergeVisitorParameters,
  readVisitorSessionCache,
  safeVisitorParameters,
  writeVisitorSessionCache,
  withVisitorIdentityLock,
  type VisitorSessionCache,
} from "@/lib/visitor-session-cache";
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
  notification_sound_id?: string;
  history_agent_minutes?: number;
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
  const queryText = search.toString();
  const [entry, setEntry] = useState<Entry | null>(null);
  const [session, setSession] = useState<VisitorSession | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [starting, setStarting] = useState(false);
  const [status, setStatus] = useState("");
  const [storageWarning, setStorageWarning] = useState("");
  const navigationScope = `${entryKey}?${queryText}`;
  const activeEntry = useRef(navigationScope);
  activeEntry.current = navigationScope;
  const lastNavigation = useRef(navigationScope);
  const navigationGeneration = useRef(0);
  if (lastNavigation.current !== navigationScope) {
    lastNavigation.current = navigationScope;
    navigationGeneration.current++;
  }
  const remembered = useRef<VisitorSessionCache | null>(null);
  const memoryToken = useRef<{ space: string; value: string } | null>(null);
  const startingGeneration = useRef<number | null>(null);
  const visitID = useRef("");
  const initialized = useRef(false);
  const [form] = Form.useForm();
  useEffect(() => {
    const controller = new AbortController();
    initialized.current = false;
    setLoading(true);
    setError(null);
    setSession(null);
    visitID.current = "";
    remembered.current = null;
    const currentScope = navigationScope;
    const currentGeneration = navigationGeneration.current;
    api<Entry>(`/entries/${encodeURIComponent(entryKey)}`, { signal: controller.signal })
      .then(async (data) => {
        if (controller.signal.aborted) return;
        setEntry(data);
        let token: string | null = null;
        try {
          const legacy = localStorage.getItem(`goodchat.visitor.${data.service_space_id}`);
          const cached = readVisitorSessionCache(localStorage, data.service_space_id, entryKey);
          remembered.current = cached;
          token = cached?.visitor_token || legacy || null;
          const profile = remembered.current?.contact;
          if (profile)
            form.setFieldsValue({
              display_name: profile.display_name || "",
              phone: profile.phone || "",
            });
        } catch {
          setStorageWarning(
            "浏览器未允许保存访客会话；当前页面可继续咨询，刷新后可能无法恢复，请允许站点存储。",
          );
          token =
            memoryToken.current?.space === data.service_space_id ? memoryToken.current.value : null;
          if (!token) {
            initialized.current = true;
            return;
          }
        }
        if (!initialized.current) {
          initialized.current = true;
          await start({}, data, token || undefined, currentScope, currentGeneration);
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
  }, [entryKey, queryText]);
  /** start 用独立凭证恢复资料，单调代次及待确认编号阻止旧响应覆盖新导航。 */
  async function start(
    values: Record<string, string>,
    selected: Entry | null = entry,
    restoredToken?: string,
    operationScope = navigationScope,
    operationGeneration = navigationGeneration.current,
  ) {
    if (!selected || startingGeneration.current === operationGeneration) return;
    startingGeneration.current = operationGeneration;
    /** isCurrent 同时校验导航代次和范围，拒绝A→B→A的陈旧请求回调。 */
    const isCurrent = () =>
      navigationGeneration.current === operationGeneration &&
      activeEntry.current === operationScope;
    setStarting(true);
    setError(null);
    try {
      const cached = remembered.current;
      const parsed = mergeVisitorParameters(
        cached,
        parseVisitorParameters(new URLSearchParams(queryText)),
      );
      let token =
        restoredToken ||
        cached?.visitor_token ||
        (memoryToken.current?.space === selected.service_space_id
          ? memoryToken.current.value
          : undefined);
      const profile = {
        ...parsed.profile,
        ...(values.display_name ? { display_name: values.display_name } : {}),
        ...(values.phone ? { phone: values.phone } : {}),
      };
      const parameters = safeVisitorParameters({ ...parsed, profile });
      if (!visitID.current)
        visitID.current =
          cached?.pending_visit_id &&
          JSON.stringify(cached.parameters) === JSON.stringify(parameters)
            ? cached.pending_visit_id
            : crypto.randomUUID();
      const requestVisitId = visitID.current;
      const pending: VisitorSessionCache = {
        version: 1,
        service_space_id: selected.service_space_id,
        entry_key: selected.entry_key,
        visitor_token: token,
        conversation_id: cached?.conversation_id,
        contact: cached?.contact || {},
        parameters,
        pending_visit_id: requestVisitId,
        pending_started_at: cached?.pending_started_at || new Date().toISOString(),
        updated_at: new Date().toISOString(),
      };
      const context = {
        page_url:
          search.get("page_url") ||
          (document.referrer
            ? new URL(document.referrer).origin + new URL(document.referrer).pathname
            : location.origin + location.pathname),
        ...parsed.context,
      };
      /** requestSession 在首次空间锁内二次检查凭证，并在释放锁前保存新凭证，合并多个标签的匿名初始化。 */
      async function requestSession() {
        if (!token) {
          try {
            token =
              localStorage.getItem(`goodchat.visitor.${selected!.service_space_id}`) || undefined;
          } catch {
            /* 当前页面仍可显式咨询，存储限制已提示。 */
          }
        }
        if (!isCurrent()) throw new Error("访问范围已变化");
        pending.visitor_token = token;
        remembered.current = pending;
        try {
          writeVisitorSessionCache(localStorage, pending);
        } catch {
          setStorageWarning(
            "浏览器未能保存会话资料。当前页面仍可咨询，请允许站点存储以便下次恢复。",
          );
        }
        const response = await api<VisitorSession>("/visitor/session", {
          method: "POST",
          bodyJSON: {
            entry_key: selected!.entry_key,
            visit_id: requestVisitId,
            visitor_token: token,
            resume_conversation_id:
              cached?.visitor_token === token ? cached?.conversation_id : undefined,
            routing: parsed.routing,
            profile,
            context,
            attributes: parsed.attributes,
            tracking: parsed.tracking,
            handoff_code: parsed.handoff_code,
          },
        });
        try {
          if (!token || !localStorage.getItem(`goodchat.visitor.${selected!.service_space_id}`))
            localStorage.setItem(`goodchat.visitor.${selected!.service_space_id}`, response.token);
        } catch {
          /* 凭证仍留在返回值供当前页面使用。 */
        }
        return response;
      }
      const result = token
        ? await requestSession()
        : await withVisitorIdentityLock(
            selected.service_space_id,
            requestSession,
            typeof navigator !== "undefined" ? navigator.locks : undefined,
          );
      if (!isCurrent()) return;
      memoryToken.current = { space: selected.service_space_id, value: result.token };
      const snapshot: VisitorSessionCache = {
        ...pending,
        visitor_token: result.token,
        conversation_id: result.conversation.id,
        contact: {
          id: result.contact.id,
          display_name: String(result.contact.display_name || ""),
          phone: String(result.contact.phone || ""),
          avatar_url: String(result.contact.avatar_url || ""),
        },
        pending_visit_id: undefined,
        pending_started_at: undefined,
        updated_at: new Date().toISOString(),
      };
      try {
        const latest = readVisitorSessionCache(
          localStorage,
          selected.service_space_id,
          selected.entry_key,
        );
        if (!latest || latest.pending_visit_id === requestVisitId)
          writeVisitorSessionCache(localStorage, snapshot);
        localStorage.setItem(`goodchat.visitor.${selected.service_space_id}`, result.token);
        setStorageWarning("");
      } catch {
        setStorageWarning("会话已建立，但浏览器未能保存恢复资料；关闭或刷新前请允许站点存储。");
      }
      remembered.current = snapshot;
      setEntry((previous) =>
        previous?.entry_key === result.entry.entry_key
          ? { ...previous, ...result.entry }
          : result.entry,
      );
      setSession(result);
      setStatus(result.conversation.status);
    } catch (problem) {
      if (!isCurrent()) return;
      if (problem instanceof ApiError && problem.code === "specified_agent_offline")
        setSession(null);
      if (problem instanceof ApiError && problem.status === 401) {
        try {
          localStorage.removeItem(`goodchat.visitor.${selected.service_space_id}`);
          forgetVisitorSessionCache(localStorage, selected.service_space_id, selected.entry_key);
        } catch {
          /* 不再从当前内存复用失效凭证。 */
        }
        remembered.current = null;
        memoryToken.current = null;
        visitID.current = "";
      }
      setError(problem);
    } finally {
      if (startingGeneration.current === operationGeneration) startingGeneration.current = null;
      if (isCurrent()) setStarting(false);
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
  const specifiedOffline = error instanceof ApiError && error.code === "specified_agent_offline";
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
                  : specifiedOffline
                    ? "无客服在线，暂时无法访问"
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
          {storageWarning && (
            <Alert type="warning" showIcon title={storageWarning} style={{ marginBlock: 10 }} />
          )}
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
                notificationSound={entry.notification_sound_id}
              />
            ) : (
              <ChatPanel
                notificationSound={entry.notification_sound_id}
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
        ) : specifiedOffline ? (
          <Result
            status="warning"
            title="无客服在线，暂时无法访问"
            subTitle="指定客服当前不在线，请稍后重新检查。"
            extra={
              <Button loading={starting} onClick={() => void start({})}>
                重新检查
              </Button>
            }
          />
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
              title={
                storageWarning
                  ? "浏览器尚未保存恢复资料，当前页面仍可继续咨询。"
                  : "访客身份保存在当前浏览器。请勿把当前浏览器交给他人使用。"
              }
              style={{ marginTop: 24 }}
            />
          </div>
        )}
        <footer className="visitor-footer">由 GoodChat 提供在线客服服务</footer>
      </div>
    </div>
  );
}
