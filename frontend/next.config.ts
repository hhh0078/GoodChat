/** 文件作用说明：配置前端启动、独立构建和同源后端 API 及实时连接代理。 */
import type { NextConfig } from "next";
import createNextIntlPlugin from "next-intl/plugin";

const backend = process.env.BACKEND_URL || "http://127.0.0.1:18080";
const config: NextConfig = {
  poweredByHeader: false,
  output: "standalone",
  experimental: { cpus: 2 },
  /** rewrites 将 API 和实时连接代理到本项目 Go 服务。 */
  async rewrites() {
    return [
      { source: "/api/v1/:path*", destination: `${backend}/api/v1/:path*` },
      { source: "/ws/v1", destination: `${backend}/ws/v1` },
    ];
  },
};
export default createNextIntlPlugin("./src/i18n/request.ts")(config);
