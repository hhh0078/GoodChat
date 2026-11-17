/** 文件作用说明：初始化桌面React入口，复用Ant Design及项目钴蓝品牌。 */
import "@/app/globals.css";
import { antdLocales } from "@/i18n/antd-locales";
import { LocaleProvider, usePreferences } from "@/i18n/locale-provider";
import enMessages from "@/i18n/messages/en.json";
import "@/styles/console-redesign.css";
import "@/styles/workbench-redesign.css";
import { App, ConfigProvider } from "antd";
import { createRoot } from "react-dom/client";
import DesktopApp from "./app";
import "./style.css";

/** Root 为桌面界面注入中文组件与品牌主题。 */
function Root() {
  const preferences = usePreferences();
  return (
    <ConfigProvider
      locale={antdLocales[preferences.locale]}
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

createRoot(document.getElementById("root")!).render(
  <LocaleProvider
    initial={{ locale: "en", timezone: "UTC" }}
    messages={enMessages}
  >
    <Root />
  </LocaleProvider>,
);
