/** 文件作用说明：让管理员集中处理 AI 知识缺口，发布补充知识后由服务端绑定机器人并完成待办。 */
"use client";
import { showRequestError } from "@/lib/request-error";
import { useEffect, useState } from "react";
import { Alert, App, Button, Form, Input, Modal, Popconfirm, Select, Tag, TreeSelect } from "antd";
import { useSession } from "@/features/auth/session";
import { useTranslator } from "@/i18n/use-translator";
import { useFormats } from "@/i18n/use-formats";
import { api } from "@/lib/api";
import { usePagedResource } from "@/lib/use-paged-resource";
import { useResource } from "@/lib/use-resource";
import { EmptyData, ErrorNotice } from "@/components/page-state";
import { LoadMore } from "@/components/load-more";
import { AITraceDetail } from "./traces";
import {
  knowledgeDocumentSelectTree,
  knowledgeFolderSelectTree,
  type AIFolder,
} from "./knowledge-tree-data";
import type { AIContent } from "./types";
import "./operations.css";

type Gap = {
  id: string;
  question: string;
  reason: string;
  status: string;
  occurrences: number;
  created_at: string;
  updated_at: string;
  last_job_id: string;
  knowledge_id?: string;
  knowledge_title: string;
  knowledge_status: string;
  knowledge_error: string;
  read_knowledge: { id: string; title: string }[];
};

