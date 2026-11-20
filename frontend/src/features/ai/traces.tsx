/** 文件作用说明：按真实任务展示模型消息、工具入参与出参、知识读取和发送结果，运行中自动更新。 */
"use client";
import { useEffect, useState } from "react";
import { Button, Drawer, Input, Select, Skeleton, Tag, Timeline } from "antd";
import { useSession } from "@/features/auth/session";
import { useTranslator } from "@/i18n/use-translator";
import { useFormats } from "@/i18n/use-formats";
import { api } from "@/lib/api";
import { usePagedResource } from "@/lib/use-paged-resource";
import { ErrorNotice, EmptyData } from "@/components/page-state";
import { LoadMore } from "@/components/load-more";
import type { AIJob } from "./types";
import "./operations.css";

type TraceJob = AIJob & { question: string; bot_name: string; completed_at?: string };
type TraceEvent = { id: number; kind: string; name: string; payload: unknown; created_at: string };
type TraceDetail = { job: TraceJob; events: TraceEvent[]; sent_messages: unknown[] };

/** AITraceDetail 仅在详情打开时串行轮询任务，终止后保留最后真实状态；切换租户自动取消请求。 */
export function AITraceDetail({ id, onClose }: { id: string; onClose: () => void }) {
  const tr = useTranslator();
  const { tenantId } = useSession();
  const { dateTimeFormatter } = useFormats();
  const [data, setData] = useState<TraceDetail | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    setData(null);
    setError(null);
    /** load 每次等待上个请求结束，防止慢网络下堆叠轮询。 */
    async function load() {
      try {
        const next = await api<TraceDetail>(`/tenant/ai/traces/${id}`, {
          tenantId,
          signal: controller.signal,
        });
        if (controller.signal.aborted) return;
        setData(next);
        setError(null);
        if (["queued", "running"].includes(next.job.status))
          timer = setTimeout(() => void load(), 2000);
      } catch (problem) {
        if (!controller.signal.aborted) setError(problem);
      }
    }
    void load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [id, tenantId, revision]);
  const labels: Record<string, string> = {
    model_request: tr("模型请求"),
    model_response: tr("模型返回"),
    model_error: tr("模型调用失败"),
    tool_start: tr("开始调用工具"),
    tool_result: tr("工具执行结果"),
    completed: tr("任务完成"),
    failed: tr("任务失败"),
  };
  const running = !!data && ["queued", "running"].includes(data.job.status);
  return (
    <Drawer
      open
      title={tr("AI调用链路")}
      onClose={onClose}
      size={880}
      className="ai-trace-drawer"
      extra={<Button onClick={() => setRevision((x) => x + 1)}>{tr("刷新")}</Button>}
    >
      <ErrorNotice error={error} retry={() => setRevision((x) => x + 1)} />
      {!data && !error ? (
        <Skeleton active />
      ) : (
        data && (
          <>
            <div className="ai-chain-summary">
              <Tag color={running ? "processing" : "default"}>
                {running ? tr("执行中") : tr("执行已结束")}
              </Tag>
              <strong>{data.job.bot_name || tr("知识库描述")}</strong>
              <p>{data.job.question || data.job.id}</p>
              <small>
                {tr("输入Token")} {data.job.input_tokens} · {tr("输出Token")}{" "}
                {data.job.output_tokens}
              </small>
              {data.job.error_code && <p role="alert">{data.job.error_code}</p>}
            </div>
            <p className="muted">{tr("仅展示实际消息和执行结果，密钥与内联图片已脱敏。")}</p>
            {!data.events.length ? (
              <EmptyData text={tr("这条历史记录未采集调用链路。")} />
            ) : (
              <Timeline
                items={data.events.map((event) => ({
                  key: event.id,
                  color:
                    event.kind.includes("error") || event.kind === "failed"
                      ? "red"
                      : event.kind === "tool_start"
                        ? "blue"
                        : "gray",
                  content: (
                    <div className="ai-chain-event">
                      <header>
                        <strong>{labels[event.kind] || event.kind}</strong>
                        <time>
                          {dateTimeFormatter({ timeStyle: "medium" }).format(
                            new Date(event.created_at),
                          )}
                        </time>
                      </header>
                      <span className="ai-chain-name">{event.name}</span>
                      <details open={event.kind === "tool_start" || event.kind === "failed"}>
                        <summary>{tr("查看入参与出参")}</summary>
                        <pre>{JSON.stringify(event.payload, null, 2)}</pre>
                      </details>
                    </div>
                  ),
                }))}
              />
            )}
            {running && <p aria-live="polite">{tr("正在执行，详情每2秒自动更新。")}</p>}
            {!!data.sent_messages.length && (
              <section className="ai-chain-event">
                <h3>{tr("实际发送的消息")}</h3>
                <pre>{JSON.stringify(data.sent_messages, null, 2)}</pre>
              </section>
            )}
          </>
        )
      )}
    </Drawer>
  );
}

