/** 文件作用说明：按服务空间与入口持久缓存访客恢复资料，缓存编号只作提示，访问权限仍由服务端凭证验证。 */
import { translate as tr } from "../i18n/runtime.ts";
import type { VisitorParameters } from "./visitor-params";
type CacheStorage = Pick<Storage, "getItem" | "setItem" | "removeItem"> &
  Partial<Pick<Storage, "key" | "length">>;
export type VisitorSessionCache = {
  version: 1;
  service_space_id: string;
  entry_key: string;
  visitor_token?: string;
  conversation_id?: string;
  contact: {
    id?: string;
    display_name?: string;
    phone?: string;
    avatar_url?: string;
  };
  parameters: Omit<VisitorParameters, "handoff_code">;
  pending_visit_id?: string;
  pending_started_at?: string;
  updated_at: string;
};
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
/** visitorCacheKey 让不同入口与服务空间各自保存参数，不把一个入口的指定客服带到其他入口。 */
export function visitorCacheKey(space: string, entry: string) {
  return `goodchat.visitor-session.v1.${encodeURIComponent(space)}.${encodeURIComponent(entry)}`;
}
/** entryCacheSpaces 仅枚举当前同源存储中相同入口的空间候选，最终仍需校验缓存正文的入口与空间。 */
function entryCacheSpaces(storage: CacheStorage, entry: string): string[] {
  if (typeof storage.key !== "function" || !Number.isSafeInteger(storage.length)) return [];
  const prefix = "goodchat.visitor-session.v1.",
    suffix = `.${encodeURIComponent(entry)}`;
  const spaces: string[] = [];
  for (let index = 0; index < (storage.length || 0); index++) {
    const key = storage.key(index);
    if (!key?.startsWith(prefix) || !key.endsWith(suffix)) continue;
    try {
      const space = decodeURIComponent(key.slice(prefix.length, -suffix.length));
      if (space && visitorCacheKey(space, entry) === key) spaces.push(space);
    } catch {
      /* 损坏的键不提供身份恢复资料。 */
    }
  }
  return spaces;
}
/** objectValue 拒绝损坏缓存中的数组和非对象，返回可安全展开的键值数据。 */
function objectValue(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}
/** stringValues 只保留字段的字符串值，缓存不能把对象注入路由或基本资料。 */
function stringValues(value: unknown, allowed?: string[]): Record<string, string> {
  return Object.fromEntries(
    Object.entries(objectValue(value)).filter(
      (item): item is [string, string] =>
        typeof item[1] === "string" && (!allowed || allowed.includes(item[0])),
    ),
  );
}
/** safeVisitorParameters 只缓存公开接入资料，不持久保存一次性handoff或原始带凭证网址。 */
export function safeVisitorParameters(
  value: Partial<VisitorParameters>,
): Omit<VisitorParameters, "handoff_code"> {
  return {
    profile: stringValues(value.profile, [
      "display_name",
      "phone",
      "avatar_url",
      "external_user_id",
    ]),
    routing: stringValues(value.routing, [
      "service_group_id",
      "routing_pool_id",
      "agent_id",
      "agent_mode",
    ]),
    context: { ...objectValue(value.context) },
    attributes: { ...objectValue(value.attributes) },
    tracking: stringValues(value.tracking, [
      "channel_code",
      "utm_source",
      "utm_medium",
      "utm_campaign",
      "utm_content",
      "utm_term",
    ]),
  };
}
/** readVisitorSessionCache 校验缓存版本、所属入口与字段形态，损坏缓存不作可信身份。 */
export function readVisitorSessionCache(
  storage: CacheStorage,
  space: string,
  entry: string,
): VisitorSessionCache | null {
  const raw = storage.getItem(visitorCacheKey(space, entry));
  if (!raw || raw.length > 262144) return null;
  try {
    const value = JSON.parse(raw) as VisitorSessionCache;
    if (
      value.version !== 1 ||
      value.service_space_id !== space ||
      value.entry_key !== entry ||
      !value.parameters
    )
      return null;
    const contact = stringValues(value.contact);
    return {
      version: 1,
      service_space_id: space,
      entry_key: entry,
      visitor_token:
        typeof value.visitor_token === "string" &&
        /^v_[A-Za-z0-9_-]{20,256}$/.test(value.visitor_token)
          ? value.visitor_token
          : undefined,
      conversation_id:
        typeof value.conversation_id === "string" && uuid.test(value.conversation_id)
          ? value.conversation_id
          : undefined,
      contact: {
        id: typeof contact.id === "string" && uuid.test(contact.id) ? contact.id : undefined,
        display_name: contact.display_name,
        phone: contact.phone,
        avatar_url: contact.avatar_url,
      },
      parameters: safeVisitorParameters(value.parameters),
      pending_visit_id:
        typeof value.pending_visit_id === "string" && uuid.test(value.pending_visit_id)
          ? value.pending_visit_id
          : undefined,
      pending_started_at:
        typeof value.pending_started_at === "string" ? value.pending_started_at : undefined,
      updated_at: typeof value.updated_at === "string" ? value.updated_at : "",
    };
  } catch {
    return null;
  }
}
/** recoverVisitorSessionCache 在同入口换默认空间后恢复旧凭证提示，服务端必须再次验证凭证及原会话权限。 */
export function recoverVisitorSessionCache(
  storage: CacheStorage,
  space: string,
  entry: string,
): VisitorSessionCache | null {
  const current = readVisitorSessionCache(storage, space, entry);
  if (current?.visitor_token) return current;
  let newest: VisitorSessionCache | null = null;
  for (const previousSpace of entryCacheSpaces(storage, entry)) {
    if (previousSpace === space) continue;
    const previous = readVisitorSessionCache(storage, previousSpace, entry);
    if (
      previous?.visitor_token &&
      (!newest || (Date.parse(previous.updated_at) || 0) > (Date.parse(newest.updated_at) || 0))
    )
      newest = previous;
  }
  return newest ? { ...newest, service_space_id: space } : current;
}
/** removeOtherEntryCaches 在当前键已保存或凭证失效后清理同入口旧键，绝不删除其他入口的凭证。 */
function removeOtherEntryCaches(storage: CacheStorage, space: string, entry: string) {
  for (const previousSpace of entryCacheSpaces(storage, entry))
    if (previousSpace !== space && readVisitorSessionCache(storage, previousSpace, entry))
      storage.removeItem(visitorCacheKey(previousSpace, entry));
}
/** writeVisitorSessionCache 有界保存资料与待确认访问编号，不因缓存失败丢弃已成功建立的页面会话。 */
export function writeVisitorSessionCache(storage: CacheStorage, value: VisitorSessionCache) {
  const raw = JSON.stringify({ ...value, parameters: safeVisitorParameters(value.parameters) });
  if (raw.length > 262144) throw new Error(tr("访客资料超过本地缓存限制"));
  storage.setItem(visitorCacheKey(value.service_space_id, value.entry_key), raw);
  removeOtherEntryCaches(storage, value.service_space_id, value.entry_key);
}
/** mergeVisitorParameters 当前链接显式参数覆盖同入口缓存，保留未重新提供的业务值与零和false。 */
export function mergeVisitorParameters(
  previous: VisitorSessionCache | null,
  current: VisitorParameters,
): VisitorParameters {
  const cached = previous?.parameters;
  return {
    profile: { ...cached?.profile, ...current.profile },
    routing: { ...cached?.routing, ...current.routing },
    context: { ...cached?.context, ...current.context },
    attributes: { ...cached?.attributes, ...current.attributes },
    tracking: { ...cached?.tracking, ...current.tracking },
    ...(current.handoff_code ? { handoff_code: current.handoff_code } : {}),
  };
}
/** forgetVisitorSessionCache 只删除本入口的恢复资料，不能拿过期身份继续打开旧会话。 */
export function forgetVisitorSessionCache(storage: CacheStorage, space: string, entry: string) {
  storage.removeItem(visitorCacheKey(space, entry));
  removeOtherEntryCaches(storage, space, entry);
}
/** withVisitorIdentityLock 在浏览器支持时串行首次身份初始化，跨标签共享空间凭证，异常会自动释放锁。 */
export async function withVisitorIdentityLock<T>(
  space: string,
  operation: () => Promise<T>,
  locks?: Pick<LockManager, "request">,
): Promise<T> {
  return locks ? locks.request(`goodchat.visitor.identity.${space}`, operation) : operation();
}
