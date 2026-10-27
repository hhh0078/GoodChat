/** 文件作用说明：导入私有静态ZIP、浏览文件树、修订文本为新资源包并应用到真实装修草稿。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import {
  Alert,
  App,
  Button,
  Card,
  Drawer,
  Form,
  Image,
  Input,
  Modal,
  Popconfirm,
  Space,
  Spin,
  Table,
  Tag,
  Tree,
} from "antd";
import { api, ApiError } from "@/lib/api";
import { exportFilename, startFileDownload } from "@/lib/csv-download";
import { buildPackageTree } from "@/lib/package-tree";
import { useResource } from "@/lib/use-resource";
import { EmptyData, ErrorNotice } from "@/components/page-state";
import type { Resource } from "@/lib/types";
type CodePackage = Resource & {
  entrypoint: string;
  file_count: number;
  total_bytes: number;
  manifest: { path: string; content_type: string; size: number; sha256: string }[];
};
type PackageResult = { package: CodePackage; design: { package_id: string } };
type PackageFile = {
  path: string;
  content_type: string;
  size: number;
  content?: string;
  data_url?: string;
};
/** CodePackages 保留资源包的私有归属与不可变版本，导入不会自动发布访客页面。 */
export function CodePackages({
  activeId,
  onUse,
}: {
  activeId?: string;
  onUse?: (id: string) => void;
}) {
  const data = useResource<CodePackage>("/tenant/code-packages");
  const activeTenant = useRef(data.tenantId);
  activeTenant.current = data.tenantId;
  const { message, modal } = App.useApp();
  const router = useRouter();
  const [selected, setSelected] = useState<CodePackage | null>(null);
  const [selectedPath, setSelectedPath] = useState("");
  const [file, setFile] = useState<PackageFile | null>(null);
  const [text, setText] = useState("");
  const [fileLoading, setFileLoading] = useState(false);
  const [fileError, setFileError] = useState<unknown>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [zip, setZIP] = useState<File | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [packageName, setPackageName] = useState("");
  const [form] = Form.useForm();
  const dirty = typeof file?.content === "string" && text !== file.content;
  useEffect(() => {
    setSelected(null);
    setSelectedPath("");
    setFile(null);
    setText("");
    setError(null);
    setImportOpen(false);
    setCreateOpen(false);
    setZIP(null);
    setBusy(false);
  }, [data.tenantId]);
  useEffect(() => {
    if (!dirty) return;
    /** prevent 提醒用户浏览器即将关闭，当前文本改动尚未另存。 */
    const prevent = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", prevent);
    return () => window.removeEventListener("beforeunload", prevent);
  }, [dirty]);
  useEffect(() => {
    setFile(null);
    setText("");
    setFileError(null);
    if (!selected || !selectedPath) return;
    const controller = new AbortController();
    setFileLoading(true);
    api<PackageFile>(
      `/tenant/code-packages/${selected.id}/files?${new URLSearchParams({ path: selectedPath })}`,
      { tenantId: data.tenantId, signal: controller.signal },
    )
      .then((value) => {
        if (!controller.signal.aborted) {
          setFile(value);
          setText(value.content || "");
        }
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setFileError(problem);
      })
      .finally(() => {
        if (!controller.signal.aborted) setFileLoading(false);
      });
    return () => controller.abort();
  }, [selected?.id, selectedPath, data.tenantId]);
  /** discardGuard 在用户确认后才切换资源，避免丢失尚未另存的文本改动。 */
  function discardGuard(action: () => void) {
    const tenant = data.tenantId;
    if (dirty)
      modal.confirm({
        title: "当前文本尚未另存为新资源包，确认放弃改动？",
        onOk: () => {
          if (activeTenant.current === tenant) action();
        },
      });
    else action();
  }
  /** choosePackage 读取指定私有包的文件清单，不执行任何租户代码。 */
  function choosePackage(item: CodePackage) {
    discardGuard(() => {
      setSelected(item);
      setSelectedPath(item.entrypoint);
      setPackageName(`${item.name || "资源包"} 修订`);
    });
  }
  /** importPackage 上传实际ZIP，只有完整校验通过后才显示导入成功。 */
  async function importPackage(values: Record<string, string>) {
    const tenant = data.tenantId;
    setBusy(true);
    setError(null);
    try {
      if (!zip || !zip.size || zip.size > 10 * 1024 * 1024)
        throw new Error("请选择不超过10MiB的静态ZIP资源包");
      const body = new FormData();
      body.append("file", zip);
      body.append("name", values.name);
      body.append("entrypoint", values.entrypoint || "index.html");
      const result = await api<PackageResult>("/tenant/code-packages", {
        method: "POST",
        body,
        tenantId: data.tenantId,
      });
      if (activeTenant.current !== tenant) return;
      data.reload();
      setImportOpen(false);
      setZIP(null);
      choosePackage(result.package);
      message.success("私有资源包已导入，尚未发布");
    } catch (problem) {
      if (activeTenant.current === tenant) setError(problem);
    } finally {
      if (activeTenant.current === tenant) setBusy(false);
    }
  }
  /** reviseFile 将当前文本变更保存为完整新包，原包与已发布快照保持不变。 */
  async function reviseFile() {
    if (!selected || !file || typeof file.content !== "string") return;
    const tenant = data.tenantId;
    setBusy(true);
    setFileError(null);
    try {
      if (new TextEncoder().encode(text).byteLength > 5 * 1024 * 1024)
        throw new Error("单个文本文件不能超过5MiB，请精简后重试");
      const result = await api<PackageResult>(`/tenant/code-packages/${selected.id}/revisions`, {
        method: "POST",
        tenantId: data.tenantId,
        bodyJSON: {
          name: packageName || `${selected.name} 修订`,
          entrypoint: selected.entrypoint,
          files: [{ path: file.path, content: text }],
        },
      });
      if (activeTenant.current !== tenant) return;
      setSelected(result.package);
      setPackageName(`${result.package.name} 修订`);
      data.reload();
      message.success("文本改动已另存为新资源包，请明确应用后保存装修草稿");
    } catch (problem) {
      if (activeTenant.current === tenant) setFileError(problem);
    } finally {
      if (activeTenant.current === tenant) setBusy(false);
    }
  }
  /** createTemplate 创建引用当前包的租户私有模板，不自动生成页面或发布。 */
  async function createTemplate(values: Record<string, string>) {
    if (!selected) return;
    const tenant = data.tenantId;
    setBusy(true);
    setError(null);
    try {
      const result = await api<Resource>("/tenant/templates", {
        method: "POST",
        tenantId: data.tenantId,
        bodyJSON: {
          name: values.name,
          render_mode: "code",
          design: { package_id: selected.id, html: "", css: "", javascript: "" },
        },
      });
      if (activeTenant.current !== tenant) return;
      setCreateOpen(false);
      router.push(`/console/templates/${result.id}`);
    } catch (problem) {
      if (activeTenant.current === tenant) setError(problem);
    } finally {
      if (activeTenant.current === tenant) setBusy(false);
    }
  }
  /** downloadExample 下载服务端生成的真实多文件SDK示例，失败不会冒充导出成功。 */
  async function downloadExample() {
    const tenant = data.tenantId;
    setBusy(true);
    setError(null);
    try {
      const response = await fetch("/api/v1/tenant/code-packages/example", {
        credentials: "same-origin",
        headers: { "X-Tenant-ID": data.tenantId },
        cache: "no-store",
        signal: AbortSignal.timeout(30000),
      });
      if (!response.ok) {
        const result = await response.json().catch(() => null);
        throw new ApiError(
          result?.message || "示例资源包下载失败",
          response.status,
          result?.code || "PACKAGE_DOWNLOAD_FAILED",
          result?.request_id,
        );
      }
      const blob = await response.blob();
      if (activeTenant.current !== tenant) return;
      if (!response.headers.get("Content-Type")?.includes("application/zip") || !blob.size)
        throw new Error("示例资源包没有返回有效ZIP文件");
      startFileDownload(
        blob,
        exportFilename(response.headers.get("Content-Disposition"), "goodchat-static-example.zip"),
      );
      message.success("示例ZIP下载已发起，请查看浏览器下载记录");
    } catch (problem) {
      if (activeTenant.current === tenant) setError(problem);
    } finally {
      if (activeTenant.current === tenant) setBusy(false);
    }
  }
  return (
    <div>
      <Alert
        type="info"
        showIcon
        title="导入已构建的静态网站，资源包仅本租户可用。"
        description="ZIP最大10MiB，展开20MiB，最多200项，单文件5MiB。支持本包HTML/CSS/JS、相对模块、图片及字体；导入不执行构建命令，不加载外部CDN或裸包依赖。"
        style={{ marginBottom: 16 }}
      />
      <Space wrap style={{ marginBottom: 16 }}>
        <Button
          type="primary"
          onClick={() => {
            setError(null);
            form.resetFields();
            form.setFieldsValue({ entrypoint: "index.html" });
            setZIP(null);
            setImportOpen(true);
          }}
        >
          导入ZIP资源包
        </Button>
        <Button loading={busy} onClick={() => void downloadExample()}>
          下载多文件SDK示例
        </Button>
        {activeId && <Tag>当前草稿已引用资源包</Tag>}
      </Space>
      <ErrorNotice error={error} />
      <ErrorNotice error={data.error} retry={data.reload} />
      <Table
        size="small"
        rowKey="id"
        dataSource={data.items}
        loading={data.loading}
        locale={{ emptyText: <EmptyData text="尚未导入私有资源包，可先下载示例查看目录组织。" /> }}
        columns={[
          { title: "名称", dataIndex: "name" },
          { title: "入口", dataIndex: "entrypoint" },
          { title: "文件数", dataIndex: "file_count" },
          {
            title: "大小",
            dataIndex: "total_bytes",
            render: (value) => `${(Number(value) / 1024).toFixed(1)} KiB`,
          },
          {
            title: "操作",
            render: (_, item) => (
              <Button type="link" onClick={() => choosePackage(item)}>
                {selected?.id === item.id ? "正在查看" : "查看文件"}
              </Button>
            ),
          },
        ]}
      />
      {selected && (
        <Card
          size="small"
          title={
            <>
              资源包 {selected.name} {activeId === selected.id && <Tag color="green">当前草稿</Tag>}
            </>
          }
          style={{ marginTop: 16 }}
          extra={
            onUse ? (
              <Popconfirm
                title="应用此包并清空当前HTML/CSS/JS入口覆盖？"
                description="只修改当前装修草稿，保存和发布仍需分别确认。"
                onConfirm={() => {
                  onUse(selected.id);
                  message.success("资源包已应用到当前草稿，请保存后预览");
                }}
              >
                <Button type="primary" disabled={dirty}>
                  应用到当前草稿
                </Button>
              </Popconfirm>
            ) : (
              <Button
                type="primary"
                disabled={dirty}
                onClick={() => {
                  setError(null);
                  setCreateOpen(true);
                }}
              >
                创建私有代码模板
              </Button>
            )
          }
        >
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "minmax(140px, 1fr) minmax(0, 3fr)",
              gap: 16,
            }}
          >
            <Tree
              treeData={buildPackageTree(selected.manifest || [])}
              defaultExpandAll
              selectedKeys={selectedPath ? [`file:${selectedPath}`] : []}
              onSelect={(keys, info) => {
                if (info.node.isLeaf && keys[0])
                  discardGuard(() => setSelectedPath(String(keys[0]).slice(5)));
              }}
            />
            <div>
              <ErrorNotice error={fileError} />
              {fileLoading ? (
                <Spin />
              ) : (
                file && (
                  <>
                    <p>
                      <strong>{file.path}</strong> · {file.size} 字节
                    </p>
                    {typeof file.content === "string" ? (
                      <>
                        <Input.TextArea
                          aria-label="资源包文件源码"
                          className="code-editor"
                          value={text}
                          onChange={(event) => setText(event.target.value)}
                          rows={18}
                        />
                        <Space wrap style={{ marginTop: 12 }}>
                          <Input
                            aria-label="修订资源包名称"
                            value={packageName}
                            onChange={(event) => setPackageName(event.target.value)}
                            maxLength={100}
                            style={{ width: 220 }}
                          />
                          <Button
                            type="primary"
                            loading={busy}
                            disabled={!dirty}
                            onClick={() => void reviseFile()}
                          >
                            另存为新资源包
                          </Button>
                          <Button
                            disabled={!dirty || busy}
                            onClick={() => setText(file.content || "")}
                          >
                            放弃当前文件改动
                          </Button>
                        </Space>
                        <p className="form-hint">
                          只保存当前文本文件的修改为新包。替换二进制素材请重新导入ZIP；旧包和旧发布不会被覆盖。
                        </p>
                      </>
                    ) : (
                      <>
                        {file.content_type.startsWith("image/") &&
                          file.data_url?.startsWith("data:image/") && (
                            <Image
                              src={file.data_url}
                              alt={file.path}
                              style={{ maxWidth: "100%", maxHeight: 260 }}
                            />
                          )}
                        <p className="muted">
                          此文件按二进制保存，可查看元数据；替换请重新导入ZIP。
                        </p>
                      </>
                    )}
                  </>
                )
              )}
            </div>
          </div>
        </Card>
      )}
      <Drawer
        title="导入私有静态资源包"
        open={importOpen}
        onClose={() => setImportOpen(false)}
        width={500}
        destroyOnHidden
      >
        <ErrorNotice error={error} />
        <Form form={form} layout="vertical" onFinish={importPackage}>
          <Form.Item name="name" label="资源包名称" rules={[{ required: true, whitespace: true }]}>
            <Input maxLength={100} />
          </Form.Item>
          <Form.Item
            name="entrypoint"
            label="HTML入口路径"
            extra="相对资源包根目录，默认index.html。"
          >
            <Input placeholder="index.html" maxLength={240} />
          </Form.Item>
          <Form.Item label="ZIP文件" required>
            <input
              type="file"
              aria-label="选择静态资源ZIP"
              accept=".zip,application/zip"
              onChange={(event) => setZIP(event.target.files?.[0] || null)}
            />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={busy} disabled={!zip}>
            校验并导入
          </Button>
        </Form>
      </Drawer>
      <Modal
        title="从资源包创建私有模板"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        footer={null}
        destroyOnHidden
      >
        <ErrorNotice error={error} />
        <Form layout="vertical" onFinish={createTemplate} initialValues={{ name: selected?.name }}>
          <Form.Item name="name" label="模板名称" rules={[{ required: true, whitespace: true }]}>
            <Input maxLength={100} />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>
            创建并编辑
          </Button>
        </Form>
      </Modal>
    </div>
  );
}
