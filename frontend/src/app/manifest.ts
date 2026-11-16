/** 文件作用说明：为浏览器收藏提供统一品牌名称与不同遮罩图标，不声明离线消息功能。 */
import type { MetadataRoute } from "next";

/** manifest 将普通图标和遮罩安全图分别交给浏览器，保持正常浏览器打开方式。 */
export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "GoodChat",
    short_name: "GoodChat",
    start_url: "/",
    display: "browser",
    theme_color: "#165DFF",
    background_color: "#FFFFFF",
    icons: [
      { src: "/logo/icon-192.png", sizes: "192x192", type: "image/png", purpose: "any" },
      { src: "/logo/icon-512.png", sizes: "512x512", type: "image/png", purpose: "any" },
      {
        src: "/logo/icon-maskable-512.png",
        sizes: "512x512",
        type: "image/png",
        purpose: "maskable",
      },
    ],
  };
}
