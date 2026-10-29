/** 文件作用说明：按预览容器的实际内容区域等比缩放固定设备视口，保证页面四边完整可见。 */

/** previewScale 同时受宽高约束且不放大；内容区域由 ResizeObserver 提供，已扣除容器内边距。 */
export function previewScale(
  width: number,
  height: number,
  viewportWidth: number,
  viewportHeight: number,
): number {
  if (
    [width, height, viewportWidth, viewportHeight].some(
      (value) => !Number.isFinite(value) || value <= 0,
    )
  )
    return 0;
  return Math.min(1, width / viewportWidth, height / viewportHeight);
}
