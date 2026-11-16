/** 文件作用说明：初始化桌面React入口，复用Ant Design及项目钴蓝品牌。 */
import { createRoot } from "react-dom/client";
import { App, ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import "@/app/globals.css";
import "@/styles/workbench-redesign.css";
import "@/styles/console-redesign.css";
import DesktopApp from "./app";
import "./style.css";

/** Root 为桌面界面注入中文组件与品牌主题。 */
function Root() {
  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        token: {
          colorPrimary: "#1455ff",
          borderRadius: 8,
          fontFamily:
            '"Helvetica Neue", "PingFang SC", "Microsoft YaHei", sans-serif',
        },
      }}
    >
      <App>
        <DesktopApp />
      </App>
    </ConfigProvider>
  );
}

createRoot(document.getElementById("root")!).render(<Root />);
