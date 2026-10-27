/** 文件作用说明：管理当前账号、可用租户与工作空间切换，不在浏览器保存成员登录令牌。 */
"use client";
import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Alert, Button, Result, Spin } from "antd";
import { api, ApiError, errorMessage } from "@/lib/api";
import type { CurrentUser } from "@/lib/types";

type SessionValue = {
  current: CurrentUser;
  tenantId: string;
  setTenantId: (id: string) => void;
  reload: () => Promise<void>;
};
const SessionContext = createContext<SessionValue | null>(null);

/** SessionProvider 获取服务端身份，限定可选租户并在未登录时跳转。 */
export function SessionProvider({ children }: { children: React.ReactNode }) {
  const [current, setCurrent] = useState<CurrentUser | null>(null);
  const [tenantId, setTenant] = useState("");
  const [error, setError] = useState("");
  const router = useRouter();
  const pathname = usePathname();
  /** reload 重新读取账号权限，失败时清空旧身份。 */
  const reload = useCallback(async () => {
    try {
      const data = await api<CurrentUser>("/auth/me");
      setCurrent(data);
      setError("");
      let remembered: string | null = null;
      try {
        remembered = localStorage.getItem("goodchat.activeTenant");
      } catch {
        /* 禁用存储时使用服务端默认租户。 */
      }
      setTenant(
        data.memberships.some((item) => item.tenant_id === remembered)
          ? remembered!
          : data.active_tenant_id || data.memberships[0]?.tenant_id || "",
      );
    } catch (problem) {
      setCurrent(null);
      if (problem instanceof ApiError && problem.status === 401)
        router.replace(`/login?next=${encodeURIComponent(pathname)}`);
      else setError(errorMessage(problem));
    }
  }, [pathname, router]);
  useEffect(() => {
    void reload();
  }, [reload]);
  /** setTenantId 只接受当前账号已有成员关系的租户。 */
  function setTenantId(id: string) {
    if (!current?.memberships.some((item) => item.tenant_id === id)) return;
    try {
      localStorage.setItem("goodchat.activeTenant", id);
    } catch {
      /* 当前窗口仍可切换工作空间。 */
    }
    setTenant(id);
  }
  if (error)
    return (
      <Result
        status="warning"
        title="暂时没有读到工作空间"
        subTitle={<Alert type="error" title={error} />}
        extra={<Button onClick={() => void reload()}>重新连接</Button>}
      />
    );
  if (!current)
    return (
      <div className="console-loading">
        <Spin description="正在读取工作空间" size="large" />
      </div>
    );
  return (
    <SessionContext.Provider value={{ current, tenantId, setTenantId, reload }}>
      {children}
    </SessionContext.Provider>
  );
}
/** useSession 读取工作端身份，避免在未认证区域误用租户请求。 */
export function useSession(): SessionValue {
  const context = useContext(SessionContext);
  if (!context) throw new Error("工作空间身份尚未初始化");
  return context;
}
