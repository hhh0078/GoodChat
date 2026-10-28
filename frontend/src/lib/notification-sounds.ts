/** 文件作用说明：定义租户可选的五个1秒提示音，使用受控本地资源并统一回退选择。 */
export const notificationSounds = [
  { id: "gentle", name: "轻柔和弦", url: "/sounds/gentle.wav", duration: 1 },
  { id: "glass", name: "清透水晶", url: "/sounds/glass.wav", duration: 1 },
  { id: "wood", name: "温润木琴", url: "/sounds/wood.wav", duration: 1 },
  { id: "bell", name: "柔和风铃", url: "/sounds/bell.wav", duration: 1 },
  { id: "bubble", name: "轻快气泡", url: "/sounds/bubble.wav", duration: 1 },
] as const;

/** notificationSound 将已保存编号映射到固定本地音频，旧配置使用默认和弦。 */
export function notificationSound(id?: string) {
  return notificationSounds.find((item) => item.id === id) || notificationSounds[0];
}
