/** 文件作用说明：用独立外部AI编辑和导入ZIP的操作向导替代后台在线生成代码入口。 */
"use client";
import { useSession } from "@/features/auth/session";
import { TemplateImport } from "@/features/assistant/template-import";
import { useTranslator } from "@/i18n/use-translator";
import { Button, Modal, Steps } from "antd";
import { SkinOutlined } from "@ant-design/icons";
import { useState } from "react";
/** AIDecorationGuide 展示版本化模板的外部编辑路径，入口直接复用真实导入和下载能力。 */
export function AIDecorationGuide({ open, onClose }: { open: boolean; onClose: () => void }) {
  const tr = useTranslator();
  const { tenantId } = useSession();
  return (
    <Modal
      title={tr("用 AI 制作专属客服页面")}
      open={open}
      onCancel={onClose}
      footer={null}
      width={680}
    >
      <p className="muted">
        {tr("用你熟悉的 AI 工具设计页面，聊天能力由 GoodChat 官方核心提供。")}
      </p>
      <Steps
        orientation="vertical"
        size="small"
        items={[
          { title: tr("复制提示词，下载基础模板 ZIP。") },
          { title: tr("打开 AI 工具，上传 ZIP，粘贴提示词并描述想要的效果。") },
          { title: tr("等待 AI 返回带新版本号的完整 ZIP，再到这里上传。") },
          { title: tr("导入后预览并测试；不满意就让 AI 继续修改，再上传新版本。") },
        ]}
      />
      <div style={{ marginTop: 20 }}>
        <TemplateImport tenantId={tenantId} onImported={onClose} />
      </div>
    </Modal>
  );
}
/** AIDecorationEntry 提供管理员的小型辅助入口，不覆盖AI客服主导航与日常接待操作。 */
export function AIDecorationEntry() {
  const tr = useTranslator();
  const { current, tenantId } = useSession();
  const member = current.memberships.find((x) => x.tenant_id === tenantId);
  const [open, setOpen] = useState(false);
  if (!member || !["owner", "admin"].includes(member.role)) return null;
  return (
    <>
      <Button
        className="ai-decoration-guide-button"
        aria-label={tr("AI 装修指南")}
        title={tr("AI 装修指南")}
        type="text"
        size="small"
        icon={<SkinOutlined />}
        onClick={() => setOpen(true)}
      >
        {tr("AI 装修指南")}
      </Button>
      <AIDecorationGuide open={open} onClose={() => setOpen(false)} />
    </>
  );
}
