/** 文件作用说明：连接持久 AI 对话、图片上传和阶段任务，恢复面板时重新读取服务端真实状态。 */
"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, errorMessage } from "@/lib/api";
import {
  activeAssistantJob,
  assistantAssetProblem,
  type AssistantAsset,
  type AssistantJob,
  type AssistantRequest,
  type AssistantSource,
  type AssistantThread,
} from "./state";

/** useAssistant 管理当前租户的对话与文件；关闭窗口不会取消服务端已提交任务。 */
export function useAssistant(tenantId: string, open: boolean, initial: AssistantRequest) {
  const [threads, setThreads] = useState<AssistantThread[]>([]);
  const [thread, setThread] = useState<AssistantThread | null>(null);
  const [job, setJob] = useState<AssistantJob | null>(null);
  const [source, setSource] = useState<AssistantSource>(initial.source || "tenant");
  const [draft, setDraft] = useState("");
  const [assets, setAssets] = useState<AssistantAsset[]>([]);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(false);
  const [sending, setSending] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [changing, setChanging] = useState(false);
  const epoch = useRef(0);
  const pending = useRef<{ signature: string; id: string } | null>(null);
  const assetList = useRef(assets);
  assetList.current = assets;
  const uploadLock = useRef(false);
  const assetCache = useRef(new Map<string, AssistantAsset>());
  const storageKey = `goodchat.assistant.thread.${tenantId}`;

  /** remember 仅记忆对话编号，不把模型密钥、图片或对话正文写入浏览器存储。 */
  const remember = useCallback(
    (id: string) => {
      try {
        sessionStorage.setItem(storageKey, id);
      } catch {
        /* 无存储权限时仍可使用当前窗口。 */
      }
    },
    [storageKey],
  );

  /** applyThread 用完整服务端对话恢复消息与最近任务，预览保持为服务端隔离地址。 */
  const applyThread = useCallback(
    (value: AssistantThread) => {
      setThread(value);
      setSource(value.source || "tenant");
      setJob(value.active_job || value.latest_job || null);
      remember(value.id);
    },
    [remember],
  );

  /** reloadThreads 刷新当前租户的历史记录，旧租户返回的数据不能覆盖新面板。 */
  const reloadThreads = useCallback(async () => {
    const current = epoch.current;
    const result = await api<{ items: AssistantThread[] }>("/tenant/assistant/threads", {
      tenantId,
    });
    if (current === epoch.current) setThreads(result.items || []);
  }, [tenantId]);

  /** selectThread 切换持久对话并丢弃上一对话的未提交图片，失败时保留已有内容。 */
  const selectThread = useCallback(
    async (id: string) => {
      const current = ++epoch.current;
      setLoading(true);
      setError(null);
      try {
        const result = await api<AssistantThread>(`/tenant/assistant/threads/${id}`, { tenantId });
        if (current !== epoch.current) return;
        applyThread(result);
        setDraft("");
        setAssets([]);
        pending.current = null;
      } catch (problem) {
        if (current === epoch.current) setError(problem);
      } finally {
        if (current === epoch.current) setLoading(false);
      }
    },
    [tenantId, applyThread],
  );

  /** newThread 创建对话前的空白编辑态，用户首次发送时才写入持久记录。 */
  function newThread(nextSource: AssistantSource = source) {
    epoch.current++;
    setThread(null);
    setJob(null);
    setSource(nextSource);
    setDraft("");
    setAssets([]);
    setError(null);
    setLoading(false);
    pending.current = null;
    try {
      sessionStorage.removeItem(storageKey);
    } catch {
      /* 无存储权限无需处理。 */
    }
  }

  useEffect(() => {
    if (!open || !tenantId) return;
    const current = ++epoch.current;
    setError(null);
    setLoading(true);
    setSending(false);
    setChanging(false);
    setUploading(false);
    /** restore 重新读取历史并恢复指定或最近对话，二创只创建私有编辑草稿。 */
    async function restore() {
      try {
        const list = await api<{ items: AssistantThread[] }>("/tenant/assistant/threads", {
          tenantId,
        });
        if (current !== epoch.current) return;
        setThreads(list.items || []);
        let id = initial.threadId || "";
        if (initial.templateId) {
          const created = await api<AssistantThread>("/tenant/assistant/threads", {
            tenantId,
            method: "POST",
            idempotent: true,
            bodyJSON: {
              title: `${initial.templateName || "模板"} · AI 二创`,
              template_id: initial.templateId,
              source: initial.source || "tenant",
            },
          });
          if (current !== epoch.current) return;
          id = created.id;
          setThreads((items) => [created, ...items.filter((item) => item.id !== created.id)]);
          window.dispatchEvent(new Event("goodchat:templates-changed"));
        } else if (!id && !initial.prompt) {
          try {
            id = sessionStorage.getItem(storageKey) || "";
          } catch {
            /* 禁用存储时使用空白对话。 */
          }
          if (!list.items?.some((item) => item.id === id)) id = "";
        }
        if (id) {
          const value = await api<AssistantThread>(`/tenant/assistant/threads/${id}`, { tenantId });
          if (current === epoch.current) applyThread(value);
        } else if (current === epoch.current) {
          setThread(null);
          setJob(null);
          setSource(initial.source || "tenant");
        }
        if (current === epoch.current) {
          setDraft(initial.prompt || "");
          setAssets([]);
          pending.current = null;
        }
      } catch (problem) {
        if (current === epoch.current) setError(problem);
      } finally {
        if (current === epoch.current) setLoading(false);
      }
    }
    void restore();
    return () => {
      epoch.current++;
    };
  }, [open, tenantId, initial.nonce, applyThread, storageKey]);

  useEffect(() => {
    if (!open || !thread?.id) return;
    const selected = thread.id;
    const controller = new AbortController();
    const ids = [...new Set((thread.messages || []).flatMap((item) => item.asset_ids || []))];
    /** restoreImages 为最近对话中的参考图读取受权图片，不向外部地址请求或写入浏览器存储。 */
    async function restoreImages() {
      for (let offset = 0; offset < ids.length; offset += 4) {
        const batch = ids.slice(offset, offset + 4).filter((id) => !assetCache.current.has(id));
        const results = await Promise.allSettled(
          batch.map((id) =>
            api<AssistantAsset>(`/tenant/assistant/assets/${id}`, {
              tenantId,
              signal: controller.signal,
            }),
          ),
        );
        if (controller.signal.aborted) return;
        for (const result of results)
          if (result.status === "fulfilled") assetCache.current.set(result.value.id, result.value);
        setThread((value) =>
          value?.id !== selected
            ? value
            : {
                ...value,
                messages: value.messages?.map((item) => ({
                  ...item,
                  assets: (item.asset_ids || [])
                    .map((id) => assetCache.current.get(id))
                    .filter((asset): asset is AssistantAsset => !!asset),
                })),
              },
        );
      }
    }
    if (ids.length) void restoreImages();
    return () => controller.abort();
  }, [open, tenantId, thread?.id, thread?.messages?.map((item) => item.id).join(":")]);

  useEffect(() => {
    if (!open || !job || !activeAssistantJob(job)) return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const current = epoch.current;
    const controller = new AbortController();
    /** poll 串行读取任务阶段，结束后拉回完整对话与真实预览，不制造模拟完成状态。 */
    async function poll() {
      try {
        const next = await api<AssistantJob>(`/tenant/assistant/jobs/${job!.id}`, {
          tenantId,
          signal: controller.signal,
        });
        if (stopped || current !== epoch.current) return;
        setError(null);
        if (!activeAssistantJob(next)) {
          if (thread?.id) {
            const detail = await api<AssistantThread>(`/tenant/assistant/threads/${thread.id}`, {
              tenantId,
              signal: controller.signal,
            });
            if (!stopped && current === epoch.current) applyThread(detail);
          }
          await reloadThreads();
          if (!stopped && next.status === "succeeded")
            window.dispatchEvent(new Event("goodchat:templates-changed"));
          if (!stopped && current === epoch.current) setJob(next);
          return;
        }
        setJob(next);
      } catch (problem) {
        if (!stopped && current === epoch.current) setError(problem);
      }
      if (!stopped) timer = setTimeout(poll, 2500);
    }
    timer = setTimeout(poll, 1200);
    return () => {
      stopped = true;
      clearTimeout(timer);
      controller.abort();
    };
  }, [open, job?.id, activeAssistantJob(job), tenantId, thread?.id, applyThread, reloadThreads]);

  /** send 保留同一内容的提交编号，超时后重试由服务端去重，上传完成前不能提交。 */
  async function send() {
    if (!draft.trim() || sending || loading || uploading || activeAssistantJob(job)) return;
    const current = epoch.current;
    setSending(true);
    setError(null);
    try {
      let selected = thread;
      if (!selected) {
        selected = await api<AssistantThread>("/tenant/assistant/threads", {
          tenantId,
          method: "POST",
          bodyJSON: { title: draft.trim().slice(0, 40), source },
          idempotent: true,
        });
        if (current !== epoch.current) return;
        applyThread(selected);
      }
      const content = draft.trim();
      const asset_ids = assets.map((asset) => asset.id);
      const signature = JSON.stringify([selected.id, content, asset_ids]);
      if (pending.current?.signature !== signature)
        pending.current = { signature, id: crypto.randomUUID() };
      const result = await api<{ job: AssistantJob }>(
        `/tenant/assistant/threads/${selected.id}/messages`,
        {
          tenantId,
          method: "POST",
          bodyJSON: { content, asset_ids, client_message_id: pending.current.id },
        },
      );
      if (current !== epoch.current) return;
      pending.current = null;
      setDraft("");
      setAssets([]);
      setJob(result.job);
      const detail = await api<AssistantThread>(`/tenant/assistant/threads/${selected.id}`, {
        tenantId,
      });
      if (current !== epoch.current) return;
      applyThread(detail);
      await reloadThreads();
    } catch (problem) {
      if (current === epoch.current) setError(problem);
    } finally {
      if (current === epoch.current) setSending(false);
    }
  }

  /** uploadFiles 顺序上传并验证引用图片，只有服务端确认的图片编号才随消息发送。 */
  async function uploadFiles(files: File[]) {
    if (uploadLock.current || sending || activeAssistantJob(job)) return;
    uploadLock.current = true;
    const current = epoch.current;
    setUploading(true);
    setError(null);
    try {
      for (const file of files) {
        const problem = assistantAssetProblem(file, assetList.current.length);
        if (problem) throw new Error(problem);
        const form = new FormData();
        form.set("file", file);
        const asset = await api<AssistantAsset>("/tenant/assistant/assets", {
          tenantId,
          method: "POST",
          body: form,
        });
        if (current !== epoch.current) return;
        assetList.current = [...assetList.current, asset];
        setAssets(assetList.current);
      }
    } catch (problem) {
      if (current === epoch.current) setError(problem);
    } finally {
      uploadLock.current = false;
      if (current === epoch.current) setUploading(false);
    }
  }

  /** cancel 请求服务端取消当前阶段，仍以返回和后续轮询的状态为准。 */
  async function cancel() {
    if (!thread || changing) return;
    const current = epoch.current;
    setChanging(true);
    setError(null);
    try {
      await api(`/tenant/assistant/threads/${thread.id}/cancel`, {
        tenantId,
        method: "POST",
        bodyJSON: {},
      });
      const detail = await api<AssistantThread>(`/tenant/assistant/threads/${thread.id}`, {
        tenantId,
      });
      if (current === epoch.current) applyThread(detail);
    } catch (problem) {
      if (current === epoch.current) setError(problem);
    } finally {
      if (current === epoch.current) setChanging(false);
    }
  }

  /** retry 只重试后端允许的失败阶段，供应商用量未知时由界面先提示。 */
  async function retry() {
    if (!job || changing) return;
    const current = epoch.current;
    setChanging(true);
    setError(null);
    try {
      const next = await api<AssistantJob>(`/tenant/assistant/jobs/${job.id}/retry`, {
        tenantId,
        method: "POST",
        bodyJSON: {},
      });
      if (current === epoch.current) setJob(next);
    } catch (problem) {
      if (current === epoch.current) setError(problem);
    } finally {
      if (current === epoch.current) setChanging(false);
    }
  }

  /** refresh 恢复失败的读取，保留用户尚未发送的文字和图片。 */
  async function refresh() {
    setError(null);
    try {
      await reloadThreads();
      if (thread) {
        const current = epoch.current;
        const detail = await api<AssistantThread>(`/tenant/assistant/threads/${thread.id}`, {
          tenantId,
        });
        if (current === epoch.current) applyThread(detail);
      }
    } catch (problem) {
      setError(new Error(errorMessage(problem)));
    }
  }

  return {
    threads,
    thread,
    job,
    source,
    draft,
    setDraft,
    assets,
    setAssets,
    error,
    loading,
    sending,
    uploading,
    changing,
    selectThread,
    newThread,
    send,
    uploadFiles,
    cancel,
    retry,
    refresh,
  };
}
