/** 文件作用说明：定义AI装修当日提交次数，并区分模型未配置、平台禁用与次数用尽。 */
export type AIConfiguration = {
  configured: boolean;
  enabled: boolean;
  daily_limit: number;
  used_today: number;
  remaining_today: number;
  timezone: string;
};
/** aiSubmissionState 根据服务端状态决定能否提交，次数只代表任务提交次数。 */
export function aiSubmissionState(configuration: AIConfiguration | null) {
  if (!configuration)
    return { allowed: false, reason: "unknown", message: "请先读取 AI 装修配置与当日提交次数。" };
  if (!configuration.configured)
    return {
      allowed: false,
      reason: "unconfigured",
      message: "AI 模型尚未配置，请联系平台管理员。你仍可手工创建模板。",
    };
  if (!configuration.enabled || configuration.daily_limit === 0)
    return {
      allowed: false,
      reason: "disabled",
      message: "平台已停用 AI 装修。你仍可手工创建模板。",
    };
  if (configuration.remaining_today <= 0)
    return {
      allowed: false,
      reason: "exhausted",
      message: "今日 AI 装修提交次数已用完，请在租户本地日期更新后重试。",
    };
  return {
    allowed: true,
    reason: "ready",
    message: "描述页面需求，AI 生成后保存为本租户私有模板，预览发布由你决定。",
  };
}
