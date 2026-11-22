/** 文件作用说明：用文件夹树组织共享知识，文档继续使用原有正文编辑、发布与历史版本流程。 */
"use client";
import { showRequestError } from "@/lib/request-error";
import { useEffect, useState } from "react";
import { App, Button, Form, Input, Modal, Popconfirm, Select, Tag, Tree } from "antd";
import { FolderOutlined, FileTextOutlined, PlusOutlined } from "@ant-design/icons";
import { useSession } from "@/features/auth/session";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import { EmptyData, ErrorNotice } from "@/components/page-state";
import { AIKnowledgeEditor } from "./content";
import { buildKnowledgeTree, knowledgeFolderPath, type AIFolder } from "./knowledge-tree-data";
import type { AIContent } from "./types";
import "./knowledge-tree.css";

/** AIFolderEditor 使用乐观版本保存目录名及父目录，循环和跨租户归属由后端再次验证。 */
function AIFolderEditor({
  item,
  parent,
  folders,
  onClose,
  onSaved,
}: {
  item: AIFolder | null;
  parent: string;
  folders: AIFolder[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const tr = useTranslator();
  const { tenantId } = useSession();
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false);
  /** save 保存目录组织信息，不调用模型或修改任何知识正文。 */
  async function save() {
    try {
      const values = await form.validateFields();
      setBusy(true);
      await api(`/tenant/ai/knowledge-folders${item ? `/${item.id}` : ""}`, {
        tenantId,
        method: item ? "PATCH" : "POST",
        bodyJSON: { ...values, version: item?.version || 0 },
      });
      onSaved();
      onClose();
    } catch (error) {
      if (!(error && typeof error === "object" && "errorFields" in error)) showRequestError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      open
      title={item ? tr("编辑目录") : tr("新建目录")}
      onCancel={busy ? undefined : onClose}
      onOk={() => void save()}
      confirmLoading={busy}
    >
      <Form
        form={form}
        layout="vertical"
        initialValues={{ name: item?.name || "", parent_id: item?.parent_id || parent }}
      >
        <Form.Item
          name="name"
          label={tr("目录名称")}
          rules={[{ required: true, whitespace: true }]}
        >
          <Input maxLength={100} />
        </Form.Item>
        <Form.Item name="parent_id" label={tr("上级目录")}>
          <Select
            showSearch
            optionFilterProp="label"
            options={[
              { value: "", label: tr("根目录") },
              ...folders
                .filter((f) => f.id !== item?.id)
                .map((f) => ({ value: f.id, label: knowledgeFolderPath(f.id, folders) })),
            ]}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}

/** AIKnowledgeTree 所有操作位于同一卡片内，授权目录完整发送，正文仍由 Agent 按需读取。 */
export function AIKnowledgeTree() {
  const tr = useTranslator();
  const { current, tenantId } = useSession();
  const { message } = App.useApp();
  const docs = useResource<AIContent>("/tenant/ai/knowledge");
  const folders = useResource<AIFolder>("/tenant/ai/knowledge-folders");
  const manages = ["owner", "admin"].includes(
    current.memberships.find((m) => m.tenant_id === tenantId)?.role || "",
  );
  const [selected, setSelected] = useState("root");
  const [search, setSearch] = useState("");
  const [editor, setEditor] = useState<{ item: AIContent | null } | null>(null);
  const [folderEditor, setFolderEditor] = useState<{ item: AIFolder | null } | null>(null);
  const document = docs.items.find((d) => `k:${d.id}` === selected);
  const folder = folders.items.find((f) => `f:${f.id}` === selected);
  const folderID = folder?.id || document?.folder_id || "";
  const tree = buildKnowledgeTree(folders.items, docs.items, search);
  useEffect(() => {
    if (!docs.items.some((d) => d.status === "pending")) return;
    const timer = setInterval(docs.reload, 2500);
    return () => clearInterval(timer);
  }, [docs.items, docs.reload]);
  /** refresh 同步刷新目录和文档，避免移动后的旧列表残留。 */
  function refresh() {
    docs.reload();
    folders.reload();
  }
  /** remove 文档沿用历史保留规则，目录只允许删除空节点。 */
  async function remove(kind: "knowledge" | "knowledge-folders", id: string) {
    try {
      await api(`/tenant/ai/${kind}/${id}`, { tenantId, method: "DELETE" });
      setSelected("root");
      refresh();
    } catch (error) {
      showRequestError(error);
    }
  }
  const visible = docs.items.filter(
    (d) =>
      (selected === "root" || d.folder_id === folderID) &&
      `${d.title} ${d.description}`.toLowerCase().includes(search.toLowerCase()),
  );
  return (
    <div className="ai-resource">
      <div className="ai-toolbar">
        <Input
          allowClear
          aria-label={tr("搜索知识或目录")}
          placeholder={tr("搜索知识或目录")}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <div className="ai-knowledge-toolbar-actions">
          <Button onClick={refresh}>{tr("刷新")}</Button>
          {manages && (
            <Button icon={<FolderOutlined />} onClick={() => setFolderEditor({ item: null })}>
              {tr("新建目录")}
            </Button>
          )}
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setEditor({ item: null })}>
            {tr("新增知识库")}
          </Button>
        </div>
      </div>
      <p className="ai-help">
        {tr("每次 AI 客服调用都携带授权范围内的完整树形目录，正文按需读取。")}
      </p>
      <ErrorNotice error={docs.error || folders.error} retry={refresh} />
      <div className="ai-knowledge-layout">
        <nav className="ai-knowledge-tree" aria-label={tr("知识目录")}>
          <Tree
            blockNode
            showIcon
            defaultExpandAll
            selectedKeys={[selected]}
            key={
              search +
              folders.items.map((f) => f.id).join(":") +
              docs.items.map((d) => d.id).join(":")
            }
            treeData={[
              { key: "root", title: tr("全部知识"), icon: <FolderOutlined />, children: tree },
            ]}
            onSelect={(keys) => {
              if (keys[0]) setSelected(String(keys[0]));
            }}
            icon={(node) => (node.isLeaf ? <FileTextOutlined /> : <FolderOutlined />)}
          />
        </nav>
        <section className="ai-knowledge-main">
          <header className="ai-knowledge-heading">
            <div>
              <h3>{document?.title || folder?.name || tr("全部知识")}</h3>
              <p>{knowledgeFolderPath(folderID, folders.items) || tr("根目录")}</p>
            </div>
            {folder && manages && !document && (
              <div className="ai-knowledge-toolbar-actions">
                <Button size="small" onClick={() => setFolderEditor({ item: folder })}>
                  {tr("编辑目录")}
                </Button>
                <Popconfirm
                  title={tr("确认删除空目录？")}
                  onConfirm={() => remove("knowledge-folders", folder.id)}
                >
                  <Button size="small" danger>
                    {tr("删除")}
                  </Button>
                </Popconfirm>
              </div>
            )}
            {document?.editable && (
              <Button size="small" onClick={() => setEditor({ item: document })}>
                {tr("编辑")}
              </Button>
            )}
          </header>
          {document ? (
            <>
              <div className="ai-knowledge-document-meta">
                <Tag>
                  {document.status === "ready"
                    ? tr("已发布")
                    : document.status === "failed"
                      ? tr("生成失败")
                      : tr("生成中")}
                </Tag>
                <Tag>{document.enabled ? tr("启用") : tr("停用")}</Tag>
              </div>
              <p className="ai-help">{document.description || tr("等待AI生成描述")}</p>
              <pre className="ai-knowledge-body">
                {document.published_body || document.draft_body}
              </pre>
              {(document.editable || manages) && (
                <Popconfirm
                  title={tr("确认移除？历史消息会保留。")}
                  onConfirm={() => remove("knowledge", document.id)}
                >
                  <Button danger>{tr("删除")}</Button>
                </Popconfirm>
              )}
            </>
          ) : visible.length ? (
            <div className="ai-knowledge-documents">
              {visible.map((d) => (
                <article key={d.id}>
                  <button
                    type="button"
                    className="ai-knowledge-document-link"
                    onClick={() => setSelected(`k:${d.id}`)}
                  >
                    <FileTextOutlined />
                    <span>
                      <strong>{d.title}</strong>
                      <small>{d.description || tr("等待AI生成描述")}</small>
                    </span>
                  </button>
                  <Tag>
                    {d.status === "ready"
                      ? tr("已发布")
                      : d.status === "failed"
                        ? tr("生成失败")
                        : tr("生成中")}
                  </Tag>
                  {d.editable && (
                    <Button size="small" onClick={() => setEditor({ item: d })}>
                      {tr("编辑")}
                    </Button>
                  )}
                </article>
              ))}
            </div>
          ) : (
            !docs.loading && !folders.loading && <EmptyData />
          )}
        </section>
      </div>
      {editor && (
        <AIKnowledgeEditor
          item={editor.item}
          defaultFolder={folderID}
          onClose={() => setEditor(null)}
          onSaved={() => {
            setEditor(null);
            refresh();
          }}
        />
      )}
      {folderEditor && (
        <AIFolderEditor
          item={folderEditor.item}
          parent={folderID}
          folders={folders.items}
          onClose={() => setFolderEditor(null)}
          onSaved={refresh}
        />
      )}
    </div>
  );
}
