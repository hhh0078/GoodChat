/** 文件作用说明：提供需要成员身份的客服网页工作台入口。 */
import { AgentWorkspace } from "@/features/chat/workspace";
import { conversationFromQuery } from "@/lib/urls";
/** WorkspacePage 加载客服接待工作台。 */
export default async function WorkspacePage({
  searchParams,
}: {
  searchParams: Promise<{ conversation?: string | string[] }>;
}) {
  const query = await searchParams;
  return <AgentWorkspace linkedConversationId={conversationFromQuery(query.conversation)} />;
}
