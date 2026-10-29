/** 文件作用说明：构建平台模板保存内容，防止改名或上下架时覆盖独立静态资源包。 */

/** platformTemplateDesign 保留已保存的平台包引用，只有内联模板才读取代码或组件表单。 */
export function platformTemplateDesign(
  mode: unknown,
  values: Record<string, unknown>,
  saved?: { platform_package_id?: string },
): unknown {
  if (saved?.platform_package_id) return { platform_package_id: saved.platform_package_id };
  if (mode === "code")
    return {
      html: values.html || "",
      css: values.css || "",
      javascript: values.javascript || "",
    };
  try {
    return JSON.parse(String(values.design_json));
  } catch {
    throw new Error("组件设计必须是合法 JSON。");
  }
}
