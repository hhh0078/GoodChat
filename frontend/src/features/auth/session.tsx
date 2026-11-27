/** 文件作用说明：使用服务端默认客服后台管理当前账号身份，不在浏览器保存成员登录令牌。 */
"use client";
import { ErrorNotice } from "@/components/page-state";
import { useTranslator } from "@/i18n/use-translator";
import { api, ApiError, errorMessage } from "@/lib/api";
import type { CurrentUser } from "@/lib/types";
import { loginReturnURL } from "@/lib/urls";
import { Alert, Button, Result, Spin } from "antd";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useCallback, useContext, useEffect, useState } from "react";
type SessionValue = {
  current: CurrentUser;
  tenantId: string;
  reload: () => Promise<void>;
};
const SessionContext = createContext<SessionValue | null>(null);
/** SessionProvider 获取服务端默认后台身份，忽略历史切换偏好并在未登录时跳转。 */
export function SessionProvider({ children }: { children: React.ReactNode }) {
  const tr = useTranslator();
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
      window.dispatchEvent(new Event("goodchat:authenticated"));
      setError("");
      try {
        localStorage.removeItem("goodchat.activeTenant");
      } catch {
        /* 禁用存储不影响服务端默认身份。 */
      }
      setTenant(data.active_tenant_id || data.memberships[0]?.tenant_id || "");
    } catch (problem) {
      setCurrent(null);
      if (problem instanceof ApiError && problem.status === 401)
        router.replace(loginReturnURL(pathname, window.location.search));
      else setError(errorMessage(problem));
    }
  }, [pathname, router]);
  useEffect(() => {
    void reload();
  }, [reload]);
  if (error)
    return (
      <Result
        status="warning"
        title={tr("重新连接")}
        subTitle={<ErrorNotice error={error} />}
        extra={<Button onClick={() => void reload()}>{tr("重新连接")}</Button>}
      />
    );
  if (!current)
    return (
      <div className="console-loading">
        <Spin size="large" />
      </div>
    );
  return (
    <SessionContext.Provider value={{ current, tenantId, reload }}>
      {children}
    </SessionContext.Provider>
  );
}
/** useSession 读取工作端身份，避免在未认证区域误用租户请求。 */
export function useSession(): SessionValue {
  const tr = useTranslator();
  const context = useContext(SessionContext);
  if (!context) throw new Error(tr("工作空间身份尚未初始化"));
  return context;
}
