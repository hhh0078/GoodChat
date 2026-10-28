/** 文件作用说明：上传并应用后端真实解码重编码的品牌Logo，素材只进入当前草稿。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { App, Button, Space, Upload } from "antd";
import { api } from "@/lib/api";
import { useSession } from "@/features/auth/session";
import { ErrorNotice } from "@/components/page-state";
/** BrandLogo 只接受受控PNG结果，异步完成时使用最新草稿回调以保留其他品牌编辑。 */
export function BrandLogo({
  value,
  onChange,
  disabled = false,
}: {
  value?: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const { tenantId } = useSession();
  const latestChange = useRef(onChange);
  latestChange.current = onChange;
  const controller = useRef<AbortController | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const { message } = App.useApp();
  useEffect(() => {
    setError(null);
    setBusy(false);
    return () => controller.current?.abort();
  }, [tenantId]);
  /** upload 将真实图片交服务端重编码，只有有效受控结果才修改Logo草稿。 */
  async function upload(file: File) {
    setError(null);
    if (!file.size || file.size > 5 * 1024 * 1024) {
      setError(new Error("请选择不超过5MiB的PNG、JPEG、GIF或WebP图片"));
      return;
    }
    const request = new AbortController();
    controller.current = request;
    setBusy(true);
    try {
      const body = new FormData();
      body.append("file", file);
      const result = await api<{
        data_url: string;
        content_type: string;
        width: number;
        height: number;
        size: number;
      }>("/tenant/decoration-assets", { method: "POST", body, tenantId, signal: request.signal });
      if (request.signal.aborted) return;
      if (
        result.content_type !== "image/png" ||
        !result.data_url.startsWith("data:image/png;base64,")
      )
        throw new Error("品牌素材未返回有效受控图片");
      latestChange.current(result.data_url);
      message.success("Logo已加入当前草稿，保存后可预览");
    } catch (problem) {
      if (!request.signal.aborted) setError(problem);
    } finally {
      if (!request.signal.aborted) setBusy(false);
    }
  }
  return (
    <>
      <Space wrap>
        {value?.startsWith("data:image/png;base64,") && (
          <img
            src={value}
            alt="当前品牌Logo"
            style={{
              width: 64,
              height: 64,
              objectFit: "contain",
              border: "1px solid var(--line)",
              borderRadius: 8,
            }}
          />
        )}
        <Upload
          accept="image/png,image/jpeg,image/gif,image/webp"
          showUploadList={false}
          beforeUpload={(file) => {
            void upload(file);
            return false;
          }}
        >
          <Button loading={busy} disabled={disabled || busy}>
            上传Logo
          </Button>
        </Upload>
        {!!value && (
          <Button disabled={disabled || busy} onClick={() => onChange("")}>
            移除Logo
          </Button>
        )}
      </Space>
      <p className="form-hint">
        最大5MiB、400万像素；处理后以PNG保存，最长边512像素。外部图片地址不会自动加载。
      </p>
      <ErrorNotice error={error} />
    </>
  );
}
