/** 文件作用说明：提供平台多语言文章列表、发布入口、每日生成设置与可核对的任务记录。 */
"use client";
import { showRequestError } from "@/lib/request-error";
import { App, Button, Input, Popconfirm, Select, Space, Tag } from "antd";
import { useState } from "react";
import { usePagedResource } from "@/lib/use-paged-resource";
import { useResource } from "@/lib/use-resource";
import { api } from "@/lib/api";
import { ErrorNotice, EmptyData } from "@/components/page-state";
import { LoadMore } from "@/components/load-more";
import { useTranslator } from "@/i18n/use-translator";
import { locales, languageNames, type Locale } from "@/i18n/preferences";
import { ArticleEditor } from "./editor";
import { ArticleSettingsDrawer } from "./settings";
import type { SiteArticle, ArticleJob } from "./types";
import "./articles.css";
/** ArticleManager 仅加载平台文章资源，列表筛选和分页均由服务端处理。 */
export function ArticleManager() {
  const tr = useTranslator();
  const { message } = App.useApp();
  const [query, setQuery] = useState("");
  const [language, setLanguage] = useState("");
  const [status, setStatus] = useState("");
  const [editing, setEditing] = useState<string | null | undefined>();
  const [section, setSection] = useState<"ai" | "search" | undefined>();
  const list = usePagedResource<SiteArticle>(
    "/platform/articles?" + new URLSearchParams({ query, language, status }),
    true,
    true,
  );
  const jobs = useResource<ArticleJob>("/platform/article-jobs", true);
  const statusNames: Record<string, string> = {
    draft: tr("草稿"),
    published: tr("已发布"),
    archived: tr("已下架"),
    queued: tr("等待处理"),
    running: tr("生成中"),
    succeeded: tr("已完成"),
    failed: tr("失败"),
    cancelled: tr("已取消"),
  };
  const kinds: Record<string, string> = {
    generate: tr("生成文章"),
    translate: tr("翻译文章"),
    submit: tr("搜索提交"),
  };
  /** refresh 重新读取文章及任务，反映后台翻译与提交的实际进度。 */
  function refresh() {
    list.reload();
    jobs.reload();
  }
  /** archive 下架所选语言版本，不删除其他语言或原始Markdown。 */
  async function archive(a: SiteArticle) {
    try {
      await api(`/platform/articles/${a.id}/archive`, {
        method: "POST",
        bodyJSON: { version: a.version },
      });
      refresh();
    } catch (e) {
      showRequestError(e);
    }
  }
  return (
    <div className="article-manager">
      <div className="article-manager-heading">
        <div>
          <h2>{tr("文章管理")}</h2>
          <p className="muted">{tr("按语言独立发布，官网仅展示当前语言的文章。")}</p>
        </div>
        <Space wrap>
          <Button onClick={() => setSection("search")}>{tr("搜索引擎设置")}</Button>
          <Button onClick={() => setSection("ai")}>{tr("AI定时文章")}</Button>
          <Button type="primary" onClick={() => setEditing(null)}>
            {tr("发文章")}
          </Button>
        </Space>
      </div>
      <div className="article-manager-filters">
        <Input.Search allowClear placeholder={tr("搜索文章标题")} onSearch={setQuery} />
        <Select
          aria-label={tr("语言")}
          value={language}
          onChange={setLanguage}
          options={[
            { value: "", label: tr("全部语言") },
            ...locales.map((value) => ({ value, label: languageNames[value] })),
          ]}
        />
        <Select
          aria-label={tr("状态")}
          value={status}
          onChange={setStatus}
          options={[
            { value: "", label: tr("全部状态") },
            ...Object.entries(statusNames)
              .filter(([value]) => ["draft", "published", "archived"].includes(value))
              .map(([value, label]) => ({ value, label })),
          ]}
        />
        <Button onClick={refresh}>{tr("刷新")}</Button>
      </div>
      <ErrorNotice error={list.error} retry={list.reload} />
      <div className="article-manager-list">
        {list.items.map((a) => (
          <article className="article-manager-row" key={a.id}>
            <div>
              <div className="article-row-title">
                <h3>{a.title}</h3>
                <Tag color={a.status === "published" ? "green" : "default"}>
                  {statusNames[a.status]}
                </Tag>
                <Tag>{languageNames[a.language as Locale] || a.language}</Tag>
              </div>
              <p>{a.summary || a.slug}</p>
              <small className="muted">{new Date(a.updated_at).toLocaleString()}</small>
            </div>
            <Space wrap>
              <Button onClick={() => setEditing(a.id)}>{tr("编辑")}</Button>
              {a.status === "published" && (
                <a href={`/${a.language}/articles/${a.slug}`} target="_blank" rel="noreferrer">
                  {tr("查看")}
                </a>
              )}
              {a.status !== "archived" && (
                <Popconfirm title={tr("下架此语言版本？")} onConfirm={() => archive(a)}>
                  <Button type="text" danger>
                    {tr("下架")}
                  </Button>
                </Popconfirm>
              )}
            </Space>
          </article>
        ))}
      </div>
      {!list.loading && !list.items.length && !list.error && <EmptyData />}
      <LoadMore
        loading={list.loading}
        hasMore={list.hasMore}
        error={list.error}
        onLoad={list.loadMore}
      />
      <details className="article-job-history">
        <summary>{tr("最近执行记录")}</summary>
        <p className="muted">{tr("自动生成、翻译和搜索提交的实际结果。刷新以查看最新状态。")}</p>
        <ErrorNotice error={jobs.error} retry={jobs.reload} />
        {jobs.items.map((j) => (
          <div className="article-job-row" key={j.id}>
            <span>
              {kinds[j.kind]} {j.provider}
            </span>
            <Tag
              color={j.status === "failed" ? "red" : j.status === "succeeded" ? "green" : "default"}
            >
              {statusNames[j.status] || j.status}
            </Tag>
            <span>
              {j.error_message ||
                j.result.title ||
                (j.result.submitted !== undefined
                  ? `${tr("已提交链接数")} ${j.result.submitted}`
                  : "")}
            </span>
            <time>{new Date(j.created_at).toLocaleString()}</time>
          </div>
        ))}
      </details>
      <ArticleEditor id={editing} onClose={() => setEditing(undefined)} onSaved={refresh} />
      <ArticleSettingsDrawer
        section={section}
        onClose={() => setSection(undefined)}
        onSaved={refresh}
      />
    </div>
  );
}
