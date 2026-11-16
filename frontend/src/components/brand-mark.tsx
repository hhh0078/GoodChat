/** 文件作用说明：以同一矢量母版展示GoodChat图形标志，文字与无障碍名称由使用方提供。 */
import Image from "next/image";

/** BrandMark 在平台品牌位置展示透明图形，反白版本用于钴蓝背景。 */
export function BrandMark({ inverted = false, size = 32 }: { inverted?: boolean; size?: number }) {
  return (
    <Image
      className="brand-symbol"
      src={inverted ? "/logo/goodchat-mark-white.svg" : "/logo/goodchat-mark.svg"}
      width={size}
      height={size}
      alt=""
      aria-hidden="true"
      unoptimized
    />
  );
}
