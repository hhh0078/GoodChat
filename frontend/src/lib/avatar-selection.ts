/** 文件作用说明：定义头像资源与头像池选择规则，空池配置回退平台默认库。 */
export type AvatarAsset = {
  id: string;
  name: string;
  category: string;
  url: string;
  builtin: boolean;
};
/** selectedAvatarReferences 将空池配置展开为内置头像，普通选择保持原编号和顺序。 */
export function selectedAvatarReferences(refs: string[], assets: AvatarAsset[]): string[] {
  return refs.length
    ? [...new Set(refs)]
    : assets.filter((asset) => asset.builtin).map((asset) => asset.id);
}
/** toggleAvatarReference 切换池中头像，单选模式固定为一个用户明确选择的编号。 */
export function toggleAvatarReference(refs: string[], id: string, multiple: boolean): string[] {
  return multiple ? (refs.includes(id) ? refs.filter((item) => item !== id) : [...refs, id]) : [id];
}
