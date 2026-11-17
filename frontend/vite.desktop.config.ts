/** 文件作用说明：将现有客服组件打包为桌面本地资源，替换Next路由与浏览器实时连接入口。 */
import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";

/** path 将配置相对路径转换为跨平台构建路径。 */
function path(value: string) {
  return fileURLToPath(new URL(value, import.meta.url));
}

export default defineConfig({
  root: path("../desktop/frontend"),
  publicDir: false,
  resolve: {
    alias: [
      { find: "@/features/auth/session", replacement: path("../desktop/frontend/src/session.tsx") },
      {
        find: "@/lib/realtime/use-realtime",
        replacement: path("../desktop/frontend/src/use-realtime.ts"),
      },
      {
        find: /^(?:@\/features\/chat\/|\.\/)workspace-reminders$/,
        replacement: path("../desktop/frontend/src/reminders.tsx"),
      },
      { find: "@/i18n/link", replacement: path("../desktop/frontend/src/link.tsx") },
      { find: /^next\/link$/, replacement: path("../desktop/frontend/src/link.tsx") },
      { find: "@", replacement: path("./src") },
      { find: /^react$/, replacement: path("./node_modules/react") },
      { find: /^react\//, replacement: path("./node_modules/react/") },
      { find: /^react-dom$/, replacement: path("./node_modules/react-dom") },
      { find: /^react-dom\//, replacement: path("./node_modules/react-dom/") },
      { find: "antd", replacement: path("./node_modules/antd") },
      { find: "@ant-design/icons", replacement: path("./node_modules/@ant-design/icons") },
      { find: "@wailsio/runtime", replacement: path("./node_modules/@wailsio/runtime") },
    ],
    dedupe: ["react", "react-dom"],
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    target: ["safari15", "chrome105"],
    chunkSizeWarningLimit: 2000,
    rolldownOptions: {
      /** onwarn 桌面所有组件都在客户端运行，忽略Next专用指令的无关提示。 */
      onwarn(warning, warn) {
        if (warning.code !== "MODULE_LEVEL_DIRECTIVE") warn(warning);
      },
    },
  },
  server: { host: "127.0.0.1", port: 13100, strictPort: true, fs: { allow: [path("../")] } },
});
