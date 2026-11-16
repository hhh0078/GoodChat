/** 文件作用说明：定义管理员 AI 助手的接口、权限和任务状态，不保存密钥或聊天凭证。 */
export type AssistantSource = "tenant" | "platform";
export type AssistantRequest = {
  templateId?: string;
  templateName?: string;
  threadId?: string;
  source?: AssistantSource;
  prompt?: string;
  view?: "conversation" | "configuration";
  nonce?: number;
};
export type AssistantConfiguration = {
  base_url: string;
  model: string;
  enabled: boolean;
  has_api_key: boolean;
  image_base_url: string;
  image_model: string;
  has_image_api_key: boolean;
  configured: boolean;
  version: number;
};
export type AssistantAsset = {
  id: string;
  data_url: string;
  content_type: string;
  width: number;
  height: number;
  size: number;
  compressed: boolean;
};
export type AssistantJob = {
  id: string;
  status: string;
  phase?: string;
  template_id?: string;
  page_id?: string;
  preview_url?: string;
  error_message?: string;
  error_message_key?: string;
  error_message_params?: Record<string, string | number>;
  billing_status?: string;
  applied?: boolean;
  design?: Record<string, unknown>;
  image_tasks?: {
    id: string;
    path: string;
    status: string;
    error_message?: string;
    error_message_key?: string;
    error_message_params?: Record<string, string | number>;
  }[];
  result?: {
    type?: string;
    text?: string;
    message?: string;
  };
};
export type AssistantMessage = {
  id: string;
  role: string;
  content: string;
  asset_ids?: string[];
  assets?: AssistantAsset[];
  job_id?: string;
  created_at?: string;
};
export type AssistantThread = {
  id: string;
  title: string;
  source: AssistantSource;
  template_id?: string;
  page_id?: string;
  preview_url?: string;
  created_at?: string;
  updated_at?: string;
  messages?: AssistantMessage[];
  active_job?: AssistantJob | null;
  latest_job?: AssistantJob | null;
};
/** canUseAssistant 只允许平台管理员及当前租户的所有者、管理员显示助手入口。 */
export function canUseAssistant(platformAdmin: boolean, role?: string): boolean {
  return platformAdmin || role === "owner" || role === "admin";
}
/** activeAssistantJob 判断任务是否仍需要轮询，未知状态也保留为待确认。 */
export function activeAssistantJob(job?: AssistantJob | null): boolean {
  return !!job && !["succeeded", "failed", "cancelled"].includes(job.status);
}
/** assistantJobLabel 将服务端阶段和状态转换为清楚的中文进度说明。 */
export function assistantJobLabel(job: AssistantJob): string {
  if (job.status === "succeeded")
    return job.design
      ? job.applied === false
        ? tr("候选模板已保留")
        : tr("模板已生成")
      : tr("AI 已回复");
  if (job.status === "failed") return tr("生成未完成");
  if (job.status === "cancelled") return tr("任务已取消");
  if (job.status === "queued") return tr("等待开始");
  if (job.status === "validating" || job.phase === "package") return tr("校验并打包模板");
  if (job.phase === "images") return tr("正在生成与处理图片");
  return tr("正在生成页面");
}
/** assistantAssetProblem 在上传前限制图片类型和数量，不依赖文件名后缀。 */
export function assistantAssetProblem(file: Pick<File, "type" | "size">, count: number): string {
  if (count >= 4) return tr("每次最多添加 4 张参考图片");
  if (!["image/png", "image/jpeg", "image/webp", "image/gif"].includes(file.type))
    return tr("请选择 PNG、JPEG、WebP 或 GIF 图片");
  if (file.size === 0 || file.size > 10 * 1024 * 1024)
    return tr("单张参考图片不能为空或超过 10MiB");
  return "";
}

import { translate as tr } from "../../i18n/runtime.ts";
