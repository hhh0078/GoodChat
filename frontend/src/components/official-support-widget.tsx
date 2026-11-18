/** 文件作用说明：仅在官网布局挂载公开客服JS，离开官网时释放悬浮窗，不向页面传入账号凭据。 */
"use client";
import { useEffect } from "react";

type WidgetWindow = Window & {
  GoodChatWidgets?: Record<string, { destroy: () => void }>;
};

/** OfficialSupportWidget 使用与第三方网站相同的外部脚本接入正式咨询入口。 */
export function OfficialSupportWidget({ entryKey }: { entryKey: string }) {
  useEffect(() => {
    if (!entryKey) return;
    const browser = window as WidgetWindow;
    const script = document.createElement("script");
    script.src = `/api/v1/widget/${encodeURIComponent(entryKey)}.js`;
    script.async = true;
    let disposed = false;
    /** releaseLateLoad 清理离开页面后才完成加载的脚本实例。 */
    function releaseLateLoad() {
      if (disposed) browser.GoodChatWidgets?.[entryKey]?.destroy();
    }
    script.addEventListener("load", releaseLateLoad, { once: true });
    document.body.append(script);
    return () => {
      disposed = true;
      script.remove();
      browser.GoodChatWidgets?.[entryKey]?.destroy();
    };
  }, [entryKey]);
  return null;
}
