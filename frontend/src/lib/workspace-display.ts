/** 文件作用说明：规范客服工作台渠道标签、客户参数与多条备注展示，保留零值和布尔值。 */

/** displayValue 将业务字段转为可读文字，保留零、布尔和结构化值。 */
export function displayValue(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—";
  return typeof value === "object" ? JSON.stringify(value) : String(value);
}

/** recordValue 仅接受普通键值对象，避免数组和空值被当作字段集合。 */
export function recordValue(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

/** channelTags 提取入口多渠道标签并兼容已有单渠道快照，按原顺序去重。 */
export function channelTags(source: unknown): string[] {
  const snapshot = recordValue(source);
  const entry = recordValue(snapshot.entry);
  const candidates = Array.isArray(entry.channel_tags)
    ? entry.channel_tags
    : Array.isArray(snapshot.channel_tags)
      ? snapshot.channel_tags
      : [entry.channel_code || snapshot.channel_code];
  const tags = [
    ...new Set(
      candidates
        .filter((item): item is string => typeof item === "string" && !!item.trim())
        .map((item) => item.trim()),
    ),
  ];
  if (tags.length) return tags;
  const legacy = entry.channel_code || snapshot.channel_code;
  return typeof legacy === "string" && legacy.trim() ? [legacy.trim()] : [];
}

/** customerFields 合并档案与当前入口额外资料，当前会话值优先且不丢失空值及零值。 */
export function customerFields(attributes: unknown, context: unknown): Record<string, unknown> {
  const current = recordValue(context);
  return { ...recordValue(attributes), ...current };
}

/** mergeNotes 以稳定记录编号合并备注分页和刷新结果，不丢失已加载的旧记录。 */
export function mergeNotes<T extends { id: string; created_at: string }>(
  old: T[],
  incoming: T[],
): T[] {
  const notes = new Map(old.map((note) => [note.id, note]));
  for (const note of incoming) notes.set(note.id, note);
  return [...notes.values()].sort(
    (a, b) => b.created_at.localeCompare(a.created_at) || b.id.localeCompare(a.id),
  );
}

/** noteFailureState 仅在身份或客户权限失效时清除备注，临时网络错误保留分页进度。 */
export function noteFailureState<T, C>(
  state: { items: T[]; cursor: C | null; paged: boolean },
  status: number,
): { items: T[]; cursor: C | null; paged: boolean } {
  return [401, 403, 404].includes(status) ? { items: [], cursor: null, paged: false } : state;
}

/** insertReplyText 在用户明确选择插入时追加话术，保留当前未发送草稿。 */
export function insertReplyText(draft: string, content: string): string {
  return draft ? `${draft}\n${content}` : content;
}
