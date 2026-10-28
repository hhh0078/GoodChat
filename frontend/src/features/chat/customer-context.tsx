/** 文件作用说明：以紧凑客户卡片展示真实资料、渠道、入口额外参数和跨会话备注，访问记录可折叠。 */
"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { Avatar, Button, Skeleton, Tag } from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import { api } from "@/lib/api";
import { ErrorNotice } from "@/components/page-state";
import type { Conversation, Membership, Resource } from "@/lib/types";
import { channelTags, customerFields, displayValue, recordValue } from "@/lib/workspace-display";
import { CustomerNotes } from "./customer-notes";

/** visitSource 展示真实访问来源，空渠道不占据时间线。 */
function visitSource(value: unknown): string {
  const tracking = recordValue(value);
  return [tracking.channel_code, tracking.utm_source, tracking.source]
    .filter((item): item is string => typeof item === "string" && !!item.trim())
    .join(" · ");
}
/** formatTime 将建档和访问时间压缩为次要文本，缺失时显示占位。 */
function formatTime(value: unknown): string {
  return typeof value === "string" && Number.isFinite(new Date(value).getTime())
    ? new Date(value).toLocaleString("zh-CN", {
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
      })
    : "—";
}
/** CustomerContext 在当前授权范围内加载客户，失权或请求失败立即清除缓存资料。 */
export function CustomerContext({
  conversation,
  membership,
}: {
  conversation: Conversation;
  membership: Membership;
}) {
  const manages = membership.role === "owner" || membership.role === "admin";
  const allowed =
    manages ||
    (["assigned", "active"].includes(conversation.status) &&
      conversation.current_agent_id === membership.id);
  const [contact, setContact] = useState<Resource | null>(null);
  const [visits, setVisits] = useState<Resource[]>([]);
  const [loading, setLoading] = useState(allowed);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    if (!allowed) return () => controller.abort();
    let pending = false;
    /** load 并行读取档案与访问记录，不把前一客户资料带到新会话。 */
    async function load() {
      if (pending) return;
      pending = true;
      try {
        const [detail, history] = await Promise.all([
          api<Resource>(`/tenant/contacts/${conversation.contact_id}`, {
            tenantId: membership.tenant_id,
            signal: controller.signal,
          }),
          api<{ items: Resource[] }>(`/tenant/contacts/${conversation.contact_id}/visits`, {
            tenantId: membership.tenant_id,
            signal: controller.signal,
          }),
        ]);
        if (controller.signal.aborted) return;
        setContact(detail);
        setVisits(Array.isArray(history?.items) ? history.items : []);
        setError(null);
      } catch (problem) {
        if (controller.signal.aborted) return;
        setContact(null);
        setVisits([]);
        setError(problem);
      } finally {
        pending = false;
        if (!controller.signal.aborted) setLoading(false);
      }
    }
    void load();
    const timer = setInterval(() => void load(), 10000);
    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [allowed, conversation.contact_id, membership.tenant_id, revision]);
  const name = String(contact?.display_name || conversation.contact?.display_name || "访客");
  const channels = channelTags(conversation.source_snapshot);
  const fields = Object.fromEntries(
    Object.entries(customerFields(contact?.attributes, conversation.context)).filter(
      ([key]) => key !== "page_url",
    ),
  );
  if (!allowed)
    return (
      <div className="customer-context">
        <h2>客户资料</h2>
        <p className="muted">详细资料仅向当前接待客服或管理员开放。</p>
      </div>
    );
  return (
    <div className="customer-context">
      <header>
        <h2>客户资料</h2>
        <Button
          type="text"
          size="small"
          aria-label="刷新客户资料"
          icon={<ReloadOutlined />}
          onClick={() => setRevision((value) => value + 1)}
        />
      </header>
      <ErrorNotice error={error} />
      {loading ? (
        <Skeleton active paragraph={{ rows: 5 }} />
      ) : (
        contact && (
          <>
            <div className="customer-identity">
              <Avatar
                size={40}
                src={
                  typeof contact.avatar_url === "string"
                    ? contact.avatar_url || undefined
                    : undefined
                }
              >
                {name.slice(0, 1)}
              </Avatar>
              <div>
                <strong>{name}</strong>
                <span className="customer-phone">{displayValue(contact.phone)}</span>
              </div>
            </div>
            <div className="customer-created">
              建档 {formatTime(contact.created_at)}
              {contact.blocked ? <Tag color="red">已封禁</Tag> : null}
            </div>
            {channels.length > 0 && (
              <div className="customer-channels">
                <span>渠道</span>
                {channels.map((channel) => (
                  <Tag key={channel}>{channel}</Tag>
                ))}
              </div>
            )}
            {Array.isArray(contact.tags) && contact.tags.length > 0 && (
              <div className="customer-tags">
                {contact.tags.map((tag: Resource) => (
                  <Tag key={tag.id}>{displayValue(tag.name)}</Tag>
                ))}
              </div>
            )}
            {Object.keys(fields).length > 0 && (
              <section>
                <h3>客户信息</h3>
                <dl className="customer-fields">
                  {Object.entries(fields).map(([key, value]) => (
                    <div className="customer-field-pair" key={key}>
                      <dt>{key}</dt>
                      <dd>{displayValue(value)}</dd>
                    </div>
                  ))}
                </dl>
              </section>
            )}
            {typeof contact.notes === "string" && contact.notes.trim() && (
              <p className="customer-note">{contact.notes}</p>
            )}
            {manages && (
              <Link className="customer-manage" href="/console/contacts">
                管理客户档案
              </Link>
            )}
            <CustomerNotes
              key={`${membership.tenant_id}.${conversation.contact_id}`}
              contactId={conversation.contact_id}
              tenantId={membership.tenant_id}
            />
            <details className="customer-visit-details">
              <summary>
                访问记录 <span>{visits.length} 次入口访问</span>
              </summary>
              {visits.length === 0 ? (
                <p className="muted">暂无访问记录</p>
              ) : (
                <ol className="customer-visits">
                  {visits.slice(0, 8).map((visit) => (
                    <li key={visit.id}>
                      <time>{formatTime(visit.created_at)}</time>
                      <strong>进入咨询</strong>
                      {visitSource(visit.tracking) && <span>{visitSource(visit.tracking)}</span>}
                    </li>
                  ))}
                </ol>
              )}
            </details>
          </>
        )
      )}
    </div>
  );
}
