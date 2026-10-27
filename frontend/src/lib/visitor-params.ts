/** 文件作用说明：严格解析公开接入参数，保留业务值类型且拒绝重复字段与非法 JSON。 */
type VisitorParameters = {
  routing: Record<string, string>;
  profile: Record<string, string>;
  context: Record<string, unknown>;
  attributes: Record<string, unknown>;
  tracking: Record<string, string>;
  handoff_code?: string;
};
/** parseVisitorParameters 只映射允许的接入字段，不将公开资料解释为认证身份。 */
export function parseVisitorParameters(input: URLSearchParams): VisitorParameters {
  const result: VisitorParameters = {
    routing: {},
    profile: {},
    context: {},
    attributes: {},
    tracking: {},
  };
  const seen = new Set<string>();
  for (const [key] of input) {
    if (seen.has(key)) throw new Error(`接入参数重复：${key}`);
    seen.add(key);
  }
  for (const key of ["service_group_id", "routing_pool_id", "agent_id", "agent_mode"]) {
    const value = input.get(key);
    if (value) result.routing[key] = value;
  }
  for (const key of ["phone", "avatar_url", "external_user_id"]) {
    const value = input.get(key);
    if (value) result.profile[key] = value;
  }
  const name = input.get("display_name") || input.get("name");
  if (
    input.get("display_name") &&
    input.get("name") &&
    input.get("display_name") !== input.get("name")
  )
    throw new Error("称呼参数存在冲突");
  if (name) result.profile.display_name = name;
  for (const key of ["context", "attributes"] as const) {
    const raw = input.get(key);
    if (!raw) continue;
    let value: unknown;
    try {
      value = JSON.parse(raw);
    } catch {
      throw new Error(`${key} 必须是合法 JSON 对象`);
    }
    if (!value || typeof value !== "object" || Array.isArray(value))
      throw new Error(`${key} 必须是 JSON 对象`);
    result[key] = value as Record<string, unknown>;
  }
  for (const key of [
    "channel_code",
    "utm_source",
    "utm_medium",
    "utm_campaign",
    "utm_content",
    "utm_term",
  ]) {
    const value = input.get(key);
    if (value) result.tracking[key] = value;
  }
  const handoff = input.get("handoff_code");
  if (handoff) result.handoff_code = handoff;
  return result;
}

/** canRestartVisit 只对首次访问幂等冲突提供新访问动作，不拿访问编号恢复身份。 */
export function canRestartVisit(error: unknown): boolean {
  return (
    !!error &&
    typeof error === "object" &&
    "status" in error &&
    error.status === 409 &&
    "code" in error &&
    error.code === "visit_conflict"
  );
}
