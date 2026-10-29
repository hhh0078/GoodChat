/** 文件作用说明：管理客服收件列表的日期滚动、选中详情保留和触底加载，将视图与分页状态隔离。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { recentConversationPeriod, type ConversationPeriod } from "@/lib/conversation-list";
import type { Conversation } from "@/lib/types";
import { useConversationList } from "./use-conversation-list";

/** useConversationInbox 将当前收件箱绑定租户与成员，分页重排后保持选中聊天并再次校验授权。 */
export function useConversationInbox({
  tenantId,
  actorId,
  enabled,
}: {
  tenantId: string;
  actorId: string;
  enabled: boolean;
}) {
  const [selected, setSelected] = useState("");
  const [status, setStatus] = useState("");
  const [query, setQuery] = useState("");
  const [period, setPeriod] = useState<ConversationPeriod>(() => recentConversationPeriod());
  const [customPeriod, setCustomPeriod] = useState(false);
  const [selection, setSelection] = useState<{ scope: string; item: Conversation } | null>(null);
  const [selectionError, setSelectionError] = useState<unknown>(null);
  const listNode = useRef<HTMLDivElement | null>(null);
  const listEnd = useRef<HTMLDivElement | null>(null);
  const conversations = useConversationList({
    tenantId,
    actorId,
    enabled,
    status,
    query,
    period,
  });
  const selectionScope = `${tenantId}:${actorId}`;
  const active =
    conversations.items.find((item) => item.id === selected) ||
    (selection?.scope === selectionScope && selection.item.id === selected
      ? selection.item
      : undefined);
  useEffect(() => {
    if (customPeriod) return;
    /** updateDefaultPeriod 以租户自然日滚动默认日期，午夜或休眠恢复后不会继续显示昨天的区间。 */
    function updateDefaultPeriod() {
      const next = recentConversationPeriod(new Date(), conversations.timezone);
      setPeriod((previous) =>
        previous.from === next.from && previous.to === next.to ? previous : next,
      );
    }
    updateDefaultPeriod();
    const timer = setInterval(updateDefaultPeriod, 30000);
    document.addEventListener("visibilitychange", updateDefaultPeriod);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", updateDefaultPeriod);
    };
  }, [customPeriod, conversations.timezone]);
  useEffect(() => {
    setCustomPeriod(false);
    setSelection(null);
    setSelected("");
    setSelectionError(null);
  }, [selectionScope]);
  useEffect(() => {
    if (!selected || conversations.loading) return;
    if (
      conversations.error instanceof ApiError &&
      [401, 403, 404].includes(conversations.error.status)
    ) {
      setSelection(null);
      setSelected("");
      return;
    }
    const listed = conversations.items.find((item) => item.id === selected);
    if (listed) {
      setSelection({ scope: selectionScope, item: listed });
      setSelectionError(null);
      return;
    }
    if (conversations.error) return;
    // 新咨询将选中项挤出分页前缀时，单独重新鉴权读取详情，聊天和资料栏继续绑定原咨询。
    const controller = new AbortController();
    api<Conversation>(`/conversations/${selected}`, { tenantId, signal: controller.signal })
      .then((item) => {
        if (!controller.signal.aborted) {
          setSelection({ scope: selectionScope, item });
          setSelectionError(null);
        }
      })
      .catch((problem) => {
        if (controller.signal.aborted) return;
        if (problem instanceof ApiError && [401, 403, 404].includes(problem.status)) {
          setSelection(null);
          setSelected("");
        }
        setSelectionError(problem);
      });
    return () => controller.abort();
  }, [
    selected,
    selectionScope,
    tenantId,
    conversations.items,
    conversations.loading,
    conversations.error,
  ]);
  useEffect(() => {
    const end = listEnd.current,
      root = listNode.current;
    if (
      !end ||
      !root ||
      !conversations.hasMore ||
      conversations.loading ||
      conversations.loadingMore ||
      conversations.error ||
      typeof IntersectionObserver === "undefined"
    )
      return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) conversations.loadMore();
      },
      { root, rootMargin: "0px 0px 80px 0px" },
    );
    observer.observe(end);
    return () => observer.disconnect();
  }, [
    conversations.items.length,
    conversations.hasMore,
    conversations.loading,
    conversations.loadingMore,
    conversations.error,
    conversations.loadMore,
  ]);

  /** select 保存已鉴权卡片快照，防止新消息排序把正在读取的会话挤出前缀时丢失聊天窗口。 */
  function select(id: string) {
    setSelected(id);
    const item = conversations.items.find((entry) => entry.id === id);
    if (item) setSelection({ scope: selectionScope, item });
    setSelectionError(null);
  }
  /** resetPosition 切换筛选时从列表顶部开始，旧游标由查询生命周期统一取消。 */
  function resetPosition() {
    setSelected("");
    setSelectionError(null);
    if (listNode.current) listNode.current.scrollTop = 0;
  }
  /** checkEnd 为触底与不支持IntersectionObserver的浏览器提供加载入口。 */
  function checkEnd() {
    const node = listNode.current;
    if (
      node &&
      node.scrollHeight - node.scrollTop - node.clientHeight < 80 &&
      conversations.hasMore &&
      !conversations.loadingMore &&
      !conversations.error
    )
      conversations.loadMore();
  }
  return {
    ...conversations,
    error: conversations.error || selectionError,
    active,
    selected,
    status,
    period,
    listNode,
    listEnd,
    select,
    checkEnd,
    /** changeStatus 按咨询状态重新查询并清除旧选择。 */
    changeStatus(value: string) {
      setStatus(value);
      resetPosition();
    },
    /** search 按当前关键词重新查询并清除旧选择。 */
    search(value: string) {
      setQuery(value);
      resetPosition();
    },
    /** changePeriod 让自定义日期固定，恢复默认则立即使用当前租户的三个自然日。 */
    changePeriod(next: ConversationPeriod, custom: boolean) {
      setCustomPeriod(custom);
      setPeriod(custom ? next : recentConversationPeriod(new Date(), conversations.timezone));
      resetPosition();
    },
  };
}
