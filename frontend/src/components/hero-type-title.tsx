/** 文件作用说明：参考MotionLab打字机逐字显示首屏标题，完整文字始终保留在服务端HTML与无障碍名称中。 */
import type { CSSProperties } from "react";

/** HeroTypeTitle 以保留排版空间的字符显隐实现打字效果，避免标题出现时推动页面。 */
export function HeroTypeTitle({ lines }: { lines: [string, string] }) {
  let offset = 0;
  const interval = Math.min(80, 3000 / lines.join("").length);
  return (
    <h1 className="hero-type-title" aria-label={lines.join(" ")}>
      {lines.map((line, row) => (
        <span className="hero-type-line" key={row} aria-hidden="true">
          {Array.from(line).map((character, index) => (
            <span
              className="hero-type-char"
              key={index}
              style={
                {
                  "--char-delay": `${offset++ * interval}ms`,
                  "--char-interval": `${interval}ms`,
                } as CSSProperties
              }
            >
              {character}
            </span>
          ))}
        </span>
      ))}
    </h1>
  );
}
