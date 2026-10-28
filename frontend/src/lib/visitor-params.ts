/** 文件作用说明：解析中英文公开接入参数和任意业务JSON，展示资料不能变成认证身份或扩大接待权限。 */
export type VisitorParameters = {
  routing: Record<string, string>;
  profile: Record<string, string>;
  context: Record<string, unknown>;
  attributes: Record<string, unknown>;
  tracking: Record<string, string>;
  handoff_code?: string;
};

const profileFields: Record<string, string[]> = {
  display_name: ["姓名", "名称", "display_name", "name", "nickName", "nickname"],
  phone: ["手机号", "手机", "电话", "phone", "mobile"],
  avatar_url: ["头像", "avatar_url", "avatar"],
  external_user_id: ["external_user_id"],
};
const routingFields: Record<string, string[]> = {
  service_group_id: ["客服组", "service_group_id"],
  routing_pool_id: ["分配池", "routing_pool_id"],
  agent_id: ["客服", "agent_id"],
  agent_mode: ["agent_mode"],
};
const trackingFields = [
  "channel_code",
  "utm_source",
  "utm_medium",
  "utm_campaign",
  "utm_content",
  "utm_term",
];
const reservedFields = new Set([
  "visitor_token",
  "token",
  "tenant_id",
  "contact_id",
  "conversation_id",
  "entry_key",
  "handoff_code",
  "page_url",
  "context",
  "extra",
  "extra_data",
  "attributes",
  "性别",
  "sex",
  "gender",
  ...Object.values(profileFields).flat(),
  ...Object.values(routingFields).flat(),
  ...trackingFields,
]);

/** parameterAlias 读取同一字段的中英文别名，拒绝不同值导致的资料歧义。 */
function parameterAlias(input: URLSearchParams, aliases: string[]): string | undefined {
  const values = aliases
    .map((key) => input.get(key))
    .filter((value): value is string => value !== null && value !== "");
  if (new Set(values).size > 1) throw new Error(`${aliases[0]}参数存在冲突`);
  return values[0];
}

/** genderLabel 将中英文性别值规范为客服可读的中文，未知业务值仍保留原文。 */
export function genderLabel(value: string): string {
  const gender = value.trim().toLowerCase();
  if (["男", "man", "male", "m", "1"].includes(gender)) return "男";
  if (["女", "woman", "female", "f", "2"].includes(gender)) return "女";
  if (["未知", "unknown", "unspecified", "other", "0"].includes(gender)) return "未知";
  return value;
}

/** parseParameterObject 验证任意业务扩展参数是JSON对象，禁止数组和损坏数据静默丢失。 */
function parseParameterObject(raw: string, key: string): Record<string, unknown> {
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    throw new Error(`${key} 必须是合法 JSON 对象`);
  }
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error(`${key} 必须是 JSON 对象`);
  return value as Record<string, unknown>;
}

/** mergeParameter 保留零、false和空值，拒绝重复业务键的不同值。 */
function mergeParameter(target: Record<string, unknown>, key: string, value: unknown): void {
  if (Object.hasOwn(target, key) && JSON.stringify(target[key]) !== JSON.stringify(value))
    throw new Error(`业务参数存在冲突：${key}`);
  Object.defineProperty(target, key, {
    value,
    enumerable: true,
    configurable: true,
    writable: true,
  });
}

/** parseVisitorParameters 映射中文常用字段、兼容已有英文链接，并把任意额外资料保留为业务上下文。 */
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
  for (const [key, aliases] of Object.entries(profileFields)) {
    const value = parameterAlias(input, aliases);
    if (value !== undefined) result.profile[key] = value;
  }
  for (const [key, aliases] of Object.entries(routingFields)) {
    const value = parameterAlias(input, aliases);
    if (value !== undefined) result.routing[key] = value;
  }
  for (const key of ["context", "extra", "extra_data"]) {
    const raw = input.get(key);
    if (raw)
      for (const [name, value] of Object.entries(parseParameterObject(raw, key)))
        mergeParameter(result.context, name, value);
  }
  const attributes = input.get("attributes");
  if (attributes) result.attributes = parseParameterObject(attributes, "attributes");
  const genders = ["性别", "sex", "gender"]
    .map((key) => input.get(key))
    .filter((value): value is string => value !== null && value !== "")
    .map(genderLabel);
  if (new Set(genders).size > 1) throw new Error("性别参数存在冲突");
  if (genders.length) mergeParameter(result.context, "性别", genders[0]);
  for (const [key, value] of input)
    if (!reservedFields.has(key)) mergeParameter(result.context, key, value);
  for (const key of trackingFields) {
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
