/** 文件作用说明：按客户展示跨会话内部备注记录，支持稳定分页和幂等新增，不向访客发送。 */
"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { App, Button, Input } from "antd";
import { api, ApiError, errorMessage } from "@/lib/api";
import { mergeNotes, noteFailureState } from "@/lib/workspace-display";
import { ErrorNotice } from "@/components/page-state";
import { NoteAttachment } from "./note-attachment";
type Note = {
  id: string;
  content: string;
  created_at: string;
  author_name?: string;
  attachment?: {
    file_name: string;
    content_type: string;
    size_bytes?: number;
    download_path: string;
  };
};
type NotePage = { items: Note[]; has_more: boolean; next_before?: string; next_before_id?: string };

/** CustomerNotes 保留已加载旧备注，失败重试继续使用原业务编号避免重复记录。 */
export function CustomerNotes({ contactId, tenantId }: { contactId: string; tenantId: string }) {
  const { message } = App.useApp();
  const [history, setHistory] = useState<{
    items: Note[];
    cursor: { before: string; before_id: string } | null;
    paged: boolean;
  }>({ items: [], cursor: null, paged: false });
  const notes = history.items;
  const cursor = history.cursor;
  const [draft, setDraft] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const saving = useRef(false);
  const pending = useRef<{ content: string; id: string } | null>(null);
  const mounted = useRef(true);
  const path = `/tenant/contacts/${contactId}/notes`;
  /** loadLatest 合并最新一页，权限失败立即移除已缓存备注。 */
  const loadLatest = useCallback(
    async (signal?: AbortSignal) => {
      try {
        const page = await api<NotePage>(`${path}?limit=30`, { tenantId, signal });
        if (signal?.aborted || !mounted.current) return;
        setHistory((previous) => ({
          items: mergeNotes(previous.items, page.items || []),
          cursor: previous.paged
            ? previous.cursor
            : page.has_more && page.next_before && page.next_before_id
              ? { before: page.next_before, before_id: page.next_before_id }
              : null,
          paged: previous.paged,
        }));
        setError(null);
      } catch (problem) {
        if (signal?.aborted || !mounted.current) return;
        setHistory((previous) =>
          noteFailureState(previous, problem instanceof ApiError ? problem.status : 0),
        );
        setError(problem);
      } finally {
        if (!signal?.aborted && mounted.current) setLoading(false);
      }
    },
    [path, tenantId],
  );
  useEffect(() => {
    mounted.current = true;
    const controller = new AbortController();
    void loadLatest(controller.signal);
    const timer = setInterval(() => void loadLatest(controller.signal), 10000);
    return () => {
      mounted.current = false;
      controller.abort();
      clearInterval(timer);
    };
  }, [loadLatest]);
  /** save 只提交客户备注资源，保存确认后清空输入并合并记录。 */
  async function save() {
    const content = draft.trim();
    if (!content || saving.current) return;
    saving.current = true;
    setBusy(true);
    if (pending.current?.content !== content)
      pending.current = { content, id: crypto.randomUUID() };
    try {
      const note = await api<Note>(path, {
        tenantId,
        method: "POST",
        bodyJSON: { content, client_note_id: pending.current.id },
      });
      if (!mounted.current) return;
      setHistory((previous) => ({ ...previous, items: mergeNotes(previous.items, [note]) }));
      setDraft("");
      pending.current = null;
      setError(null);
    } catch (problem) {
      if (mounted.current) message.error(errorMessage(problem));
    } finally {
      saving.current = false;
      if (mounted.current) setBusy(false);
    }
  }
  /** loadOlder 以时间和编号联合游标加载旧记录，避免同时间记录重复或遗漏。 */
  async function loadOlder() {
    if (!cursor || loading) return;
    setLoading(true);
    try {
      const page = await api<NotePage>(
        `${path}?${new URLSearchParams({ ...cursor, limit: "30" })}`,
        { tenantId },
      );
      if (!mounted.current) return;
      setHistory((previous) => ({
        items: mergeNotes(previous.items, page.items || []),
        cursor:
          page.has_more && page.next_before && page.next_before_id
            ? { before: page.next_before, before_id: page.next_before_id }
            : null,
        paged: true,
      }));
      setError(null);
    } catch (problem) {
      if (mounted.current) {
        setError(problem);
        setHistory((previous) =>
          noteFailureState(previous, problem instanceof ApiError ? problem.status : 0),
        );
      }
    } finally {
      if (mounted.current) setLoading(false);
    }
  }
  return (
    <section className="customer-internal-notes">
      <h3>
        内部备注 <span>仅团队可见 · 跟随客户保留</span>
      </h3>
      <Input.TextArea
        aria-label="添加客户内部备注"
        placeholder="记录客户情况、跟进事项…"
        autoSize={{ minRows: 2, maxRows: 5 }}
        maxLength={5000}
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        disabled={busy}
      />
      <Button
        size="small"
        type="primary"
        loading={busy}
        disabled={!draft.trim()}
        onClick={() => void save()}
      >
        添加备注
      </Button>
      <ErrorNotice error={error} retry={() => void loadLatest()} />
      <ol className="customer-note-records">
        {notes.map((note) => (
          <li key={note.id}>
            {note.content && <p>{note.content}</p>}
            {note.attachment && (
              <NoteAttachment
                attachment={note.attachment}
                contactId={contactId}
                noteId={note.id}
                tenantId={tenantId}
              />
            )}
            <footer>
              <span>{note.author_name || "团队成员"}</span>
              <time>
                {new Date(note.created_at).toLocaleString("zh-CN", {
                  month: "2-digit",
                  day: "2-digit",
                  hour: "2-digit",
                  minute: "2-digit",
                })}
              </time>
            </footer>
          </li>
        ))}
      </ol>
      {loading && !notes.length && <p className="muted">正在读取备注…</p>}
      {cursor && (
        <Button size="small" type="text" loading={loading} onClick={() => void loadOlder()}>
          加载更早备注
        </Button>
      )}
    </section>
  );
}
