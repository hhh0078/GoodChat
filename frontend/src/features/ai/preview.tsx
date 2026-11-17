/** 文件作用说明：以服务器素材预览AI回复计划，展示精确话术内容且不执行发送。 */
"use client";
import { useSession } from "@/features/auth/session";
import { AttachmentView } from "@/features/chat/attachment";
import { useTranslator } from "@/i18n/use-translator";
import { useResource } from "@/lib/use-resource";
import { Card } from "antd";
import type { AIBlock, AIContent, AIMedia, AIReply } from "./types";
/** AIPlanPreview 展示自由文字、引用话术及人工请求，不将模型输出当HTML执行。 */
export function AIPlanPreview({ plan }: { plan: AIReply }) {
  const tr = useTranslator();
  const { tenantId, current } = useSession();
  const member = current.memberships.find((m) => m.tenant_id === tenantId);
  const scripts = useResource<AIContent>("/tenant/ai/scripts");
  const media = useResource<AIMedia>("/tenant/ai/media");
  /** block 显示当前可见素材，失效资源明确标记。 */
  function block(item: AIBlock, key: string) {
    const asset = media.items.find((m) => m.id === item.media_id);
    return (
      <div className="ai-preview-block" key={key}>
        {item.type === "text" ? (
          <p>{item.text}</p>
        ) : asset ? (
          <AttachmentView
            attachment={asset.attachment}
            messageType={item.type === "video" ? "file" : undefined}
            identity={{ kind: "agent", actorId: member?.id || "", tenantId }}
          />
        ) : (
          <span>{tr("媒体不可用或已停用")}</span>
        )}
      </div>
    );
  }
  return (
    <div className="ai-plan">
      {plan.handoff && <p>{tr("建议转人工")}</p>}
      {plan.items?.map((item, i) => {
        if (item.type === "text") return <p key={i}>{item.text}</p>;
        const script = scripts.items.find(
          (s) => s.id === item.script_id && s.version === item.version,
        );
        return (
          <Card key={i} size="small" title={script?.title || tr("话术已更新或停用")}>
            {script?.blocks?.map((b, j) => block(b, `${i}:${j}`))}
          </Card>
        );
      })}
    </div>
  );
}
