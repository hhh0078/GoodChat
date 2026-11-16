/** 文件作用说明：前台复用网页提醒，后台由原生层领取，通知授权交给操作系统。 */
import { createContext, useContext, useEffect, useState } from "react";
import { useReminders } from "../../../frontend/src/features/chat/reminders";
import { invoke } from "./native";
type Value = ReturnType<typeof useReminders> & { native: boolean };
const Context = createContext<Value | null>(null);

/** WorkspaceRemindersProvider 随原生窗口焦点交接提醒职责，避免网页冻结后停止提醒。 */
export function WorkspaceRemindersProvider({
  tenantId,
  memberId,
  canServe,
  children,
}: {
  tenantId: string;
  memberId: string;
  canServe: boolean;
  children: React.ReactNode;
}) {
  const [foreground, setForeground] = useState(document.hasFocus());
  useEffect(() => {
    /** focus 由真实窗口事件启用前台音频，不接管后台网络任务。 */
    function focus() {
      setForeground(true);
    }
    /** blur 让原生模块负责后台提醒，网页不再抢占服务端领取。 */
    function blur() {
      setForeground(false);
    }
    window.addEventListener("focus", focus);
    window.addEventListener("blur", blur);
    return () => {
      window.removeEventListener("focus", focus);
      window.removeEventListener("blur", blur);
    };
  }, []);
  const reminders = useReminders(tenantId, canServe && foreground, memberId);
  const value: Value = {
    ...reminders,
    native: true,
    /** setSystemNotifications 经原生系统权限授权后保存服务端偏好。 */
    async setSystemNotifications(enabled: boolean) {
      if (enabled && !(await invoke<boolean>("EnableNotifications")))
        throw new Error("系统未允许GoodChat通知，请在系统设置中开启。");
      await reminders.updatePreferences({ system_enabled: enabled });
    },
  };
  return <Context.Provider value={value}>{children}</Context.Provider>;
}
/** useWorkspaceReminders 读取当前工作台的提醒偏好。 */
export function useWorkspaceReminders() {
  const value = useContext(Context);
  if (!value) throw new Error("提醒偏好尚未初始化");
  return value;
}
