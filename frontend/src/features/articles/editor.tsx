/** 文件作用说明：提供平台文章Markdown编辑、安全预览、保存及明确选择语言后发布。 */
"use client";
import { showRequestError } from "@/lib/request-error";
import { App, Button, Checkbox, Drawer, Form, Input, Select, Space, Tabs } from "antd";
import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { locales, languageNames } from "@/i18n/preferences";
import { useTranslator } from "@/i18n/use-translator";
import type { SiteArticle } from "./types";
import "./articles.css";
/** ArticleEditor 在每次打开时加载完整语言版本，服务端版本校验避免覆盖并发编辑。 */
export function ArticleEditor({
  id,
  onClose,
  onSaved,
}: {
  id: string | null | undefined;
  onClose: () => void;
  onSaved: () => void;
}) {
  const tr = useTranslator();
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [article, setArticle] = useState<SiteArticle | null>(null);
  const [busy, setBusy] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [html, setHTML] = useState("");
  const [targets, setTargets] = useState<string[]>([]);
  const [previewing, setPreviewing] = useState(false);
  const language = Form.useWatch("language", form) || "zh-CN";
  useEffect(() => {
    if (id === undefined) return;
    let cancelled = false;
    setLoaded(false);
    setHTML("");
    setTargets([]);
    setArticle(null);
    form.resetFields();
    if (!id) {
      form.setFieldsValue({
        language: "zh-CN",
        title: "",
        slug: "",
        summary: "",
        keywords: [],
        body_markdown: "",
      });
      setLoaded(true);
      return;
    }
    api<{ article: SiteArticle }>(`/platform/articles/${id}`)
      .then((data) => {
        if (!cancelled) {
          setArticle(data.article);
          form.setFieldsValue(data.article);
          setLoaded(true);
        }
      })
      .catch((e) => {
        if (!cancelled) showRequestError(e);
      });
    return () => {
      cancelled = true;
    };
  }, [id, form, message]);
  /** preview 只使用后端安全渲染结果，输入HTML不会直接进入页面。 */
  async function preview() {
    setPreviewing(true);
    try {
      const data = await api<{ body_html: string }>("/platform/articles/preview", {
        method: "POST",
        bodyJSON: { body_markdown: form.getFieldValue("body_markdown") || "" },
      });
      setHTML(data.body_html);
    } catch (e) {
      showRequestError(e);
    } finally {
      setPreviewing(false);
    }
  }
  /** save 保存Markdown后以最新版本发布，翻译失败会保留已发布原文和真实任务结果。 */
  async function save(publish: boolean) {
    setBusy(true);
    try {
      const values = await form.validateFields();
      const data = await api<{ article: SiteArticle }>(
        `/platform/articles${article ? `/${article.id}` : ""}`,
        {
          method: article ? "PATCH" : "POST",
          bodyJSON: { ...values, version: article?.version || 0, keywords: values.keywords || [] },
        },
      );
      setArticle(data.article);
      form.setFieldsValue(data.article);
      if (publish)
        await api(`/platform/articles/${data.article.id}/publish`, {
          method: "POST",
          bodyJSON: {
            version: data.article.version,
            languages: targets.filter((x) => x !== data.article.language),
          },
        });
      message.success(tr(publish ? "文章已发布，所选译文将在生成成功后显示。" : "文章已保存"));
      onSaved();
      onClose();
    } catch (e) {
      if (e instanceof Error) showRequestError(e);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Drawer
      title={tr(article ? "编辑文章" : "发文章")}
      open={id !== undefined}
      onClose={onClose}
      size={860}
      loading={!loaded}
      footer={
        <Space>
          <Button onClick={() => void save(false)} loading={busy} disabled={!loaded}>
            {tr("保存")}
          </Button>
          <Button type="primary" onClick={() => void save(true)} loading={busy} disabled={!loaded}>
            {tr("发布")}
          </Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical">
        <div className="article-editor-pair">
          <Form.Item name="language" label={tr("语言")} rules={[{ required: true }]}>
            <Select
              disabled={!!article?.published_at}
              options={locales.map((value) => ({ value, label: languageNames[value] }))}
            />
          </Form.Item>
          <Form.Item
            name="slug"
            label={tr("网址标识")}
            extra={tr("留空自动生成；发布后网址保持不变。")}
          >
            <Input disabled={!!article?.published_at} maxLength={120} />
          </Form.Item>
        </div>
        <Form.Item
          name="title"
          label={tr("文章标题")}
          rules={[{ required: true, whitespace: true }]}
        >
          <Input maxLength={160} showCount />
        </Form.Item>
        <Form.Item name="summary" label={tr("摘要")}>
          <Input.TextArea rows={2} maxLength={500} showCount />
        </Form.Item>
        <Form.Item name="keywords" label={tr("关键词")}>
          <Select mode="tags" tokenSeparators={[",", "，"]} maxCount={20} />
        </Form.Item>
        <Tabs
          onChange={(key) => {
            if (key === "preview") void preview();
          }}
          items={[
            {
              key: "edit",
              label: tr("Markdown 编辑"),
              children: (
                <Form.Item name="body_markdown" rules={[{ required: true, whitespace: true }]}>
                  <Input.TextArea
                    className="article-markdown-input"
                    rows={16}
                    maxLength={200000}
                    aria-label={tr("Markdown 正文")}
                  />
                </Form.Item>
              ),
            },
            {
              key: "preview",
              label: tr("预览"),
              children: (
                <div
                  aria-busy={previewing}
                  className="article-prose"
                  dangerouslySetInnerHTML={{ __html: html }}
                />
              ),
            },
          ]}
        />
        {!article?.source_id && (
          <Form.Item
            label={tr("发布时翻译的语言")}
            extra={tr("只生成勾选语言；每个语言版本独立发布，未完成译文不会对外展示。")}
          >
            <Checkbox.Group
              value={targets}
              onChange={(values) => setTargets(values.map(String))}
              options={locales
                .filter((x) => x !== language)
                .map((value) => ({ value, label: languageNames[value] }))}
            />
          </Form.Item>
        )}
      </Form>
    </Drawer>
  );
}
