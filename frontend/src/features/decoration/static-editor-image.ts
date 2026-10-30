/** 文件作用说明：在本地判断静态模板图片是否需要缩放压缩，保留小图、透明图与 GIF 原始内容。 */
import { editorImagePath, type EditorFile } from "./static-editor-model";

/** imageBase64 将有限大小的二进制图片编码为接口所需内容，不使用外部图片服务。 */
async function imageBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result).split(",")[1] || "");
    reader.onerror = () => reject(new Error("图片读取失败，请重新选择。"));
    reader.readAsDataURL(blob);
  });
}

/** prepareEditorImage 仅对大尺寸或大文件静态图片压缩，GIF 保留动画，服务端继续验证真实格式。 */
export async function prepareEditorImage(
  file: File,
): Promise<{ file: EditorFile; compressed: boolean }> {
  if (
    !["image/png", "image/jpeg", "image/webp", "image/gif"].includes(file.type) ||
    !file.size ||
    file.size > 10 * 1024 * 1024
  )
    throw new Error("请选择不超过 10MiB 的 PNG、JPEG、WebP 或 GIF 图片。");
  let result: Blob = file;
  if (file.type !== "image/gif") {
    const bitmap = await createImageBitmap(file);
    try {
      if (bitmap.width * bitmap.height > 40_000_000)
        throw new Error("图片像素过大，请使用较小图片。");
      if (Math.max(bitmap.width, bitmap.height) > 1920 || file.size > 650 * 1024) {
        const ratio = Math.min(1, 1920 / Math.max(bitmap.width, bitmap.height));
        const canvas = document.createElement("canvas");
        canvas.width = Math.max(1, Math.round(bitmap.width * ratio));
        canvas.height = Math.max(1, Math.round(bitmap.height * ratio));
        const context = canvas.getContext("2d");
        if (!context) throw new Error("浏览器无法处理图片，请使用较小的原图。");
        context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
        const compressed = await new Promise<Blob | null>((resolve) =>
          canvas.toBlob(resolve, file.type === "image/png" ? "image/png" : "image/webp", 0.84),
        );
        if (compressed && (ratio < 1 || compressed.size < file.size)) result = compressed;
      }
    } finally {
      bitmap.close();
    }
  }
  if (result.size > 5 * 1024 * 1024)
    throw new Error("处理后的图片仍超过 5MiB，请缩小图片；GIF 动画请先自行压缩。");
  const path = editorImagePath(result.type, crypto.randomUUID());
  return {
    file: {
      path,
      content_type: result.type,
      size: result.size,
      data_base64: await imageBase64(result),
      protected: false,
    },
    compressed: result !== file,
  };
}
