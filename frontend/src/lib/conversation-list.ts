/** 文件作用说明：计算会话自然日期范围，并串行维护游标分页与实时刷新，避免列表漏项和重复。 */
import type { Conversation } from "./types.ts";

export type ConversationPeriod = { from: string; to: string };
export type ConversationPage = {
  items: Conversation[];
  next_cursor: string;
  has_more: boolean;
  timezone?: string;
};
export type ConversationListSnapshot = ConversationPage & { loaded_pages: number };

/** recentConversationPeriod 按租户时区计算含今天的三个自然日，跨月、跨年和夏令时均按日历推算。 */
export function recentConversationPeriod(
  now = new Date(),
  timezone = "Asia/Shanghai",
): ConversationPeriod {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(now);
  const value = (name: Intl.DateTimeFormatPartTypes) =>
    parts.find((part) => part.type === name)!.value;
  const today = `${value("year")}-${value("month")}-${value("day")}`;
  const first = new Date(`${today}T00:00:00Z`);
  first.setUTCDate(first.getUTCDate() - 2);
  return { from: first.toISOString().slice(0, 10), to: today };
}

/** conversationActivityMicros 保留公开消息活动时间的微秒精度，时区交由标准日期解析，异常值按零处理。 */
function conversationActivityMicros(value: string): bigint {
  const milliseconds = Date.parse(value);
  if (!Number.isFinite(milliseconds)) return 0n;
  const fraction = /\.(\d+)(?:z|[+-]\d{2}:?\d{2})$/i.exec(value)?.[1] || "";
  const remainder = fraction.padEnd(6, "0").slice(3, 6);
  return BigInt(milliseconds) * 1000n + BigInt(remainder);
}

/** mergeConversationPages 以服务端最新记录覆盖重复编号，再按活动时间降序和编号升序保持稳定顺序。 */
export function mergeConversationPages(
  previous: Conversation[],
  incoming: Conversation[],
): Conversation[] {
  const byId = new Map(previous.map((item) => [item.id, item]));
  for (const item of incoming) byId.set(item.id, item);
  return [...byId.values()].sort((a, b) => {
    const timeA = conversationActivityMicros(a.last_message_at || a.created_at || "");
    const timeB = conversationActivityMicros(b.last_message_at || b.created_at || "");
    return timeA > timeB ? -1 : timeA < timeB ? 1 : a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  });
}

/** createConversationPager 合并刷新唤醒并串行翻页，刷新成功前保留原列表，重建已加载前缀以接回活跃老会话。 */
export function createConversationPager(options: {
  fetchPage: (cursor: string) => Promise<ConversationPage>;
  onSnapshot: (snapshot: ConversationListSnapshot) => void;
  onBusy: (initial: boolean, more: boolean) => void;
  onError: (error: unknown) => void;
  isForbidden: (error: unknown) => boolean;
  isInvalidCursor: (error: unknown) => boolean;
}) {
  let snapshot: ConversationListSnapshot = {
    items: [],
    next_cursor: "",
    has_more: false,
    loaded_pages: 0,
  };
  let stopped = false;
  let refreshPending = false;
  let morePending = false;
  let loadingMore = false;
  let running: Promise<void> | null = null;

  /** checkedPage 拒绝不前进的游标，防止触底回调在异常响应下不断请求同一页。 */
  async function checkedPage(cursor: string) {
    const page = await options.fetchPage(cursor);
    if (page.has_more && (!page.next_cursor || page.next_cursor === cursor))
      throw new Error("会话分页游标未前进，请重试。");
    return page;
  }

  /** run 顺序处理刷新与触底请求；刷新期间收到的新事件在本轮结束后继续补查。 */
  async function run(onFinish: () => void) {
    try {
      while (!stopped && (refreshPending || morePending)) {
        // 已排队的触底先消费一次，持续消息唤醒不能让用户翻页一直等待；随后仍重建最新前缀。
        const refresh = refreshPending && !morePending;
        if (refresh) refreshPending = false;
        else morePending = false;
        loadingMore = !refresh;
        options.onBusy(!snapshot.loaded_pages, loadingMore);
        try {
          let next: ConversationListSnapshot;
          if (refresh) {
            let cursor = "";
            let items: Conversation[] = [];
            let page: ConversationPage;
            let loaded = 0;
            const target = Math.max(1, snapshot.loaded_pages);
            do {
              page = await checkedPage(cursor);
              if (stopped) return;
              items = mergeConversationPages(items, page.items || []);
              cursor = page.next_cursor;
              loaded++;
            } while (page.has_more && loaded < target);
            next = { ...page, items, loaded_pages: loaded };
          } else {
            if (!snapshot.has_more || !snapshot.next_cursor) continue;
            const page = await checkedPage(snapshot.next_cursor);
            if (stopped) return;
            next = {
              ...page,
              timezone: page.timezone || snapshot.timezone,
              items: mergeConversationPages(snapshot.items, page.items || []),
              loaded_pages: snapshot.loaded_pages + 1,
            };
          }
          snapshot = next;
          options.onError(null);
          options.onSnapshot(snapshot);
        } catch (error) {
          if (stopped) return;
          if (options.isForbidden(error)) {
            snapshot = { items: [], next_cursor: "", has_more: false, loaded_pages: 0 };
            morePending = false;
            refreshPending = false;
            options.onSnapshot(snapshot);
          } else if (!refresh && options.isInvalidCursor(error)) {
            // 签名游标失效时从首屏重建相同页数，不拿失效游标持续重试。
            refreshPending = true;
          }
          options.onError(error);
        } finally {
          loadingMore = false;
          if (!stopped) options.onBusy(false, false);
        }
      }
    } finally {
      onFinish();
    }
  }

  /** schedule 启动唯一队列；清理与最后检查同处一个微任务，避免丢失末尾唤醒。 */
  function schedule() {
    if (stopped) return Promise.resolve();
    if (running) return running;
    let finished = false;
    const task = run(() => {
      finished = true;
      running = null;
    });
    if (!finished) running = task;
    return task;
  }
  return {
    /** refresh 请求重新读取已加载范围，不立即清空页面。 */
    refresh() {
      if (stopped) return Promise.resolve();
      refreshPending = true;
      return schedule();
    },
    /** loadMore 合并相同触底请求，防止同一游标被并发消费。 */
    loadMore() {
      if (stopped || loadingMore || morePending || !snapshot.has_more)
        return running || Promise.resolve();
      morePending = true;
      return schedule();
    },
    /** stop 撤销生命周期，旧租户或旧筛选的结果不得发布。 */
    stop() {
      stopped = true;
      refreshPending = false;
      morePending = false;
    },
  };
}
