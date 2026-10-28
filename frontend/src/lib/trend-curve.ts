/** 文件作用说明：生成不越过相邻真实样本范围的平滑曲线路径，避免统计插值产生负值或虚构尖峰。 */

/** smoothTrendPath 将非负计数映射到绘图区，水平贝塞尔控制点使相邻曲段平滑且不越界。 */
export function smoothTrendPath(
  values: number[],
  width: number,
  height: number,
  maximum: number,
): string {
  if (!values.length || width <= 0 || height <= 0 || maximum <= 0) return "";
  const points = values.map((value, index) => ({
    x: values.length === 1 ? width / 2 : (index * width) / (values.length - 1),
    y:
      height -
      (Math.max(0, Math.min(maximum, Number.isFinite(value) ? value : 0)) / maximum) * height,
  }));
  let path = `M ${points[0].x} ${points[0].y}`;
  for (let index = 1; index < points.length; index++) {
    const previous = points[index - 1],
      current = points[index];
    const middle = (previous.x + current.x) / 2;
    path += ` C ${middle} ${previous.y}, ${middle} ${current.y}, ${current.x} ${current.y}`;
  }
  return path;
}
