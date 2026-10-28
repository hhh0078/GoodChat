/** 文件作用说明：用真实小时或日统计绘制可聚焦查看数值的平滑趋势图，同时保留表格供精确阅读。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { EmptyData } from "@/components/page-state";
import { smoothTrendPath } from "@/lib/trend-curve";

export type TrendPoint = {
  date: string;
  timestamp?: string;
  visitors: number;
  conversations: number;
  messages: number;
};
const series = [
  { key: "visitors", label: "访客", className: "visitors" },
  { key: "conversations", label: "咨询", className: "conversations" },
  { key: "messages", label: "消息", className: "messages" },
] as const;

/** ReportTrend 用有限区间贝塞尔曲线连接计数点，悬停或键盘聚焦均可读取原始数值。 */
export function ReportTrend({ items, hourly }: { items: TrendPoint[]; hourly: boolean }) {
  const [focus, setFocus] = useState<number | null>(null);
  const plot = useRef<HTMLDivElement | null>(null);
  const [width, setWidth] = useState(900);
  useEffect(() => {
    const element = plot.current;
    if (!element) return;
    /** measure 按容器重算绘图区，使手机刻度保持实际12像素而不随整张SVG缩成小字。 */
    function measure() {
      setWidth(Math.max(200, element!.clientWidth - 84));
    }
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [items.length]);
  if (!items.length) return <EmptyData />;
  const height = 200;
  const maximum = Math.max(
    1,
    ...items.flatMap((item) => series.map(({ key }) => Number(item[key]) || 0)),
  );
  const current = items[focus === null ? items.length - 1 : Math.min(focus, items.length - 1)];
  return (
    <div className="report-trend">
      <div className="report-trend-heading">
        <div>
          <h2>{hourly ? "时段趋势" : "服务趋势"}</h2>
          <span className="muted">
            {hourly ? "按小时汇总" : "按天汇总"} · 曲线连接真实样本，具体数值以数据点为准
          </span>
        </div>
        <div className="report-trend-legend">
          {series.map((item) => (
            <span key={item.key} className={item.className}>
              <i />
              {item.label}
            </span>
          ))}
        </div>
      </div>
      <div className="report-trend-reading" aria-live="polite">
        <strong>{current.date}</strong>
        {series.map(({ key, label }) => (
          <span key={key}>
            {label} <b>{current[key]}</b>
          </span>
        ))}
      </div>
      <div ref={plot}>
        <svg
          viewBox={`-42 -12 ${width + 84} 250`}
          role="img"
          aria-label={`${hourly ? "小时" : "每日"}服务量平滑曲线，可在下方查看明细数据`}
        >
          {[0, 0.5, 1].map((ratio) => (
            <g key={ratio}>
              <line
                className="report-trend-grid"
                x1="0"
                x2={width}
                y1={height * ratio}
                y2={height * ratio}
              />
              <text x="-12" y={height * ratio + 4} textAnchor="end">
                {new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 1 }).format(
                  maximum * (1 - ratio),
                )}
              </text>
            </g>
          ))}
          {series.map(({ key, className }) => (
            <path
              key={key}
              className={`report-trend-line ${className}`}
              d={smoothTrendPath(
                items.map((item) => item[key]),
                width,
                height,
                maximum,
              )}
            />
          ))}
          {items.map((item, index) => {
            const x = items.length === 1 ? width / 2 : (index * width) / (items.length - 1);
            return (
              <g key={`${item.date}-${index}`}>
                <rect
                  className="report-trend-hit"
                  x={x - width / Math.max(2, items.length * 2)}
                  y="0"
                  width={width / Math.max(1, items.length)}
                  height={height}
                  tabIndex={0}
                  role="img"
                  aria-label={`${item.date}：访客${item.visitors}，咨询${item.conversations}，消息${item.messages}`}
                  onFocus={() => setFocus(index)}
                  onMouseEnter={() => setFocus(index)}
                  onBlur={() => setFocus(null)}
                  onMouseLeave={() => setFocus(null)}
                />
                {(index === 0 ||
                  index === items.length - 1 ||
                  index % Math.max(1, Math.ceil(items.length / (width < 400 ? 3 : 6))) === 0) && (
                  <text x={x} y={height + 28} textAnchor="middle">
                    {hourly ? item.date : item.date.slice(5)}
                  </text>
                )}
              </g>
            );
          })}
        </svg>
      </div>
    </div>
  );
}
