/** 文件作用说明：将统计缓存绑定查询范围，并串行执行同一生命周期的轮询，避免旧响应或重叠请求污染指标。 */
export type ReportSnapshot<T> = { scope: string; data: T };

/** reportScopeKey 为租户和完整查询生成无歧义范围标识，日期变化不会沿用旧指标。 */
export function reportScopeKey(tenantId: string, query: string): string {
  return JSON.stringify([tenantId, query]);
}

/** scopedReport 仅返回完全相同查询范围的缓存，切换日期失败时不会回退到上一日期。 */
export function scopedReport<T>(snapshot: ReportSnapshot<T> | null, scope: string): T | null {
  return snapshot?.scope === scope ? snapshot.data : null;
}

/** serialPoll 合并尚未完成请求期间的定时触发，生命周期已取消后不再启动任务。 */
export function serialPoll(signal: AbortSignal, action: () => Promise<void>): () => Promise<void> {
  let pending = false;
  return async function poll() {
    if (pending || signal.aborted) return;
    pending = true;
    try {
      await action();
    } finally {
      pending = false;
    }
  };
}
