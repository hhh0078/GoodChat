/** 文件作用说明：以服务端可索引内容展示AI客服核心能力、知识读取与人工协作，示意对话明确标注为演示。 */
import {
  FileTextOutlined,
  PictureOutlined,
  RobotOutlined,
  TeamOutlined,
} from "@/components/public-icons";
import { getTranslator } from "@/i18n/server";
import { PublicAIDemo } from "./public-ai-demo";
import Link from "@/i18n/link";

/** PublicAIPreview 展示知识库驱动的自动回复示意，不伪造真实客户对话或承诺业务结果。 */
export function PublicAIPreview() {
  return <PublicAIDemo />;
}

/** PublicAIOverview 将AI自动化放在官网首个功能区，基础免费优势由后续独立区域说明。 */
export async function PublicAIOverview() {
  const tr = await getTranslator();
  const capabilities = [
    {
      icon: FileTextOutlined,
      title: tr("多知识库，按需回答"),
      text: tr("售前、售后和产品资料分别建库。AI 先看描述，再读取相关正文，根据你的业务知识回答。"),
    },
    {
      icon: PictureOutlined,
      title: tr("图文与音视频话术"),
      text: tr("将图片、视频和语音保存为带使用说明的话术，由机器人按授权选择并发送。"),
    },
    {
      icon: RobotOutlined,
      title: tr("访客图片识别"),
      text: tr("识别访客发来的图片，结合上下文回复。图片输入计入实际 AI 用量。"),
    },
    {
      icon: TeamOutlined,
      title: tr("自动或辅助，人工接手"),
      text: tr("可全自动接待，也可先生成草稿让客服确认。人工接管后暂停 AI，避免重复抢答。"),
    },
  ];
  return (
    <section className="public-width public-ai-overview" aria-labelledby="public-ai-heading">
      <header>
        <div className="public-overline">01 / AI CUSTOMER SERVICE</div>
        <h2 id="public-ai-heading">{tr("让 AI 先回答，让人工更从容。")}</h2>
        <p>{tr("从业务知识到真实回复，AI 与你的客服团队协同接待。")}</p>
      </header>
      <div className="public-ai-capability-grid">
        {capabilities.map((item) => (
          <article key={item.title}>
            <Link className="public-card-link" href="/features#section-1">
              <item.icon />
              <h3>{item.title}</h3>
            </Link>
            <p>{item.text}</p>
          </article>
        ))}
      </div>
    </section>
  );
}
