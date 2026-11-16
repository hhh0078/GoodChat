/** 文件作用说明：隔离并保留当前窗口中各服务器的未发送草稿。 */

/** switchDraftServer 归档旧服务器的草稿，再恢复目标服务器自己的草稿。 */
export function switchDraftServer(storage: Storage, server: string) {
  const activeKey = "goodchat.desktop.server";
  const previous = storage.getItem(activeKey);
  if (previous === server) return;
  const targetKey = `goodchat.desktop.savedDrafts:${encodeURIComponent(server)}`;
  const saved = storage.getItem(targetKey);
  let restored: Record<string, string> = {};
  if (saved) {
    const value: unknown = JSON.parse(saved);
    if (
      !value ||
      typeof value !== "object" ||
      Array.isArray(value) ||
      Object.entries(value).some(
        ([key, item]) =>
          !key.startsWith("goodchat.draft.") || typeof item !== "string",
      )
    )
      throw new Error("目标服务器的窗口草稿记录损坏，已保留当前草稿。");
    restored = value as Record<string, string>;
  }
  const keys = Array.from({ length: storage.length }, (_, index) =>
    storage.key(index),
  ).filter((key): key is string => !!key?.startsWith("goodchat.draft."));
  if (previous)
    storage.setItem(
      `goodchat.desktop.savedDrafts:${encodeURIComponent(previous)}`,
      JSON.stringify(
        Object.fromEntries(
          keys.map((key) => [key, storage.getItem(key) || ""]),
        ),
      ),
    );
  // 先完成归档；写入失败不能先清空原服务器草稿。
  for (const key of keys) storage.removeItem(key);
  for (const [key, value] of Object.entries(restored))
    storage.setItem(key, value);
  storage.setItem(activeKey, server);
}
