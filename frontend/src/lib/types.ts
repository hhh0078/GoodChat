/** 文件作用说明：定义前端共用的账号、租户、资源和聊天接口结构。 */
export type Membership = {
  id: string;
  tenant_id: string;
  tenant_name: string;
  role: string;
  can_serve: boolean;
};
export type CurrentUser = {
  user: { id: string; email: string; display_name: string; is_platform_admin: boolean };
  memberships: Membership[];
  active_tenant_id: string;
};
export type Resource = Record<string, unknown> & { id: string; name?: string; created_at?: string };
export type Member = Resource & {
  user_id: string;
  email: string;
  display_name: string;
  role: string;
  can_serve: boolean;
  disabled: boolean;
  capacity: number;
};
export type AccessLink = Resource & {
  entry_key: string;
  service_space_id: string;
  service_group_id: string;
  routing_pool_id: string;
  status: string;
  agent_mode: string;
};
export type ChatMessage = Resource & {
  content: string;
  sender_kind: string;
  sender_id?: string;
  sender_name?: string;
  message_type: string;
  event_seq: string;
  seq: string;
  audience?: string;
  client_message_id?: string;
  withdrawn_at?: string;
  attachment?: { id: string; file_name: string; content_type: string; size_bytes: number };
  reply?: { id: string; content: string; withdrawn: boolean };
  options?: { id: string; label: string; value: string; client_message_id: string }[];
  source?: string;
  source_version?: string;
};
export type Conversation = Resource & {
  status: string;
  contact_id: string;
  contact_name?: string;
  display_name?: string;
  contact?: { id: string; display_name: string; phone: string };
  agent_name?: string;
  version?: string;
  last_message?: string;
  current_agent_id?: string;
  unread_count?: number;
  pending_transfer?: {
    id: string;
    from_agent_id: string;
    target_agent_id?: string;
    target_member_id?: string;
    status: string;
    expires_at: string;
  } | null;
};
