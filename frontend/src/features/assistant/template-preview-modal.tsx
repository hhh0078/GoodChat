/** 文件作用说明：按需请求模板的短期隔离预览，统一行业、私有和平台模板的预览入口，不创建业务副本。 */
"use client";
import { useEffect, useState } from "react";
import { Button, Modal, Spin } from "antd";
import { EyeOutlined } from "@ant-design/icons";
import { api } from "@/lib/api";
import { ErrorNotice } from "@/components/page-state";
import { AssistantPreview } from "./preview";
import "./assistant.css";

type PreviewTarget = { path: string; name: string; tenantId?: string };

/** TemplatePreviewModal 仅在打开时取得短期快照，关闭或切换来源时取消未完成请求。 */
export function TemplatePreviewModal({
  path,
  name,
  tenantId,
  open,
  onClose,
}: PreviewTarget & { open: boolean; onClose: () => void }) {
  const [url, setURL] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    setURL("");
    setError(null);
    setLoading(true);
    api<{ preview_url: string }>(path, {
      method: "POST",
      tenantId,
      signal: controller.signal,
    })
      .then((result) => {
        if (controller.signal.aborted) return;
        if (!result?.preview_url) throw new Error("服务端没有返回隔离预览地址，请重试。");
        setURL(result.preview_url);
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [open, path, tenantId, revision]);
  return (
    <Modal
      rootClassName="template-preview-modal"
      title={`模板预览 · ${name || "未命名模板"}`}
      open={open}
      onCancel={onClose}
      footer={null}
      width={1120}
      style={{ top: 24 }}
      destroyOnHidden
    >
      {loading ? (
        <div className="template-preview-loading">
          <Spin />
          <p>正在准备隔离预览</p>
        </div>
      ) : error ? (
        <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      ) : url ? (
        <AssistantPreview key={url} url={url} />
      ) : null}
    </Modal>
  );
}

/** TemplatePreviewButton 为每个已保存模板提供直接预览，尚未点击时不发请求或生成快照。 */
export function TemplatePreviewButton({ path, name, tenantId }: PreviewTarget) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button size="small" icon={<EyeOutlined />} onClick={() => setOpen(true)}>
        预览
      </Button>
      <TemplatePreviewModal
        path={path}
        name={name}
        tenantId={tenantId}
        open={open}
        onClose={() => setOpen(false)}
      />
    </>
  );
}
