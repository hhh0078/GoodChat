/** 文件作用说明：在当前接待权限内显示客户档案、备注和真实入口访问记录，失权后清除资料。 */
"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { Avatar, Button, Skeleton, Tag } from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import { api } from "@/lib/api";
import { ErrorNotice } from "@/components/page-state";
import type { Conversation, Membership, Resource } from "@/lib/types";

/** contextValue 保留零、布尔和结构化资料，未填写时统一显示占位文字。 */
function contextValue(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—";
  return typeof value === "object" ? JSON.stringify(value) : String(value);
}

/** visitSource 只展示实际填写的来源信息，空渠道和技术JSON不占据访问时间线。 */
function visitSource(value: unknown): string {
  if (!value || typeof value !== "object") return "";
  const tracking = value as Record<string, unknown>;
  return [tracking.channel_code, tracking.utm_source, tracking.source]
    .filter((item): item is string => typeof item === "string" && !!item.trim())
    .join(" · ");
}

/** CustomerContext 随租户、接待对象或授权状态重新挂载，禁止复用前一会话的客户缓存。 */
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
    /** load 重新核验后端权限，失败时移除旧资料而非继续显示缓存。 */
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
  const name = conversation.contact?.display_name || "访客";
  if (!allowed)
    return (
      <div className="customer-context">
        <h2>客户资料</h2>
        <p className="muted">当前未接待此客户，详细资料仅向当前接待客服或管理员开放。</p>
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
                size={44}
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
                <span>客户档案</span>
              </div>
            </div>
            <div className="customer-tags">
              {Array.isArray(contact.tags) &&
                contact.tags.map((tag: Resource) => (
                  <Tag key={tag.id}>{contextValue(tag.name)}</Tag>
                ))}
            </div>
            <dl className="customer-fields">
              <dt>电话</dt>
              <dd>{contextValue(contact.phone)}</dd>
              <dt>建档时间</dt>
              <dd>
                {contact.created_at
                  ? new Date(contact.created_at).toLocaleString("zh-CN", {
                      month: "2-digit",
                      day: "2-digit",
                      hour: "2-digit",
                      minute: "2-digit",
                    })
                  : "—"}
              </dd>
              <dt>客户状态</dt>
              <dd>{contact.blocked ? "已封禁" : "正常"}</dd>
            </dl>
            <section>
              <h3>客户备注</h3>
              <p className="customer-note">
                {typeof contact.notes === "string" && contact.notes
                  ? contact.notes
                  : "暂无客户资料备注"}
              </p>
              {manages && (
                <Link className="customer-manage" href="/console/contacts">
                  管理客户档案
                </Link>
              )}
            </section>
            {contact.attributes &&
            typeof contact.attributes === "object" &&
            Object.keys(contact.attributes).length > 0 ? (
              <section>
                <h3>自定义资料</h3>
                <dl className="customer-fields">
                  {Object.entries(contact.attributes).map(([key, value]) => (
                    <div className="customer-field-pair" key={key}>
                      <dt>{key}</dt>
                      <dd>{contextValue(value)}</dd>
                    </div>
                  ))}
                </dl>
              </section>
            ) : null}
            <section>
              <h3>
                访问记录 <span>咨询入口访问</span>
              </h3>
              {visits.length === 0 ? (
                <p className="muted">暂无访问记录</p>
              ) : (
                <ol className="customer-visits">
                  {visits.slice(0, 8).map((visit) => (
                    <li key={visit.id}>
                      <time>
                        {visit.created_at
                          ? new Date(visit.created_at).toLocaleString("zh-CN", {
                              month: "2-digit",
                              day: "2-digit",
                              hour: "2-digit",
                              minute: "2-digit",
                            })
                          : "—"}
                      </time>
                      <strong>进入咨询</strong>
                      {visitSource(visit.tracking) ? (
                        <span>{visitSource(visit.tracking)}</span>
                      ) : null}
                    </li>
                  ))}
                </ol>
              )}
            </section>
            {conversation.context && Object.keys(conversation.context).length > 0 ? (
              <details className="customer-parameters">
                <summary>当前会话参数</summary>
                <pre>{JSON.stringify(conversation.context, null, 2)}</pre>
              </details>
            ) : null}
          </>
        )
      )}
    </div>
  );
}