/** AIGapSupplement 只选择本人可编辑知识；展示将追加的真实问题及答复，不修改他人的知识正文。 */
function AIGapSupplement({
  item,
  onClose,
  onSaved,
}: {
  item: Gap;
  onClose: () => void;
  onSaved: () => void;
}) {
  const tr = useTranslator();
  const { tenantId } = useSession();
  const { message } = App.useApp();
  const knowledge = useResource<AIContent>("/tenant/ai/knowledge");
  const folders = useResource<AIFolder>("/tenant/ai/knowledge-folders");
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const selected = Form.useWatch("knowledge_id", form);
  /** save 提交补充后等待服务端异步发布，发布成功前不向用户宣称知识已生效。 */
  async function save() {
    const values = await form.validateFields();
    setBusy(true);
    try {
      await api(`/tenant/ai/inbox/${item.id}`, {
        tenantId,
        method: "POST",
        bodyJSON: {
          ...values,
          action: "supplement",
          version: knowledge.items.find((k) => k.id === values.knowledge_id)?.version || 0,
        },
      });
      message.success(tr("已提交补充，发布成功后自动完成待办。"));
      onSaved();
      onClose();
    } catch (error) {
      showRequestError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      open
      title={tr("补充知识")}
      onCancel={onClose}
      onOk={() => void save()}
      confirmLoading={busy}
      okText={tr("保存并发布")}
      destroyOnHidden
    >
      <p>
        <strong>{item.question}</strong>
      </p>
      <p className="muted">{item.reason}</p>
      <ErrorNotice
        error={knowledge.error || folders.error}
        retry={() => {
          knowledge.reload();
          folders.reload();
        }}
      />
      <Form
        form={form}
        layout="vertical"
        initialValues={{ knowledge_id: "", folder_id: "", title: item.question.slice(0, 80) }}
      >
        <Form.Item name="knowledge_id" label={tr("补充到知识库")}>
          <TreeSelect
            showSearch
            treeLine
            treeNodeFilterProp="title"
            loading={knowledge.loading || folders.loading}
            treeData={[
              { value: "", title: tr("新建知识库") },
              ...knowledgeDocumentSelectTree(
                folders.items,
                knowledge.items.filter((k) => k.editable && k.enabled),
              ),
            ]}
          />
        </Form.Item>
        {!selected && (
          <Form.Item name="folder_id" label={tr("所属目录")}>
            <TreeSelect
              showSearch
              treeLine
              treeNodeFilterProp="title"
              loading={folders.loading}
              treeData={[
                { value: "", title: tr("根目录") },
                ...knowledgeFolderSelectTree(folders.items),
              ]}
            />
          </Form.Item>
        )}
        {!selected && (
          <Form.Item name="title" label={tr("标题")} rules={[{ required: true }]}>
            <Input maxLength={100} />
          </Form.Item>
        )}
        <Form.Item
          name="answer"
          label={tr("补充的准确答复")}
          rules={[{ required: true, whitespace: true }]}
        >
          <Input.TextArea rows={6} maxLength={4000} />
        </Form.Item>
      </Form>
      <Alert
        type="info"
        showIcon
        title={tr("发布成功后，对应机器人下次可读取此知识；不会向历史访客自动补发。")}
      />
    </Modal>
  );
}

/** AIInbox 将缺口、命中次数与来源集中在同一卡片内，失败可重试，忽略可恢复。 */
export function AIInbox() {
  const tr = useTranslator();
  const { dateTimeFormatter } = useFormats();
  const { message } = App.useApp();
  const { tenantId } = useSession();
  const [status, setStatus] = useState("pending");
  const [q, setQ] = useState("");
  const [selected, setSelected] = useState<Gap | null>(null);
  const [trace, setTrace] = useState("");
  const [busy, setBusy] = useState("");
  const resource = usePagedResource<
    Gap,
    { summary: { pending: number; resolved: number; ignored: number } }
  >(`/tenant/ai/inbox?${new URLSearchParams({ q, status })}`);
  const publishing = resource.items.some((item) => item.status === "supplementing");
  useEffect(() => {
    if (!publishing) return;
    const timer = setInterval(resource.reload, 5000);
    return () => clearInterval(timer);
  }, [publishing, resource.reload]);
  /** act 由服务端验证处理权限和发布状态，失败保留当前记录。 */
  async function act(item: Gap, action: string) {
    setBusy(item.id);
    try {
      await api(`/tenant/ai/inbox/${item.id}`, { tenantId, method: "POST", bodyJSON: { action } });
      resource.reload();
    } catch (error) {
      showRequestError(error);
    } finally {
      setBusy("");
    }
  }
  const statuses: Record<string, string> = {
    pending: tr("待处理"),
    supplementing: tr("正在发布"),
    resolved: tr("已补充"),
    ignored: tr("已忽略"),
  };
  return (
    <div className="ai-operations">
      <header className="ai-operations-heading">
        <div>
          <h2>{tr("待处理")}</h2>
          <p>{tr("把 AI 无法回答的问题补充为可用知识。")}</p>
        </div>
        <Button onClick={resource.reload}>{tr("刷新")}</Button>
      </header>
      <div className="ai-inbox-summary">
        {["pending", "resolved", "ignored"].map((key) => (
          <button
            type="button"
            key={key}
            className={status === key ? "active" : ""}
            onClick={() => setStatus(key)}
          >
            <span>{statuses[key]}</span>
            <strong>{resource.data?.summary?.[key as "pending"] ?? "—"}</strong>
          </button>
        ))}
      </div>
      <div className="ai-operations-filters">
        <Input.Search allowClear placeholder={tr("搜索未解答问题")} onSearch={setQ} />
        <Select
          aria-label={tr("状态")}
          value={status}
          onChange={setStatus}
          options={[
            { value: "all", label: tr("全部状态") },
            ...["pending", "resolved", "ignored"].map((value) => ({
              value,
              label: statuses[value],
            })),
          ]}
        />
      </div>
      <ErrorNotice error={resource.error} retry={resource.reload} />
      {!resource.loading && !resource.error && !resource.items.length && (
        <EmptyData text={tr("暂无需要补充的知识。")} />
      )}
      <div className="ai-operation-list">
        {resource.items.map((item) => (
          <article className="ai-operation-row ai-gap-row" key={item.id}>
            <div className="ai-operation-content">
              <div className="ai-operation-title">
                <Tag color="blue">{tr("AI补充")}</Tag>
                <strong>{item.question}</strong>
                <Tag>{statuses[item.status]}</Tag>
              </div>
              <p>{item.reason}</p>
              <small>
                {tr("发现次数")} {item.occurrences} ·{" "}
                {dateTimeFormatter({ dateStyle: "short", timeStyle: "short" }).format(
                  new Date(item.updated_at),
                )}
              </small>
              {!!item.read_knowledge?.length && (
                <details>
                  <summary>{tr("已查阅的知识")}</summary>
                  <p>{item.read_knowledge.map((k) => k.title).join(" · ")}</p>
                </details>
              )}
              {item.knowledge_title && (
                <p className="muted">
                  {tr("补充到知识库")}：{item.knowledge_title}
                </p>
              )}
              {item.knowledge_error && item.status === "pending" && (
                <p role="alert">{tr("知识发布失败，请重试补充或到知识库检查。")}</p>
              )}
            </div>
            <div className="ai-operation-actions">
              <Button size="small" onClick={() => setTrace(item.last_job_id)}>
                {tr("查看链路")}
              </Button>
              {item.status === "pending" && item.knowledge_status === "failed" && (
                <Button
                  size="small"
                  loading={busy === item.id}
                  onClick={() => void act(item, "retry")}
                >
                  {tr("重试发布")}
                </Button>
              )}
              {item.status === "pending" && (
                <>
                  <Button size="small" type="primary" onClick={() => setSelected(item)}>
                    {tr("补充")}
                  </Button>
                  <Button
                    size="small"
                    loading={busy === item.id}
                    onClick={() => void act(item, "ignore")}
                  >
                    {tr("忽略")}
                  </Button>
                </>
              )}
              {item.status === "ignored" && (
                <Button size="small" onClick={() => void act(item, "reopen")}>
                  {tr("恢复待处理")}
                </Button>
              )}
              {item.status !== "supplementing" && (
                <Popconfirm
                  title={tr("删除此待办？相同问题将不再重复新增。")}
                  onConfirm={() => act(item, "delete")}
                >
                  <Button type="text" danger size="small">
                    {tr("删除")}
                  </Button>
                </Popconfirm>
              )}
            </div>
          </article>
        ))}
      </div>
      <LoadMore
        loading={resource.loading}
        hasMore={resource.hasMore}
        error={resource.error}
        onLoad={resource.loadMore}
      />
      {selected && (
        <AIGapSupplement
          item={selected}
          onClose={() => setSelected(null)}
          onSaved={resource.reload}
        />
      )}
      {trace && <AITraceDetail key={trace} id={trace} onClose={() => setTrace("")} />}
    </div>
  );
}
