/** 文件作用说明：按租户身份读取游标列表，取消旧请求、合并触底请求并按编号去重。 */
"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { useLocale } from "next-intl";
import { useSession } from "@/features/auth/session";
import { api, ApiError } from "./api";
import { translate as tr } from "@/i18n/runtime";
type Page<T> = { items: T[]; next_cursor: string; has_more: boolean };

/** usePagedResource 列表刷新从第一页重读，追加失败保留已读项并等待用户主动重试。 */
export function usePagedResource<T extends { id: string }, Metadata = object>(
  path: string,
  enabled = true,
) {
  const { tenantId, current } = useSession();
  const locale = useLocale();
  const [revision, setRevision] = useState(0);
  const scope = JSON.stringify([tenantId, current.user.id, locale, path, enabled, revision]);
  const [state, setState] = useState<{
    scope: string;
    items: T[];
    data?: Metadata;
    loading: boolean;
    hasMore: boolean;
    error: unknown;
  }>({ scope: "", items: [], loading: true, hasMore: false, error: null });
  const operation = useRef<(() => void) | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    let pending = false;
    let cursor = "";
    let more = true;
    setState({ scope, items: [], loading: enabled, hasMore: false, error: null });
    /** load 只消费一个当前游标，身份变化或组件销毁后不再更新视图。 */
    async function load() {
      if (pending || !more || controller.signal.aborted || !enabled || !tenantId) return;
      pending = true;
      setState((previous) => ({ ...previous, loading: true, error: null }));
      try {
        const query = new URLSearchParams({ limit: "20", ...(cursor ? { cursor } : {}) });
        const page = await api<Page<T> & Metadata>(
          `${path}${path.includes("?") ? "&" : "?"}${query}`,
          { tenantId, signal: controller.signal },
        );
        if (controller.signal.aborted) return;
        if (page.has_more && (!page.next_cursor || page.next_cursor === cursor))
          throw new Error(tr("分页位置无效，请刷新后重试。"));
        const first = !cursor;
        cursor = page.next_cursor || "";
        more = !!page.has_more;
        setState((previous) => ({
          scope,
          items: Array.from(
            new Map(
              [...previous.items, ...(page.items || [])].map((item) => [item.id, item]),
            ).values(),
          ),
          data: first ? page : previous.data,
          loading: false,
          hasMore: more,
          error: null,
        }));
      } catch (error) {
        if (!controller.signal.aborted)
          setState((previous) => ({
            ...previous,
            ...(error instanceof ApiError && [401, 403, 404].includes(error.status)
              ? { items: [], data: undefined, hasMore: false }
              : {}),
            loading: false,
            error,
          }));
      } finally {
        pending = false;
      }
    }
    operation.current = () => {
      void load();
    };
    void load();
    return () => {
      controller.abort();
      operation.current = null;
    };
  }, [scope, path, tenantId, enabled]);
  const reload = useCallback(() => setRevision((value) => value + 1), []);
  const loadMore = useCallback(() => operation.current?.(), []);
  const active = state.scope === scope ? state : null;
  return {
    items: active?.items || [],
    data: active?.data,
    loading: enabled && (!active || active.loading),
    hasMore: active?.hasMore || false,
    error: active?.error,
    reload,
    loadMore,
    tenantId,
  };
}
