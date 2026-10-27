/** 文件作用说明：以当前租户为边界加载列表，处理切换租户、取消请求与错误状态。 */
"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "./api";
import { useSession } from "@/features/auth/session";
/** useResource 加载资源列表，更新触发真实重新查询。 */
export function useResource<T>(path: string, platform = false) {
  const { tenantId } = useSession();
  const [items, setItems] = useState<T[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  const scope = useRef("");
  useEffect(() => {
    const controller = new AbortController();
    const nextScope = `${tenantId}:${platform}:${path}`;
    if (scope.current !== nextScope) {
      setItems([]);
      scope.current = nextScope;
    }
    setError(null);
    if (!path || (!tenantId && !platform)) {
      setLoading(false);
      return () => controller.abort();
    }
    setLoading(true);
    api<{ items: T[] }>(path, {
      tenantId: platform ? undefined : tenantId,
      signal: controller.signal,
    })
      .then((data) => {
        if (!controller.signal.aborted) setItems(Array.isArray(data?.items) ? data.items : []);
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [path, tenantId, platform, revision]);
  const reload = useCallback(() => setRevision((value) => value + 1), []);
  return { items, loading, error, reload, tenantId };
}
