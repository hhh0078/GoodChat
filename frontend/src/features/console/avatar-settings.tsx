/** 文件作用说明：提供租户访客头像池与当前客服固定头像设置，复用平台库和受控上传资源。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { App, Avatar, Button, Checkbox, Select, Spin, Upload } from "antd";
import { CheckOutlined, UploadOutlined } from "@ant-design/icons";
import { api, errorMessage } from "@/lib/api";
import { useSession } from "@/features/auth/session";
import { ErrorNotice } from "@/components/page-state";
import {
  selectedAvatarReferences,
  toggleAvatarReference,
  type AvatarAsset,
} from "@/lib/avatar-selection";
const categories = [
  { value: "all", label: "全部头像" },
  { value: "professional", label: "职业西装" },
  { value: "cartoon", label: "卡通头像" },
  { value: "illustration", label: "萌趣头像" },
  { value: "uploaded", label: "本租户上传" },
];

/** AvatarSettings 保存租户头像池或个人头像，失败时保留本次选择供重试。 */
export function AvatarSettings({ mode }: { mode: "pool" | "agent" }) {
  const { tenantId, reload } = useSession();
  const { message } = App.useApp();
  const [assets, setAssets] = useState<AvatarAsset[]>([]);
  const [refs, setRefs] = useState<string[]>([]);
  const [usingDefaults, setUsingDefaults] = useState(mode === "pool");
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const [category, setCategory] = useState("all");
  const [loading, setLoading] = useState(true);
  const [loaded, setLoaded] = useState(false);
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  const path = mode === "pool" ? "/tenant/avatar-pool" : "/agent/avatar";
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setLoaded(false);
    setError(null);
    setAssets([]);
    setRefs([]);
    Promise.all([
      api<{ items: AvatarAsset[] }>("/tenant/avatar-assets", {
        tenantId,
        signal: controller.signal,
      }),
      api<{ avatar_refs?: string[]; avatar_ref?: string; using_defaults?: boolean }>(path, {
        tenantId,
        signal: controller.signal,
      }),
    ])
      .then(([library, settings]) => {
        if (controller.signal.aborted) return;
        setLoaded(true);
        setAssets(library.items || []);
        setUsingDefaults(
          mode === "pool" && (settings.using_defaults === true || !settings.avatar_refs?.length),
        );
        setRefs(
          mode === "pool"
            ? settings.avatar_refs || []
            : settings.avatar_ref
              ? [settings.avatar_ref]
              : [],
        );
      })
      .catch((problem) => {
        if (!controller.signal.aborted) setError(problem);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [tenantId, path, mode, revision]);
  const selected = mode === "pool" && usingDefaults ? selectedAvatarReferences([], assets) : refs;
  const preview = assets.find((asset) => asset.id === refs[0]);
  /** save 将所选资源编号交服务端核验，不把图片路径当作可任意写入的配置。 */
  async function save() {
    setSaving(true);
    setError(null);
    try {
      await api(path, {
        tenantId,
        method: "PATCH",
        bodyJSON:
          mode === "pool"
            ? { avatar_refs: usingDefaults ? [] : refs }
            : { avatar_ref: refs[0] || "" },
      });
      if (mode === "agent") await reload();
      if (!mounted.current) return;
      message.success(mode === "pool" ? "访客头像池已保存" : "客服头像已保存");
    } catch (problem) {
      if (mounted.current) setError(problem);
    } finally {
      if (mounted.current) setSaving(false);
    }
  }
  /** upload 只上传有界图片到头像资源接口，成功后加入选择但仍等待明确保存。 */
  async function upload(file: File) {
    if (!file.size || file.size > 5 * 1024 * 1024) {
      message.error("请选择不超过5MiB的PNG、JPEG、GIF或WebP图片");
      return;
    }
    setUploading(true);
    setError(null);
    try {
      const body = new FormData();
      body.append("file", file);
      const asset = await api<AvatarAsset>("/tenant/avatar-assets", {
        tenantId,
        method: "POST",
        body,
      });
      if (!mounted.current) return;
      setUsingDefaults(false);
      setAssets((previous) => [...previous.filter((item) => item.id !== asset.id), asset]);
      setRefs(mode === "pool" ? [...new Set([...selected, asset.id])] : [asset.id]);
      setCategory("uploaded");
      message.success("头像已上传，保存设置后生效");
    } catch (problem) {
      if (mounted.current) setError(problem);
    } finally {
      if (mounted.current) setUploading(false);
    }
  }
  return (
    <section
      className="avatar-settings"
      aria-label={mode === "pool" ? "访客头像池设置" : "客服头像设置"}
    >
      <header>
        <div>
          <h2>{mode === "pool" ? "访客头像池" : "我的客服头像"}</h2>
          <p className="muted">
            {mode === "pool"
              ? "从平台库选择或上传头像。无头像访客首次获得一个随机头像，保存到客户档案后保持不变。"
              : "为当前工作空间设置固定客服头像，在账号菜单、成员列表和聊天中显示。"}
          </p>
        </div>
        {mode === "agent" && (
          <Avatar size={56} src={preview?.url}>
            我
          </Avatar>
        )}
      </header>
      <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />
      {loading ? (
        <Spin />
      ) : (
        <>
          <div className="avatar-picker-toolbar">
            <Select
              aria-label="头像分类"
              value={category}
              onChange={setCategory}
              options={categories}
              style={{ width: 150 }}
            />
            <Upload
              accept="image/png,image/jpeg,image/gif,image/webp"
              showUploadList={false}
              beforeUpload={(file) => {
                void upload(file);
                return false;
              }}
            >
              <Button icon={<UploadOutlined />} loading={uploading} disabled={saving || !loaded}>
                上传头像
              </Button>
            </Upload>
            <span className="muted">
              {mode === "pool"
                ? `已选 ${selected.length} 张${usingDefaults ? " · 平台默认库" : ""}`
                : "点击选择一张头像"}
            </span>
          </div>
          {mode === "pool" && (
            <Checkbox
              checked={assets
                .filter((asset) => asset.builtin)
                .every((asset) => selected.includes(asset.id))}
              onChange={(event) => {
                setUsingDefaults(false);
                setRefs(
                  event.target.checked
                    ? [
                        ...new Set([
                          ...selected,
                          ...assets.filter((asset) => asset.builtin).map((asset) => asset.id),
                        ]),
                      ]
                    : selected.filter((id) => !assets.find((asset) => asset.id === id)?.builtin),
                );
              }}
            >
              选择全部平台头像
            </Checkbox>
          )}
          <div className="avatar-picker-grid">
            {assets
              .filter((asset) => category === "all" || asset.category === category)
              .map((asset) => (
                <button
                  type="button"
                  key={asset.id}
                  className={`avatar-choice ${selected.includes(asset.id) ? "is-selected" : ""}`}
                  aria-label={`选择头像 ${asset.name}`}
                  aria-pressed={selected.includes(asset.id)}
                  title={asset.name}
                  disabled={saving || uploading}
                  onClick={() => {
                    setUsingDefaults(false);
                    setRefs(toggleAvatarReference(selected, asset.id, mode === "pool"));
                  }}
                >
                  <Avatar size={56} src={asset.url}>
                    {asset.name.slice(0, 1)}
                  </Avatar>
                  {selected.includes(asset.id) && <CheckOutlined className="avatar-choice-check" />}
                  <span>{asset.name}</span>
                </button>
              ))}
          </div>
          <div className="avatar-picker-actions">
            <Button
              type="primary"
              loading={saving}
              disabled={
                !loaded || uploading || !assets.length || (mode === "pool" && !selected.length)
              }
              onClick={() => void save()}
            >
              {mode === "pool" ? "保存访客头像池" : "保存客服头像"}
            </Button>
            <Button
              disabled={saving || uploading}
              onClick={() => {
                setRefs([]);
                setUsingDefaults(mode === "pool");
              }}
            >
              {mode === "pool" ? "恢复平台默认库" : "清除客服头像"}
            </Button>
          </div>
          <p className="form-hint">
            头像用于公开展示。支持PNG、JPEG、GIF和WebP，最大5MiB。上传图片经服务端校验并转为受控PNG。
            {mode === "pool"
              ? "空配置使用平台默认50张；已有明确头像的客户不会被随机覆盖。"
              : "上传后请点击保存，头像不会自动变化。"}
          </p>
        </>
      )}
    </section>
  );
}
