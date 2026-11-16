/** 文件作用说明：为复用的客服组件提供桌面身份上下文，不依赖Next.js路由。 */
import { createContext, useContext } from "react";
import type { CurrentUser } from "@/lib/types";
export type SessionValue = {
  current: CurrentUser;
  tenantId: string;
  setTenantId: (id: string) => void;
  reload: () => Promise<void>;
};
const Context = createContext<SessionValue | null>(null);
export const DesktopSessionProvider = Context.Provider;

/** useSession 只在已完成设备认证的工作台中读取当前工作空间。 */
export function useSession(): SessionValue {
  const value = useContext(Context);
  if (!value) throw new Error("工作空间尚未初始化");
  return value;
}