/** AITraces 将每次回复收拢为紧凑行，管理员看团队、普通成员仅看自己的机器人任务。 */
export function AITraces() {
  const tr = useTranslator();
  const { dateTimeFormatter } = useFormats();
  const [q, setQ] = useState("");
  const [status, setStatus] = useState("");
  const [id, setID] = useState("");
  const resource = usePagedResource<TraceJob>(
    `/tenant/ai/traces?${new URLSearchParams({ q, status })}`,
  );
  const statuses: Record<string, string> = {
    queued: tr("等待处理"),
    running: tr("执行中"),
    succeeded: tr("已完成"),
    draft: tr("待人工确认"),
    failed: tr("失败"),
    cancelled: tr("已取消"),
    uncertain: tr("用量待核对"),
  };
  const kinds: Record<string, string> = {
    reply: tr("客服回复"),
    summary: tr("知识库描述"),
    test: tr("调试"),
    restore: tr("知识库回滚"),
  };
  return (
    <div className="ai-operations">
      <header className="ai-operations-heading">
        <div>
          <h2>{tr("AI调用记录")}</h2>
          <p>{tr("从访客提问到最终回复，查看每一步实际执行。")}</p>
        </div>
        <Button onClick={resource.reload}>{tr("刷新")}</Button>
      </header>
      <div className="ai-operations-filters">
        <Input.Search allowClear placeholder={tr("搜索问题或机器人")} onSearch={setQ} />
        <Select
          aria-label={tr("状态")}
          value={status}
          onChange={setStatus}
          options={[
            { value: "", label: tr("全部状态") },
            ...Object.entries(statuses).map(([value, label]) => ({ value, label })),
          ]}
        />
      </div>
      <ErrorNotice error={resource.error} retry={resource.reload} />
      {!resource.loading && !resource.error && !resource.items.length && <EmptyData />}
      <div className="ai-operation-list">
        {resource.items.map((job) => (
          <article className="ai-operation-row" key={job.id}>
            <div className="ai-operation-content">
              <div className="ai-operation-title">
                <Tag
                  color={
                    job.status === "running"
                      ? "processing"
                      : job.status === "failed"
                        ? "error"
                        : "default"
                  }
                >
                  {statuses[job.status] || job.status}
                </Tag>
                <strong>{job.question || kinds[job.kind] || job.kind}</strong>
              </div>
              <p>
                {job.bot_name || kinds[job.kind]} ·{" "}
                {dateTimeFormatter({ dateStyle: "short", timeStyle: "medium" }).format(
                  new Date(job.created_at),
                )}{" "}
                · {job.input_tokens + job.output_tokens} Token
              </p>
              {job.error_code && <small>{job.error_code}</small>}
            </div>
            <Button onClick={() => setID(job.id)}>{tr("查看链路")}</Button>
          </article>
        ))}
      </div>
      <LoadMore
        loading={resource.loading}
        hasMore={resource.hasMore}
        error={resource.error}
        onLoad={resource.loadMore}
      />
      {id && <AITraceDetail key={id} id={id} onClose={() => setID("")} />}
    </div>
  );
}
