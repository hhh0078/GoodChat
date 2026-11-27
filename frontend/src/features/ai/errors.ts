/** 文件作用说明：把安全AI错误分类转成用户可处理的本地化提示，不显示供应商原始响应。 */
import type { Translator } from "@/i18n/runtime";
/** aiErrorMessage 根据平台稳定错误分类提示余额、授权、时段或供应商问题。 */
export function aiErrorMessage(code: string, tr: Translator): string {
  const messages: Record<string, string> = {
    ai_insufficient_balance: "AI余额不足",
    ai_daily_limit: "AI每日额度已用完",
    ai_model_unavailable: "AI模型暂不可用",
    ai_provider_rejected: "AI模型暂不可用",
    ai_provider_unavailable: "AI模型暂不可用",
    ai_provider_uncertain: "AI用量需要核对",
    ai_stale: "内容已更新，请刷新后重试",
    ai_context_limit: "已选目录过长，请减少勾选数量",
    ai_images_not_supported: "当前模型不支持访客图片识别",
    ai_image_unavailable: "图片无法识别，请重新上传清晰图片",
  };
  return tr(messages[code] || "生成失败");
}
