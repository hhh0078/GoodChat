/** 文件作用说明：提供仅总管理员可访问的独立服务器监控页面。 */
import { ServerMonitorPage } from "@/features/platform/server-monitor";
/** ServerMonitoringRoute 连接平台资源监控组件。 */
export default function ServerMonitoringRoute() {
  return <ServerMonitorPage />;
}
