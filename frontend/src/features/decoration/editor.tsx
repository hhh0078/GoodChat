/** 文件作用说明：编辑结构化或代码装修，处理版本保存、组件排序及真实隔离预览发布回滚。 */
"use client";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import {
  Alert,
  App,
  Button,
  Card,
  Drawer,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Spin,
  Tabs,
  Tag,
} from "antd";
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  DeleteOutlined,
  EyeOutlined,
  PlusOutlined,
  SaveOutlined,
} from "@ant-design/icons";
import { safeRenderURL } from "@/lib/frame-bridge";
import { api, errorMessage } from "@/lib/api";
import { useSession } from "@/features/auth/session";
import { useResource } from "@/lib/use-resource";
import type { AccessLink, Resource } from "@/lib/types";
import type { Design, Template } from "./templates";
import { starterDesign } from "./templates";
import { ErrorNotice, PageTitle } from "@/components/page-state";
type Editable = {
  id: string;
  name: string;
  render_mode: "schema" | "code";
  draft: Design;
  version: number;
  current_release_id?: string;
};
const blockNames: Record<string, string> = {
  welcome: "欢迎区",
  announcement: "公告",
  faq: "常见问题",
  contact: "联系方式",
  chat: "聊天区域",
};

/** DecorationEditor 维护可回溯的装修草稿，只有明确发布才影响访客链接。 */
export function DecorationEditor({ id, kind }: { id: string; kind: "templates" | "pages" }) {
  const { tenantId } = useSession();
  const router = useRouter();
  const { message } = App.useApp();
  const [item, setItem] = useState<Editable | null>(null);
  const [design, setDesign] = useState<Design>(starterDesign());
  const [name, setName] = useState("");
  const [baseline, setBaseline] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [saving, setSaving] = useState(false);
  const [activeBlock, setActiveBlock] = useState("");
  const [dragged, setDragged] = useState("");
  const [previewURL, setPreviewURL] = useState("");
  const [publishOpen, setPublishOpen] = useState(false);
  const [linkIds, setLinkIds] = useState<string[]>([]);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [releases, setReleases] = useState<Resource[]>([]);
  const links = useResource<AccessLink>("/tenant/access-links");
  const dirty = !!item && JSON.stringify({ name, design }) !== baseline;
  const selected = design.blocks?.find((block) => block.id === activeBlock);
  /** load 读取最新服务端版本，不将编辑内容写回其他租户。 */
  async function load() {
    setError(null);
    try {
      let result: Editable;
      if (kind === "templates") {
        const data = await api<{ items: Template[] }>("/tenant/templates", { tenantId });
        const template = data.items.find((record) => record.id === id);
        if (!template) throw new Error("模板不存在或你没有访问权限。");
        result = { ...template, name: template.name || "", draft: template.design };
      } else result = await api<Editable>(`/tenant/pages/${id}`, { tenantId });
      setItem(result);
      setName(result.name);
      setDesign(result.draft || starterDesign());
      setBaseline(JSON.stringify({ name: result.name, design: result.draft || starterDesign() }));
    } catch (problem) {
      setError(problem);
    }
  }
  useEffect(() => {
    void load();
  }, [id, kind, tenantId]);
  useEffect(() => {
    if (!dirty) return;
    const prevent = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", prevent);
    return () => window.removeEventListener("beforeunload", prevent);
  }, [dirty]);
  /** updateDesign 在草稿中更新字段，不修改当前发布版本。 */
  function updateDesign(patch: Partial<Design>) {
    setDesign((previous) => ({ ...previous, ...patch }));
  }
  /** updateBlock 更新当前选中的结构化组件。 */
  function updateBlock(patch: Record<string, unknown>) {
    updateDesign({
      blocks: design.blocks?.map((block) =>
        block.id === activeBlock ? { ...block, ...patch } : block,
      ),
    });
  }
  /** moveBlock 按拖拽或键盘按钮调整组件顺序。 */
  function moveBlock(fromId: string, toIndex: number) {
    const blocks = [...(design.blocks || [])];
    const from = blocks.findIndex((block) => block.id === fromId);
    if (from < 0 || toIndex < 0 || toIndex >= blocks.length) return;
    const [block] = blocks.splice(from, 1);
    blocks.splice(toIndex, 0, block);
    updateDesign({ blocks });
  }
  /** save 将草稿和版本一起提交，冲突时保留用户当前编辑。 */
  async function save() {
    if (!item) return;
    setSaving(true);
    setError(null);
    try {
      const body = {
        name,
        render_mode: item.render_mode,
        version: item.version,
        ...(kind === "templates" ? { design } : { draft: design }),
      };
      await api(`/tenant/${kind}/${id}`, { method: "PATCH", tenantId, bodyJSON: body });
      message.success("草稿已保存");
      await load();
    } catch (problem) {
      setError(problem);
    } finally {
      setSaving(false);
    }
  }
  /** preview 取得后端隔离预览地址，拒绝同源代码注入。 */
  async function preview() {
    if (dirty) {
      message.warning("请先保存草稿，再预览服务端校验后的版本。");
      return;
    }
    try {
      if (kind === "templates") {
        const page = await api<Resource>("/tenant/pages", {
          method: "POST",
          tenantId,
          bodyJSON: { name: `${name} 页面`, template_id: id },
          idempotent: true,
        });
        router.push(`/console/pages/${page.id}`);
        return;
      }
      const result = await api<{ preview_url: string }>(`/tenant/pages/${id}/preview`, {
        method: "POST",
        tenantId,
        bodyJSON: {},
      });
      const url = safeRenderURL(result.preview_url, window.location.origin);
      if (!url) throw new Error("预览服务需要配置安全的独立来源，请检查隔离渲染地址。");
      setPreviewURL(url.toString());
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  /** publish 原子发布指定版本并绑定选定入口，不伪造发布结果。 */
  async function publish() {
    if (!item) return;
    setSaving(true);
    setError(null);
    try {
      await api(`/tenant/pages/${id}/publish`, {
        method: "POST",
        tenantId,
        bodyJSON: {
          version: item.version,
          expected_release_id: item.current_release_id || "",
          link_ids: linkIds,
        },
        idempotent: true,
      });
      message.success("页面已发布");
      setPublishOpen(false);
      await load();
    } catch (problem) {
      setError(problem);
    } finally {
      setSaving(false);
    }
  }
  /** showHistory 读取真实不可变发布记录。 */
  async function showHistory() {
    try {
      const data = await api<{ items: Resource[] }>(`/tenant/pages/${id}/releases`, { tenantId });
      setReleases(data.items || []);
      setHistoryOpen(true);
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  /** rollback 依据当前发布指针回滚，服务端校验并发冲突。 */
  async function rollback(release: Resource) {
    try {
      await api(`/tenant/pages/${id}/rollback`, {
        method: "POST",
        tenantId,
        bodyJSON: { release_id: release.id, expected_release_id: item?.current_release_id || "" },
        idempotent: true,
      });
      message.success("已回滚到选定版本");
      setHistoryOpen(false);
      await load();
    } catch (problem) {
      message.error(errorMessage(problem));
    }
  }
  if (!item)
    return (
      <>
        <ErrorNotice error={error} retry={() => void load()} />
        {!error && <Spin />}
      </>
    );
  return (
    <>
      <PageTitle
        title={kind === "templates" ? "编辑私有模板" : "页面装修与发布"}
        description={
          kind === "templates"
            ? "模板可创建多个独立页面，保存不会改变已有页面。"
            : "草稿与线上版本分开，预览校验通过后再发布到接入链接。"
        }
        action={
          <Space wrap>
            {dirty && <Tag color="gold">有未保存修改</Tag>}
            <Button
              icon={<SaveOutlined />}
              onClick={() => void save()}
              loading={saving}
              disabled={!dirty}
            >
              保存草稿
            </Button>
            <Button icon={<EyeOutlined />} onClick={() => void preview()}>
              {kind === "templates" ? "从模板创建页面" : "隔离预览"}
            </Button>
            {kind === "pages" && (
              <>
                <Button onClick={() => void showHistory()}>发布记录</Button>
                <Button
                  type="primary"
                  disabled={dirty}
                  onClick={() => {
                    setError(null);
                    setPublishOpen(true);
                  }}
                >
                  发布到入口
                </Button>
              </>
            )}
          </Space>
        }
      />
      <ErrorNotice error={error} />
      <div className="editor-grid">
        <Card
          title={
            <Input
              aria-label="设计名称"
              value={name}
              onChange={(event) => setName(event.target.value)}
              maxLength={100}
            />
          }
        >
          <Tag>{item.render_mode === "code" ? "代码装修" : "基础设置与可视化装修"}</Tag>
          <p className="form-hint">服务端草稿版本 {item.version}</p>
          {item.render_mode === "schema" ? (
            <Tabs
              items={[
                {
                  key: "brand",
                  label: "基础设置",
                  children: (
                    <Form layout="vertical">
                      <Form.Item label="客服页面标题">
                        <Input
                          value={design.brand?.title}
                          onChange={(event) =>
                            updateDesign({ brand: { ...design.brand, title: event.target.value } })
                          }
                        />
                      </Form.Item>
                      <Form.Item label="副标题与服务说明">
                        <Input
                          value={design.brand?.subtitle}
                          onChange={(event) =>
                            updateDesign({
                              brand: { ...design.brand, subtitle: event.target.value },
                            })
                          }
                        />
                      </Form.Item>
                      <Form.Item
                        label="Logo 地址"
                        extra="受控品牌素材尚待接入，当前隔离渲染不加载外链 Logo。"
                      >
                        <Input
                          disabled
                          value={design.brand?.logo_url}
                          onChange={(event) =>
                            updateDesign({
                              brand: { ...design.brand, logo_url: event.target.value },
                            })
                          }
                          placeholder="https://..."
                        />
                      </Form.Item>
                      <div className="two-columns">
                        <Form.Item label="主色">
                          <Input
                            value={design.theme?.primary_color}
                            onChange={(event) =>
                              updateDesign({
                                theme: { ...design.theme, primary_color: event.target.value },
                              })
                            }
                            placeholder="#23684e"
                          />
                        </Form.Item>
                        <Form.Item label="背景色">
                          <Input
                            value={design.theme?.background}
                            onChange={(event) =>
                              updateDesign({
                                theme: { ...design.theme, background: event.target.value },
                              })
                            }
                          />
                        </Form.Item>
                      </div>
                      <Form.Item label="圆角">
                        <InputNumber
                          min={0}
                          max={40}
                          value={design.theme?.radius}
                          onChange={(value) =>
                            updateDesign({ theme: { ...design.theme, radius: value || 0 } })
                          }
                        />
                      </Form.Item>
                    </Form>
                  ),
                },
                {
                  key: "blocks",
                  label: "组件布局",
                  children: (
                    <>
                      <p className="form-hint">拖拽卡片调整顺序，也可使用上下按钮。</p>
                      <Space orientation="vertical" style={{ width: "100%" }}>
                        {design.blocks?.map((block, index) => (
                          <div
                            key={block.id}
                            draggable
                            onDragStart={() => setDragged(block.id)}
                            onDragOver={(event) => event.preventDefault()}
                            onDrop={(event) => {
                              event.preventDefault();
                              moveBlock(dragged, index);
                              setDragged("");
                            }}
                          >
                            <Card
                              size="small"
                              style={{
                                borderColor: activeBlock === block.id ? "#23684e" : undefined,
                              }}
                            >
                              <Space style={{ justifyContent: "space-between", width: "100%" }}>
                                <Button type="text" onClick={() => setActiveBlock(block.id)}>
                                  {blockNames[block.type] || block.type}
                                </Button>
                                <Space size={2}>
                                  <Button
                                    size="small"
                                    icon={<ArrowUpOutlined />}
                                    aria-label="上移组件"
                                    disabled={index === 0}
                                    onClick={() => moveBlock(block.id, index - 1)}
                                  />
                                  <Button
                                    size="small"
                                    icon={<ArrowDownOutlined />}
                                    aria-label="下移组件"
                                    disabled={index === (design.blocks?.length || 0) - 1}
                                    onClick={() => moveBlock(block.id, index + 1)}
                                  />
                                  <Button
                                    size="small"
                                    danger
                                    icon={<DeleteOutlined />}
                                    aria-label="删除组件"
                                    disabled={block.type === "chat"}
                                    onClick={() =>
                                      updateDesign({
                                        blocks: design.blocks?.filter(
                                          (item) => item.id !== block.id,
                                        ),
                                      })
                                    }
                                  />
                                </Space>
                              </Space>
                            </Card>
                          </div>
                        ))}
                      </Space>
                      <Select<string>
                        style={{ width: "100%", margin: "16px 0" }}
                        value={undefined}
                        placeholder="添加组件"
                        options={Object.entries(blockNames)
                          .filter(([value]) => value !== "chat")
                          .map(([value, label]) => ({ value, label }))}
                        onChange={(value) => {
                          const newBlock = {
                            id: crypto.randomUUID(),
                            type: value,
                            title: blockNames[value],
                            content: "",
                            items: [],
                          };
                          updateDesign({ blocks: [...(design.blocks || []), newBlock] });
                          setActiveBlock(newBlock.id);
                        }}
                      />
                      {selected && (
                        <Form layout="vertical">
                          <Form.Item label="组件标题">
                            <Input
                              value={selected.title}
                              onChange={(event) => updateBlock({ title: event.target.value })}
                            />
                          </Form.Item>
                          <Form.Item label="文字内容">
                            <Input.TextArea
                              rows={4}
                              value={selected.content}
                              onChange={(event) => updateBlock({ content: event.target.value })}
                            />
                          </Form.Item>
                        </Form>
                      )}
                    </>
                  ),
                },
                {
                  key: "css",
                  label: "自定义 CSS",
                  children: (
                    <>
                      <Alert
                        type="info"
                        showIcon
                        title="样式只会在隔离装修页面内生效，不注入管理端。"
                      />
                      <Input.TextArea
                        className="code-editor"
                        rows={16}
                        value={design.custom_css}
                        onChange={(event) => updateDesign({ custom_css: event.target.value })}
                        style={{ marginTop: 16 }}
                      />
                    </>
                  ),
                },
              ]}
            />
          ) : (
            <Tabs
              items={[
                {
                  key: "html",
                  label: "HTML",
                  children: (
                    <Input.TextArea
                      aria-label="HTML 源码"
                      className="code-editor"
                      rows={22}
                      value={design.html || ""}
                      onChange={(event) => updateDesign({ html: event.target.value })}
                    />
                  ),
                },
                {
                  key: "css",
                  label: "CSS",
                  children: (
                    <Input.TextArea
                      aria-label="CSS 源码"
                      className="code-editor"
                      rows={22}
                      value={design.css || ""}
                      onChange={(event) => updateDesign({ css: event.target.value })}
                    />
                  ),
                },
                {
                  key: "js",
                  label: "JavaScript",
                  children: (
                    <Input.TextArea
                      aria-label="JavaScript 源码"
                      className="code-editor"
                      rows={22}
                      value={design.javascript || ""}
                      onChange={(event) => updateDesign({ javascript: event.target.value })}
                    />
                  ),
                },
              ]}
            />
          )}
        </Card>
        <div>
          <Card title="布局示意">
            <div
              className="design-preview"
              style={{
                background: /^#[\da-f]{6}$/i.test(design.theme?.background || "")
                  ? design.theme?.background
                  : undefined,
              }}
            >
              {item.render_mode === "code" ? (
                <Alert
                  type="info"
                  showIcon
                  title="完整代码不会在管理页面直接执行。保存草稿后，通过独立来源预览实际界面。"
                />
              ) : (
                <div
                  style={{
                    maxWidth: 390,
                    margin: "auto",
                    background: "#fff",
                    borderRadius: design.theme?.radius || 14,
                    overflow: "hidden",
                  }}
                >
                  <div
                    style={{
                      padding: 24,
                      background: /^#[\da-f]{6}$/i.test(design.theme?.primary_color || "")
                        ? design.theme?.primary_color
                        : "#23684e",
                      color: "#fff",
                    }}
                  >
                    <h3 style={{ marginBottom: 6, color: "inherit" }}>
                      {design.brand?.title || "在线客服"}
                    </h3>
                    <small>{design.brand?.subtitle}</small>
                  </div>
                  <div style={{ padding: 20 }}>
                    {design.blocks?.map((block) => (
                      <div key={block.id} style={{ marginBottom: 20 }}>
                        <strong>{block.title || blockNames[block.type]}</strong>
                        {block.type === "chat" ? (
                          <div className="preview-composer">消息输入区域 · 示意</div>
                        ) : (
                          <p className="muted" style={{ fontSize: 13, whiteSpace: "pre-wrap" }}>
                            {block.content || "尚未填写内容"}
                          </p>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
            <p className="form-hint">
              此处为布局示意，不使用真实聊天数据；自定义样式与代码以隔离预览结果为准。
            </p>
          </Card>
          <Alert
            type="info"
            title={
              kind === "templates"
                ? "保存后可反复创建多个页面。平台模板更新不会覆盖本租户作品。"
                : "发布之前请确认电脑与手机、排队和离线场景。回滚不会删除聊天记录。"
            }
            showIcon
            style={{ marginTop: 20 }}
          />
        </div>
      </div>
      <Modal
        title="隔离页面预览"
        open={!!previewURL}
        onCancel={() => setPreviewURL("")}
        footer={null}
        width={960}
        destroyOnHidden
      >
        {previewURL && (
          <iframe
            src={previewURL}
            title="隔离装修预览"
            sandbox="allow-scripts allow-same-origin"
            referrerPolicy="no-referrer"
            style={{ width: "100%", height: "70vh", border: "1px solid #dfe6dc", borderRadius: 12 }}
          />
        )}
      </Modal>
      <Drawer
        title="发布页面到接入链接"
        open={publishOpen}
        onClose={() => setPublishOpen(false)}
        width={480}
      >
        <ErrorNotice error={error} />
        <p>页面：{name}</p>
        <p className="muted">
          发布将创建不可变版本。选中链接随后使用此页面，正在咨询的访客仍保留原运行版本。
        </p>
        <Select
          mode="multiple"
          style={{ width: "100%", marginBottom: 20 }}
          placeholder="选择要绑定的入口"
          value={linkIds}
          onChange={setLinkIds}
          options={links.items.map((link) => ({
            label: link.name || link.entry_key,
            value: link.id,
          }))}
        />
        <Button type="primary" loading={saving} onClick={() => void publish()}>
          确认发布
        </Button>
      </Drawer>
      <Drawer
        title="发布与回滚记录"
        open={historyOpen}
        onClose={() => setHistoryOpen(false)}
        width={500}
      >
        <Space orientation="vertical" style={{ width: "100%" }}>
          {releases.map((release) => (
            <Card size="small" key={release.id}>
              <strong>发布版本 {String(release.version || release.id.slice(0, 8))}</strong>
              <p className="form-hint">
                {release.created_at
                  ? new Date(release.created_at).toLocaleString("zh-CN")
                  : release.id}
              </p>
              <Popconfirm
                title="将页面回滚到此版本？"
                description="会影响当前跟随此页面的入口，不删除消息。"
                onConfirm={() => rollback(release)}
              >
                <Button size="small" disabled={release.id === item.current_release_id}>
                  回滚到此版本
                </Button>
              </Popconfirm>
            </Card>
          ))}
        </Space>
      </Drawer>
    </>
  );
}
