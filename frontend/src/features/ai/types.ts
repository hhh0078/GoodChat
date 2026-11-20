/** 文件作用说明：定义客服AI资源、媒体引用、机器人和微元账本的前端结构。 */
import { locales } from "@/i18n/preferences";
export type AIBlock = {
  type: "text" | "image" | "video" | "audio";
  text?: string;
  media_id?: string;
};
export type AIMedia = {
  id: string;
  title: string;
  editable: boolean;
  references: number;
  attachment: { id: string; file_name: string; content_type: string; size_bytes: number };
};
export type AIContent = {
  folder_id?: string | null;
  id: string;
  title: string;
  description: string;
  draft_body?: string;
  published_body?: string;
  published_version?: number;
  status?: string;
  error_code?: string;
  language?: string;
  blocks?: AIBlock[];
  enabled: boolean;
  editable: boolean;
  version: number;
};
export type AIModel = {
  supports_images?: boolean;
  id: string;
  name: string;
  base_url: string;
  model: string;
  enabled: boolean;
  version: number;
  has_api_key: boolean;
  api_key?: string;
  cost_input: number;
  cost_output: number;
  sale_input: number;
  sale_output: number;
  max_input_tokens: number;
  max_output_tokens: number;
};
export type AIBot = {
  avatar_ref: string;
  id?: string;
  owner_membership_id?: string;
  model_id: string;
  name: string;
  mode: "off" | "assistant" | "auto";
  prompt: string;
  language: string;
  knowledge_ids: string[];
  script_ids: string[];
  version: number;
  settings: {
    offline: boolean;
    start: string;
    end: string;
    weekdays: number[];
    daily_budget_micro: number;
    max_output_tokens: number;
    max_turns: number;
  };
};
export type AIReply = {
  items?: { type: "text" | "script"; text?: string; script_id?: string; version?: number }[];
  description?: string;
  handoff?: boolean;
  reason?: string;
  read_ids?: string[];
};
export type AIJob = {
  id: string;
  kind: string;
  status: string;
  error_code: string;
  result: AIReply;
  input_tokens: number;
  output_tokens: number;
  image_inputs: number;
  sale_micro: number;
  charged_micro?: number;
  owner_name?: string;
  billing_status: string;
  created_at: string;
  bot_id?: string;
  owner_membership_id: string;
};
export type AIWalletEntry = {
  id: string;
  kind: string;
  reason: string;
  delta_micro: number;
  created_at: string;
};
export type AIUsageData = {
  summary?: { charged_micro: number; calls: number; uncertain: number };
  wallet: { balance_micro: number; reserved_micro: number; unlimited?: boolean } | null;
  jobs: AIJob[] | null;
  entries:
    { id: string; kind: string; reason: string; delta_micro: number; created_at: string }[] | null;
};
export const aiLanguages = locales;
