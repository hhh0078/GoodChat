/** 文件作用说明：将链接模板表单规范化，清空绑定时明确恢复现有默认用户端页面。 */
export type LinkTemplateBinding = {
  page_id: string | null;
  binding_mode: "FOLLOW_PAGE" | "PIN_RELEASE";
  pinned_release_id: string | null;
};

/** linkTemplateBinding 规范化绑定字段，避免清空选项后遗留旧页面或固定版本。 */
export function linkTemplateBinding(values: Record<string, unknown>): LinkTemplateBinding {
  const page = typeof values.page_id === "string" ? values.page_id.trim() : "";
  if (!page) return { page_id: null, binding_mode: "FOLLOW_PAGE", pinned_release_id: null };
  const fixed = values.binding_mode === "PIN_RELEASE";
  return {
    page_id: page,
    binding_mode: fixed ? "PIN_RELEASE" : "FOLLOW_PAGE",
    pinned_release_id:
      fixed && typeof values.pinned_release_id === "string" ? values.pinned_release_id : null,
  };
}
