/** 文件作用说明：维护小篇幅知识库及其AI描述，允许在文本光标处插入稳定话术引用。 */
"use client";
import { showRequestError } from "@/lib/request-error";
import { ErrorNotice } from "@/components/page-state";
import { useSession } from "@/features/auth/session";
import { useTranslator } from "@/i18n/use-translator";
import { api } from "@/lib/api";
import { insertKnowledgeReference } from "@/lib/ai-content";
import { useResource } from "@/lib/use-resource";
import { PlusOutlined } from "@ant-design/icons";
import {
  App,
  Button,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  TreeSelect,
} from "antd";
import { useEffect, useRef, useState } from "react";
import type { AIContent } from "./types";
import { AIKnowledgeHistory } from "./revisions";
import { knowledgeFolderSelectTree, type AIFolder } from "./knowledge-tree-data";
import { AIScriptEditor } from "./script-editor";

/** AIContentList 为知识和话术显示真实状态及成员维护权限。 */
export function AIContentList({ kind }: { kind: "knowledge" | "scripts" }) {
  const tr = useTranslator();
  const { message } = App.useApp();
  const { tenantId, current } = useSession();
  const data = useResource<AIContent>(`/tenant/ai/${kind}`);
  const [editing, setEditing] = useState<AIContent | null>(null);
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const member = current.memberships.find((m) => m.tenant_id === tenantId);
  const manages = member?.role === "owner" || member?.role === "admin";
  useEffect(() => {
    if (kind !== "knowledge" || !data.items.some((item) => item.status === "pending")) return;
    const timer = setInterval(data.reload, 2500);
    return () => clearInterval(timer);
  }, [data.items, data.reload, kind]);
  /** remove 只移除后续可用资源，已发送消息和附件继续保留。 */
  async function remove(item: AIContent) {
    try {
      await api(`/tenant/ai/${kind}/${item.id}`, { method: "DELETE", tenantId });
      data.reload();
    } catch (error) {
      showRequestError(error);
    }
  }
  /** edit 在新弹窗生命周期内加载草稿，避免继承前一条内容。 */
  function edit(item: AIContent | null) {
    setEditing(item);
    setOpen(true);
  }
  const columns = [
    { title: tr("标题"), dataIndex: "title", width: 170 },
    {
      title: tr("使用描述"),
      dataIndex: "description",
      render: (value: string) => value || tr("等待AI生成描述"),
    },
    {
      title: tr("状态"),
      width: 150,
      render: (_: unknown, item: AIContent) => (
        <Space wrap>
          <Tag color={item.enabled ? "blue" : "default"}>
            {item.enabled ? tr("启用") : tr("停用")}
          </Tag>
          {kind === "knowledge" && (
            <Tag>
              {item.status === "ready"
                ? tr("已发布")
                : item.status === "failed"
                  ? tr("生成失败")
                  : tr("生成中")}
            </Tag>
          )}
        </Space>
      ),
    },
    {
      title: tr("操作"),
      width: 170,
      render: (_: unknown, item: AIContent) => (
        <Space>
          {item.editable && (
            <Button size="small" onClick={() => edit(item)}>
              {tr("编辑")}
            </Button>
          )}
          {(item.editable || manages) && (
            <Popconfirm title={tr("确认移除？历史消息会保留。")} onConfirm={() => remove(item)}>
              <Button size="small" danger>
                {item.editable ? tr("删除") : tr("停用")}
              </Button>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];
  return (
    <div className="ai-resource">
      <ErrorNotice error={data.error} retry={data.reload} />
      <div className="ai-toolbar">
        <Input
          aria-label={tr("搜索")}
          placeholder={tr("搜索")}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          allowClear
        />
        <Button type="primary" icon={<PlusOutlined />} onClick={() => edit(null)}>
          {kind === "knowledge" ? tr("新增知识库") : tr("新增AI话术")}
        </Button>
      </div>
      <p className="ai-help">
        {kind === "knowledge"
          ? tr(
              "保存正文后，AI自动生成使用描述；成功后与正文一起发布，旧发布版本在此期间继续使用。描述生成会产生AI用量。",
            )
          : tr("使用描述必填。所有成员可查看，只能编辑自己的话术；机器人只使用明确勾选的话术。")}
      </p>
      <Table
        rowKey="id"
        columns={columns}
        dataSource={data.items.filter((item) =>
          `${item.title} ${item.description}`.toLowerCase().includes(search.toLowerCase()),
        )}
        loading={data.loading}
        scroll={{ x: 650 }}
        pagination={{ pageSize: 10 }}
      />
      {open &&
        (kind === "knowledge" ? (
          <AIKnowledgeEditor
            item={editing}
            onClose={() => setOpen(false)}
            onSaved={() => {
              setOpen(false);
              data.reload();
            }}
          />
        ) : (
          <AIScriptEditor
            item={editing}
            onClose={() => setOpen(false)}
            onSaved={() => {
              setOpen(false);
              data.reload();
            }}
          />
        ))}
    </div>
  );
}

/** AIKnowledgeEditor 保留纯文本正文，光标插入的话术编号不扩大机器人授权。 */
export function AIKnowledgeEditor({
  item,
  defaultFolder = "",
  onClose,
  onSaved,
}: {
  item: AIContent | null;
  defaultFolder?: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const tr = useTranslator();
  const { message } = App.useApp();
  const { tenantId } = useSession();
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const scripts = useResource<AIContent>("/tenant/ai/scripts");
  const folders = useResource<AIFolder>("/tenant/ai/knowledge-folders");
  const selection = useRef({
    start: item?.draft_body?.length || 0,
    end: item?.draft_body?.length || 0,
  });
  /** save 保存正文版本，生成失败仍能在列表重新编辑和重试。 */
  async function save() {
    try {
      const values = await form.validateFields();
      setBusy(true);
      await api(`/tenant/ai/knowledge${item ? `/${item.id}` : ""}`, {
        method: item ? "PATCH" : "POST",
        tenantId,
        bodyJSON: {
          title: values.title,
          folder_id: values.folder_id || "",
          body: values.body,
          enabled: values.enabled,
          version: item?.version || 0,
        },
      });
      onSaved();
    } catch (error) {
      if (!(error && typeof error === "object" && "errorFields" in error)) showRequestError(error);
    } finally {
      setBusy(false);
    }
  }
  /** insert 将话术引用插入最后记录的光标位置。 */
  function insert(id: string) {
    const text = String(form.getFieldValue("body") || "");
    const next = insertKnowledgeReference(text, selection.current.start, selection.current.end, id);
    form.setFieldValue("body", next);
    const pos = selection.current.start + `[[script:${id}]]`.length;
    selection.current = { start: pos, end: pos };
  }
  return (
    <Modal
      open
      width={760}
      title={item ? tr("编辑知识库") : tr("新增知识库")}
      onCancel={busy ? undefined : onClose}
      onOk={() => void save()}
      confirmLoading={busy}
      okText={tr("保存并生成描述")}
      cancelText={tr("取消")}
    >
      {item && (
        <Button onClick={() => setHistoryOpen(true)} disabled={busy}>
          {tr("历史版本")}
        </Button>
      )}
      {item && historyOpen && (
        <AIKnowledgeHistory
          item={item}
          onClose={() => setHistoryOpen(false)}
          onRestored={onSaved}
        />
      )}
      <Form
        form={form}
        layout="vertical"
        initialValues={{
          title: item?.title || "",
          folder_id: item?.folder_id || defaultFolder,
          body: item?.draft_body || "",
          enabled: item?.enabled ?? true,
        }}
        disabled={busy}
      >
        <ErrorNotice error={folders.error} retry={folders.reload} />
        <Form.Item name="folder_id" label={tr("所属目录")}>
          <TreeSelect
            showSearch
            treeNodeFilterProp="title"
            treeDefaultExpandedKeys={[""]}
            loading={folders.loading}
            treeData={[
              {
                value: "",
                title: tr("根目录"),
                children: knowledgeFolderSelectTree(folders.items),
              },
            ]}
          />
        </Form.Item>
        <Form.Item name="title" label={tr("标题")} rules={[{ required: true, whitespace: true }]}>
          <Input maxLength={100} />
        </Form.Item>
        <Form.Item
          name="body"
          label={tr("知识库正文")}
          rules={[
            { required: true, whitespace: true },
            {
              validator: (_, value: string) =>
                new TextEncoder().encode(value || "").length <= 16000
                  ? Promise.resolve()
                  : Promise.reject(new Error(tr("知识库正文不能为空且不能超过16000字节"))),
            },
          ]}
        >
          <Input.TextArea
            rows={12}
            onSelect={(e) => {
              const node = e.currentTarget;
              selection.current = { start: node.selectionStart, end: node.selectionEnd };
            }}
          />
        </Form.Item>
        <div className="ai-editor-tools">
          <Select
            aria-label={tr("插入话术")}
            placeholder={tr("在光标处插入话术")}
            value={null}
            onChange={insert}
            options={scripts.items
              .filter((s) => s.enabled)
              .map((s) => ({ value: s.id, label: s.title }))}
          />
        </div>
        <p className="ai-help">{tr("插入引用后，还需在机器人设置中勾选对应话术。")}</p>
        {item?.description && (
          <Form.Item label={tr("已发布描述")}>
            <p>{item.description}</p>
          </Form.Item>
        )}
        {item?.status === "failed" && (
          <p role="alert">{tr("描述生成失败，请检查模型、余额或稍后重试。")}</p>
        )}
        <Form.Item name="enabled" label={tr("启用")} valuePropName="checked">
          <Switch />
        </Form.Item>
      </Form>
    </Modal>
  );
}
