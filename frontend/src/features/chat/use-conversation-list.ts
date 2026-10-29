/** 文件作用说明：将客服会话分页绑定租户、成员和完整筛选，取消过时请求并提供稳定刷新入口。 */
"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError } from "@/lib/api";
import {
  createConversationPager,
  type ConversationListSnapshot,
  type ConversationPage,
  type ConversationPeriod,
} from "@/lib/conversation-list";

/** useConversationList 每页读取三十条，并将实时刷新、触底和查询切换隔离到独立生命周期。 */
export function useConversationList({
  tenantId,
  actorId,
  enabled,
  status,
  query,
  period,
}: {
  tenantId: string;
  actorId: string;
  enabled: boolean;
  status: string;
  query: string;
  period: ConversationPeriod;
}) {
  const path = `/conversations?${new URLSearchParams({
    status,
    query,
    activity_from: period.from,
    activity_to: period.to,
    limit: "30",
  })}`;
  const scope = JSON.stringify([tenantId, actorId, enabled, path]);
  const identityScope = JSON.stringify([tenantId, actorId]);
  const [knownTimezone, setKnownTimezone] = useState({ identityScope: "", value: "Asia/Shanghai" });
  const [result, setResult] = useState<{
    scope: string;
    snapshot?: ConversationListSnapshot;
    loading: boolean;
    loadingMore: boolean;
    error: unknown;
  }>({ scope: "", loading: true, loadingMore: false, error: null });
  const operations = useRef<ReturnType<typeof createConversationPager> | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    setResult({ scope, loading: enabled, loadingMore: false, error: null });
    if (!enabled || !tenantId || !actorId) return () => controller.abort();
    const pager = createConversationPager({
      fetchPage: (cursor) =>
        api<ConversationPage>(`${path}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, {
          tenantId,
          signal: controller.signal,
        }),
      onSnapshot: (snapshot) => {
        if (!controller.signal.aborted) {
          setResult((previous) => ({ ...previous, scope, snapshot }));
          if (snapshot.timezone)
            setKnownTimezone((previous) =>
              previous.identityScope === identityScope && previous.value === snapshot.timezone
                ? previous
                : { identityScope, value: snapshot.timezone! },
            );
        }
      },
      onBusy: (loading, loadingMore) => {
        if (!controller.signal.aborted)
          setResult((previous) => ({ ...previous, scope, loading, loadingMore }));
      },
      onError: (error) => {
        if (!controller.signal.aborted) setResult((previous) => ({ ...previous, scope, error }));
      },
      isForbidden: (error) => error instanceof ApiError && [401, 403, 404].includes(error.status),
      isInvalidCursor: (error) => error instanceof ApiError && error.code === "invalid_cursor",
    });
    operations.current = pager;
    void pager.refresh();
    return () => {
      controller.abort();
      pager.stop();
      if (operations.current === pager) operations.current = null;
    };
  }, [scope, identityScope, tenantId, actorId, enabled, path]);
  const reload = useCallback(() => {
    void operations.current?.refresh();
  }, []);
  const loadMore = useCallback(() => {
    void operations.current?.loadMore();
  }, []);
  const scoped = result.scope === scope ? result : null;
  return {
    items: scoped?.snapshot?.items || [],
    hasMore: scoped?.snapshot?.has_more || false,
    timezone: knownTimezone.identityScope === identityScope ? knownTimezone.value : "Asia/Shanghai",
    loading: enabled && (!scoped || scoped.loading),
    loadingMore: scoped?.loadingMore || false,
    error: scoped?.error,
    reload,
    loadMore,
  };
}
