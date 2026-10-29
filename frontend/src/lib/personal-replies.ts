/** 文件作用说明：定义客服个人自动回复及公共只读规则的接口类型与展示文案。 */
export type ReplySource = "personal" | "tenant" | "space" | "link" | "legacy";
export type WelcomeContent = {
  type: "text" | "image" | "options";
  content: string;
  attachment_id?: string;
  delay_seconds: number;
  options?: { id: string; label: string; value: string }[];
};
export type PersonalRule = {
  id: string;
  name: string;
  enabled: boolean;
  version: number;
  editable: boolean;
  source_scope: ReplySource;
};
export type PersonalWelcome = PersonalRule & {
  scope_type: string;
  scope_id?: string;
  scenario: "all" | "business" | "outside";
  contents: WelcomeContent[];
};
export type PersonalKeyword = PersonalRule & {
  match_mode: "contains" | "exact";
  pattern: string;
  reply: string;
  priority: number;
  cooldown_seconds: number;
};
export const replySourceNames: Record<ReplySource, string> = {
  personal: "我的",
  tenant: "团队",
  space: "服务空间",
  link: "接入链接",
  legacy: "基础欢迎语",
};
export const welcomeScenarioNames = { all: "全部时段", business: "营业中", outside: "非营业" };
