/** 文件作用说明：独立查询授权聊天记录，并按相同筛选分别下载会话元数据或默认不含内部备注的正文CSV。 */
"use client";
import filterStyles from "@/components/filter-toolbar.module.css";
import { EmptyData, ErrorNotice, PageTitle } from "@/components/page-state";
import { useSession } from "@/features/auth/session";
import { AttachmentView } from "@/features/chat/attachment";
import { localizeStatic } from "@/i18n/runtime";
import { useFormats } from "@/i18n/use-formats";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import { requestCSV, startFileDownload } from "@/lib/csv-download";
import type { ChatMessage, Conversation, Member } from "@/lib/types";
import { useResource } from "@/lib/use-resource";
import { DownloadOutlined } from "@ant-design/icons";
import {
  App,
  Avatar,
  Button,
  DatePicker,
  Drawer,
  Input,
  Select,
  Space,
  Spin,
  Table,
  Tag,
} from "antd";
import dayjs from "dayjs";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { ExportJobs } from "./export-jobs";
const statusText: Record<string, string> = {
  queued: "排队中",
  assigned: "待接单",
  active: "接待中",
  closed: "已结束",
};
/** RecordsPage 以独立会话查询历史，导出生成结果和浏览器实际保存分开反馈。 */
export function RecordsPage() {
  const { currentPreferences, numberFormatter } = useFormats();

  const tr = useTranslator();
  const { tenantId, current } = useSession();
  const manages = current.memberships.some(
    (member) => member.tenant_id === tenantId && ["owner", "admin"].includes(member.role),
  );
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("");
  const [dates, setDates] = useState<string[]>([]);
  const [agent, setAgent] = useState("");
  const [channel, setChannel] = useState("");
  const [submittedFilters, setSubmittedFilters] = useState("");
  const [selected, setSelected] = useState<Conversation | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(false);
  const [exporting, setExporting] = useState("");
  const [exportError, setExportError] = useState<unknown>(null);
  const exportController = useRef<AbortController | null>(null);
  const { message } = App.useApp();
  const params = new URLSearchParams(submittedFilters);
  const records = useResource<Conversation>(`/tenant/chat-records?${params}`);
  const members = useResource<Member>(manages ? "/tenant/members" : "");
  /** searchRecords 一次提交当前筛选，让记录列表与导出使用相同条件。 */
  function searchRecords(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmittedFilters(
      new URLSearchParams({
        query: query.trim(),
        status,
        agent_id: agent,
        channel_code: channel.trim(),
        ...(dates[0] ? { from: dates[0], to: dates[1] } : {}),
      }).toString(),
    );
  }
  /** resetFilters 同时清空输入和已提交条件，避免残留不可见筛选。 */
  function resetFilters() {
    setQuery("");
    setStatus("");
    setDates([]);
    setAgent("");
    setChannel("");
    setSubmittedFilters("");
  }
  useEffect(() => {
    resetFilters();
    setSelected(null);
    setMessages([]);
    setExportError(null);
    return () => exportController.current?.abort();
  }, [tenantId]);
  useEffect(() => {
    if (!selected) return;
    const controller = new AbortController();
    setError(null);
    setMessages([]);
    setLoading(true);
    let pending = false;
    const conversationId = selected.id;
    /** load 复核所选历史的实时权限，旧范围不因网络错误持续留在资料抽屉。 */
    async function load() {
      if (pending || controller.signal.aborted) return;
      pending = true;
      try {
        const data = await api<{
          items: ChatMessage[];
        }>(`/conversations/${conversationId}/messages?limit=100`, {
          tenantId,
          signal: controller.signal,
        });
        if (!controller.signal.aborted) {
          setMessages(data.items || []);
          setError(null);
        }
      } catch (problem) {
        if (!controller.signal.aborted) {
          setMessages([]);
          setError(problem);
        }
      } finally {
        pending = false;
        if (!controller.signal.aborted) setLoading(false);
      }
    }
    void load();
    const interval = setInterval(() => void load(), 4000);
    /** resume 回到前台立即重验权限，避免后台定时器限流留下旧范围。 */
    function resume() {
      if (document.visibilityState === "visible") void load();
    }
    document.addEventListener("visibilitychange", resume);
    window.addEventListener("focus", resume);
    return () => {
      controller.abort();
      clearInterval(interval);
      document.removeEventListener("visibilitychange", resume);
      window.removeEventListener("focus", resume);
    };
  }, [selected, tenantId]);
  /** exportRecords 复用当前筛选，拒绝JSON错误、空实体和异常成功响应。 */
  async function exportRecords(includeMessages: boolean) {
    const controller = new AbortController();
    exportController.current = controller;
    setExporting(includeMessages ? "messages" : "metadata");
    setExportError(null);
    const filters = new URLSearchParams(params);
    filters.set("include_messages", String(includeMessages));
    filters.set("include_internal", "false");
    try {
      const file = await requestCSV(
        `/tenant/chat-records/export?${filters}`,
        tenantId,
        includeMessages ? tr("GoodChat-消息正文.csv") : tr("GoodChat-会话列表.csv"),
        controller.signal,
      );
      if (controller.signal.aborted) return;
      startFileDownload(file.blob, file.filename);
      message.success(
        tr("已发起{p0}下载{p1}，请查看浏览器下载记录", {
          p0: String(includeMessages ? tr("消息正文") : tr("会话列表")),
          p1: String(file.rows !== null ? tr("（{p0}条数据）", { p0: String(file.rows) }) : ""),
        }),
      );
    } catch (problem) {
      if (!controller.signal.aborted) setExportError(problem);
    } finally {
      if (exportController.current === controller) {
        setExporting("");
        exportController.current = null;
      }
    }
  }
  return (
    <section className="console-screen console-records">
      <PageTitle
        title={tr("聊天记录")}
        description={tr("按独立会话检索授权历史；日期按租户时区解释，结束日期包含当天。")}
        action={
          manages && (
            <Space wrap>
              <ExportJobs filters={params.toString()} />
              <Button
                icon={<DownloadOutlined />}
                disabled={
                  records.loading || !!records.error || !records.items.length || !!exporting
                }
                loading={exporting === "metadata"}
                onClick={() => void exportRecords(false)}
              >
                {tr("导出会话列表 ")}
              </Button>
              <Button
                icon={<DownloadOutlined />}
                disabled={
                  records.loading || !!records.error || !records.items.length || !!exporting
                }
                loading={exporting === "messages"}
                onClick={() => void exportRecords(true)}
              >
                {tr("导出消息正文 ")}
              </Button>
            </Space>
          )
        }
      />
      <div className="table-card">
        <form
          className={filterStyles.toolbar}
          aria-label={tr("聊天记录筛选")}
          onSubmit={searchRecords}
        >
          <div className={filterStyles.field} style={{ width: 150 }}>
            <span>{tr("客户")}</span>
            <Input
              placeholder={tr("名称 / 电话")}
              aria-label={tr("客户名称或电话")}
              variant="borderless"
              allowClear
              maxLength={200}
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
          </div>
          <div className={filterStyles.field} style={{ width: 116 }}>
            <span>{tr("状态")}</span>
            <Select
              aria-label={tr("会话状态")}
              variant="borderless"
              allowClear
              placeholder={tr("全部")}
              value={status || undefined}
              options={Object.entries(localizeStatic(statusText, tr)).map(([value, label]) => ({
                value,
                label,
              }))}
              onChange={(value) => setStatus(value || "")}
            />
          </div>
          <div className={`${filterStyles.field} ${filterStyles.date}`} style={{ width: 264 }}>
            <span>{tr("日期")}</span>
            <DatePicker.RangePicker
              aria-label={tr("会话日期")}
              variant="borderless"
              placeholder={[tr("开始"), tr("结束")]}
              value={dates[0] && dates[1] ? [dayjs(dates[0]), dayjs(dates[1])] : null}
              onChange={(value) =>
                setDates(
                  value?.[0] && value?.[1]
                    ? [value[0].format("YYYY-MM-DD"), value[1].format("YYYY-MM-DD")]
                    : [],
                )
              }
            />
          </div>
          {manages && (
            <div className={filterStyles.field} style={{ width: 130 }}>
              <span>{tr("客服")}</span>
              <Select
                aria-label={tr("接待客服")}
                variant="borderless"
                allowClear
                showSearch
                optionFilterProp="label"
                placeholder={tr("全部")}
                value={agent || undefined}
                options={members.items.map((item) => ({
                  label: item.display_name || item.email,
                  value: item.id,
                }))}
                onChange={(value) => setAgent(value || "")}
              />
            </div>
          )}
          <div className={filterStyles.field} style={{ width: 126 }}>
            <span>{tr("渠道")}</span>
            <Input
              placeholder={tr("来源编码")}
              aria-label={tr("渠道来源编码")}
              variant="borderless"
              allowClear
              value={channel}
              onChange={(event) => setChannel(event.target.value)}
            />
          </div>
          <div className={filterStyles.actions}>
            <Button type="primary" htmlType="submit">
              {tr("搜索 ")}
            </Button>
            <Button onClick={resetFilters}>{tr("重置")}</Button>
          </div>
        </form>
        {manages && (
          <p className="form-hint">
            {tr(
              "两个导出动作都沿用当前筛选。正文默认不含内部备注，最多 10000 条数据；超限请缩小范围。浏览器发起下载不代表文件已保存到磁盘。 ",
            )}
          </p>
        )}
        <ErrorNotice error={exportError} />
        <ErrorNotice error={records.error} retry={records.reload} />
        <Table
          rowKey="id"
          dataSource={records.items}
          loading={records.loading}
          scroll={{ x: 850 }}
          locale={{ emptyText: <EmptyData text={tr("没有符合条件的会话")} /> }}
          columns={[
            {
              title: tr("客户"),
              render: (_, row) =>
                row.contact?.display_name ||
                row.contact_name ||
                row.display_name ||
                row.contact_id?.slice(0, 8) ||
                tr("访客"),
            },
            {
              title: tr("接待客服"),
              render: (_, row) =>
                row.agent_name ? (
                  <Space size={8}>
                    <Avatar size={28} src={row.agent_avatar_url || undefined}>
                      {String(row.agent_name).slice(0, 1)}
                    </Avatar>
                    <span>{row.agent_name}</span>
                  </Space>
                ) : (
                  <span className="muted">{tr("尚未分配")}</span>
                ),
            },
            {
              title: tr("会话编号"),
              dataIndex: "id",
              ellipsis: true,
              className: "console-record-id",
            },
            {
              title: tr("状态"),
              dataIndex: "status",
              render: (value) => <Tag>{localizeStatic(statusText, tr)[value] || value}</Tag>,
            },
            {
              title: tr("创建时间"),
              dataIndex: "created_at",
              render: (value) =>
                value
                  ? new Date(String(value)).toLocaleString(currentPreferences().locale, {
                      timeZone: currentPreferences().timezone,
                    })
                  : "—",
            },
            {
              title: tr("操作"),
              render: (_, row) => (
                <Button type="link" size="small" onClick={() => setSelected(row)}>
                  {tr("查看聊天记录 ")}
                </Button>
              ),
            },
          ]}
        />
      </div>
      <Drawer
        open={!!selected}
        title={tr("会话聊天记录")}
        onClose={() => setSelected(null)}
        width={680}
      >
        <p className="muted" style={{ fontSize: 12 }}>
          {tr("会话编号 ")}
          {selected?.id}
        </p>
        <ErrorNotice error={error} />
        {loading ? (
          <Spin />
        ) : !messages.length ? (
          <EmptyData text={tr("该会话还没有消息")} />
        ) : (
          messages.map((item) => (
            <div
              className={`message-row ${item.sender_kind === "agent" ? "own" : ""} ${item.audience === "internal" ? "internal" : ""}`}
              key={item.id}
            >
              <div className="message-meta">
                {item.audience === "internal" ? tr("内部备注 \u00B7 ") : ""}
                {item.sender_kind === "visitor"
                  ? tr("访客")
                  : item.sender_kind === "system"
                    ? tr("系统")
                    : tr("客服")}{" "}
                ·{" "}
                {item.created_at
                  ? new Date(item.created_at).toLocaleString(currentPreferences().locale, {
                      timeZone: currentPreferences().timezone,
                    })
                  : ""}
              </div>
              <div className="message-bubble">
                {item.withdrawn_at ? (
                  tr("消息已撤回")
                ) : (
                  <>
                    {item.reply && (
                      <div className="form-hint">
                        {tr("引用：")}
                        {item.reply.withdrawn ? tr("消息已撤回") : item.reply.content}
                      </div>
                    )}
                    {item.content}
                    {item.attachment && (
                      <AttachmentView
                        attachment={item.attachment}
                        messageType={item.message_type}
                        identity={{
                          tenantId,
                          actorId:
                            current.memberships.find((member) => member.tenant_id === tenantId)
                              ?.id || "",
                          kind: "agent",
                        }}
                      />
                    )}
                  </>
                )}
              </div>
            </div>
          ))
        )}
      </Drawer>
    </section>
  );
}
