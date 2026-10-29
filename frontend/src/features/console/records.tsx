/** 文件作用说明：独立查询授权聊天记录，并按相同筛选分别下载会话元数据或默认不含内部备注的正文CSV。 */
"use client";
import { useEffect, useRef, useState, type FormEvent } from "react";
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
import { DownloadOutlined } from "@ant-design/icons";
import dayjs from "dayjs";
import { useResource } from "@/lib/use-resource";
import { api, errorMessage } from "@/lib/api";
import { requestCSV, startFileDownload } from "@/lib/csv-download";
import { AttachmentView } from "@/features/chat/attachment";
import { ExportJobs } from "./export-jobs";
import { useSession } from "@/features/auth/session";
import type { ChatMessage, Conversation, Member } from "@/lib/types";
import { PageTitle, ErrorNotice, EmptyData } from "@/components/page-state";
import filterStyles from "@/components/filter-toolbar.module.css";
const statusText: Record<string, string> = {
  queued: "排队中",
  assigned: "待接单",
  active: "接待中",
  closed: "已结束",
};
/** RecordsPage 以独立会话查询历史，导出生成结果和浏览器实际保存分开反馈。 */
export function RecordsPage() {
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
        const data = await api<{ items: ChatMessage[] }>(
          `/conversations/${conversationId}/messages?limit=100`,
          { tenantId, signal: controller.signal },
        );
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
        includeMessages ? "GoodChat-消息正文.csv" : "GoodChat-会话列表.csv",
        controller.signal,
      );
      if (controller.signal.aborted) return;
      startFileDownload(file.blob, file.filename);
      message.success(
        `已发起${includeMessages ? "消息正文" : "会话列表"}下载${file.rows !== null ? `（${file.rows}条数据）` : ""}，请查看浏览器下载记录`,
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
        title="聊天记录"
        description="按独立会话检索授权历史；日期按租户时区解释，结束日期包含当天。"
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
                导出会话列表
              </Button>
              <Button
                icon={<DownloadOutlined />}
                disabled={
                  records.loading || !!records.error || !records.items.length || !!exporting
                }
                loading={exporting === "messages"}
                onClick={() => void exportRecords(true)}
              >
                导出消息正文
              </Button>
            </Space>
          )
        }
      />
      <div className="table-card">
        <form className={filterStyles.toolbar} aria-label="聊天记录筛选" onSubmit={searchRecords}>
          <div className={filterStyles.field} style={{ width: 150 }}>
            <span>客户</span>
            <Input
              placeholder="名称 / 电话"
              aria-label="客户名称或电话"
              variant="borderless"
              allowClear
              maxLength={200}
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
          </div>
          <div className={filterStyles.field} style={{ width: 116 }}>
            <span>状态</span>
            <Select
              aria-label="会话状态"
              variant="borderless"
              allowClear
              placeholder="全部"
              value={status || undefined}
              options={Object.entries(statusText).map(([value, label]) => ({ value, label }))}
              onChange={(value) => setStatus(value || "")}
            />
          </div>
          <div className={`${filterStyles.field} ${filterStyles.date}`} style={{ width: 264 }}>
            <span>日期</span>
            <DatePicker.RangePicker
              aria-label="会话日期"
              variant="borderless"
              placeholder={["开始", "结束"]}
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
              <span>客服</span>
              <Select
                aria-label="接待客服"
                variant="borderless"
                allowClear
                showSearch
                optionFilterProp="label"
                placeholder="全部"
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
            <span>渠道</span>
            <Input
              placeholder="来源编码"
              aria-label="渠道来源编码"
              variant="borderless"
              allowClear
              value={channel}
              onChange={(event) => setChannel(event.target.value)}
            />
          </div>
          <div className={filterStyles.actions}>
            <Button type="primary" htmlType="submit">
              搜索
            </Button>
            <Button onClick={resetFilters}>重置</Button>
          </div>
        </form>
        {manages && (
          <p className="form-hint">
            两个导出动作都沿用当前筛选。正文默认不含内部备注，最多 10000
            条数据；超限请缩小范围。浏览器发起下载不代表文件已保存到磁盘。
          </p>
        )}
        <ErrorNotice error={exportError} />
        <ErrorNotice error={records.error} retry={records.reload} />
        <Table
          rowKey="id"
          dataSource={records.items}
          loading={records.loading}
          scroll={{ x: 850 }}
          locale={{ emptyText: <EmptyData text="没有符合条件的会话" /> }}
          columns={[
            {
              title: "客户",
              render: (_, row) =>
                row.contact?.display_name ||
                row.contact_name ||
                row.display_name ||
                row.contact_id?.slice(0, 8) ||
                "访客",
            },
            {
              title: "接待客服",
              render: (_, row) =>
                row.agent_name ? (
                  <Space size={8}>
                    <Avatar size={28} src={row.agent_avatar_url || undefined}>
                      {String(row.agent_name).slice(0, 1)}
                    </Avatar>
                    <span>{row.agent_name}</span>
                  </Space>
                ) : (
                  <span className="muted">尚未分配</span>
                ),
            },
            { title: "会话编号", dataIndex: "id", ellipsis: true, className: "console-record-id" },
            {
              title: "状态",
              dataIndex: "status",
              render: (value) => <Tag>{statusText[value] || value}</Tag>,
            },
            {
              title: "创建时间",
              dataIndex: "created_at",
              render: (value) => (value ? new Date(String(value)).toLocaleString("zh-CN") : "—"),
            },
            {
              title: "操作",
              render: (_, row) => (
                <Button type="link" size="small" onClick={() => setSelected(row)}>
                  查看聊天记录
                </Button>
              ),
            },
          ]}
        />
      </div>
      <Drawer open={!!selected} title="会话聊天记录" onClose={() => setSelected(null)} width={680}>
        <p className="muted" style={{ fontSize: 12 }}>
          会话编号 {selected?.id}
        </p>
        <ErrorNotice error={error} />
        {loading ? (
          <Spin />
        ) : !messages.length ? (
          <EmptyData text="该会话还没有消息" />
        ) : (
          messages.map((item) => (
            <div
              className={`message-row ${item.sender_kind === "agent" ? "own" : ""} ${item.audience === "internal" ? "internal" : ""}`}
              key={item.id}
            >
              <div className="message-meta">
                {item.audience === "internal" ? "内部备注 · " : ""}
                {item.sender_kind === "visitor"
                  ? "访客"
                  : item.sender_kind === "system"
                    ? "系统"
                    : "客服"}{" "}
                · {item.created_at ? new Date(item.created_at).toLocaleString("zh-CN") : ""}
              </div>
              <div className="message-bubble">
                {item.withdrawn_at ? (
                  "消息已撤回"
                ) : (
                  <>
                    {item.reply && (
                      <div className="form-hint">
                        引用：{item.reply.withdrawn ? "消息已撤回" : item.reply.content}
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
