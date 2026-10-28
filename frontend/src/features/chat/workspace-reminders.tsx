/** 文件作用说明：在所有工作端页面共享实时在线连接与接待提醒，工作台音量控件复用同一成员状态。 */
"use client";
import { createContext, useContext } from "react";
import { useRealtime } from "@/lib/realtime/use-realtime";
import { useReminders } from "./reminders";
const ReminderContext = createContext<ReturnType<typeof useReminders> | null>(null);

/** WorkspaceRemindersProvider 为所有有效成员维持在线连接，为可接待成员持续检查未读提醒。 */
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
  const reminders = useReminders(tenantId, canServe, memberId);
  useRealtime(
    { tenantId, actorId: memberId, kind: "agent" },
    { enabled: !!memberId && !!tenantId, onEvent: reminders.receive, onRecover: reminders.recover },
  );
  return <ReminderContext.Provider value={reminders}>{children}</ReminderContext.Provider>;
}

/** useWorkspaceReminders 让工作台读取全工作端同一个提醒控制器，不再重复创建声音与定时器。 */
export function useWorkspaceReminders() {
  const value = useContext(ReminderContext);
  if (!value) throw new Error("接待提醒需要工作端身份上下文");
  return value;
}
