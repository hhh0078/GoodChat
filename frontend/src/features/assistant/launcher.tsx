/** 文件作用说明：在工作端右上角提供仅管理员可见的全局 AI 入口，并允许模板页发起二创。 */
"use client";
import { createContext, useCallback, useContext, useEffect, useState } from "react";
import dynamic from "next/dynamic";
import { Button, Tooltip } from "antd";
import { RobotOutlined } from "@ant-design/icons";
import { useSession } from "@/features/auth/session";
import { canUseAssistant, type AssistantRequest } from "./state";
import "./assistant.css";

const AssistantPanel = dynamic(() => import("./panel"), { ssr: false });
const AssistantContext = createContext<{
  openAssistant: (request?: AssistantRequest) => void;
} | null>(null);

/** AssistantProvider 以当前租户和角色限定面板挂载，权限变化后不会保留旧对话界面。 */
export function AssistantProvider({ children }: { children: React.ReactNode }) {
  const { current, tenantId } = useSession();
  const member = current.memberships.find((item) => item.tenant_id === tenantId);
  const tenantAdmin = member?.role === "owner" || member?.role === "admin";
  const allowed = canUseAssistant(current.user.is_platform_admin, member?.role);
  const [request, setRequest] = useState<AssistantRequest>({});
  const [open, setOpen] = useState(false);
  useEffect(() => {
    setOpen(false);
    setRequest({});
  }, [tenantId, member?.role]);

  /** openAssistant 每次入口操作生成新的恢复标识，禁止客服通过模板按钮打开助手。 */
  const openAssistant = useCallback(
    (next: AssistantRequest = {}) => {
      if (!allowed) return;
      setRequest({
        ...next,
        source: next.source || (current.user.is_platform_admin ? "platform" : "tenant"),
        nonce: Date.now(),
      });
      setOpen(true);
    },
    [allowed, current.user.is_platform_admin],
  );

  return (
    <AssistantContext.Provider value={{ openAssistant }}>
      {children}
      {allowed && open && (
        <AssistantPanel
          key={`${tenantId}:${current.user.id}:${member?.role}`}
          open={open}
          onClose={() => setOpen(false)}
          initial={request}
          tenantId={tenantId}
          platformAdmin={current.user.is_platform_admin}
          tenantAdmin={tenantAdmin}
        />
      )}
    </AssistantContext.Provider>
  );
}

/** useAssistantLauncher 供模板按钮复用全局入口，不另建彼此独立的 AI 会话面板。 */
export function useAssistantLauncher() {
  const value = useContext(AssistantContext);
  if (!value) throw new Error("AI 助手入口尚未初始化");
  return value;
}

/** AssistantLauncher 根据当前成员身份显示全局入口，普通客服不会请求任何 AI 配置。 */
export function AssistantLauncher() {
  const { current, tenantId } = useSession();
  const { openAssistant } = useAssistantLauncher();
  const member = current.memberships.find((item) => item.tenant_id === tenantId);
  if (!canUseAssistant(current.user.is_platform_admin, member?.role)) return null;
  return (
    <Tooltip title="AI 设计用户端模板">
      <Button
        className="assistant-launcher"
        aria-label="AI 助手"
        icon={<RobotOutlined />}
        onClick={() => openAssistant()}
      >
        <span>AI 助手</span>
      </Button>
    </Tooltip>
  );
}
