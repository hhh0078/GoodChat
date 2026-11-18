/** 文件作用说明：在官网与认证页共享可翻译的AI客服产品示意，明确标注演示并支持紧凑布局。 */
import { CheckOutlined, FileTextOutlined, RobotOutlined } from "@/components/public-icons";
import { useTranslator } from "@/i18n/use-translator";

/** AIReplyPreview 展示知识读取和人工协同示意，compact用于认证页较窄的产品展示栏。 */
export function AIReplyPreview({ compact = false }: { compact?: boolean }) {
  const tr = useTranslator();
  return (
    <figure className={`product-illustration public-ai-preview${compact ? " is-compact" : ""}`}>
      <div className="product-window-bar">
        <strong>
          <RobotOutlined />
          {tr("AI客服")}
        </strong>
        <span className="product-status">
          <i />
          {tr("自动回复")}
        </span>
      </div>
      <div className="public-ai-preview-body">
        <aside className="public-ai-preview-knowledge">
          <strong>
            <FileTextOutlined />
            {tr("知识库")}
          </strong>
          <span>{tr("售前咨询")}</span>
          <span className="is-selected">
            <CheckOutlined />
            {tr("订单售后")}
          </span>
          <small>{tr("AI 按需读取相关知识")}</small>
        </aside>
        <div className="public-ai-preview-chat">
          <div className="public-ai-preview-controls">
            <span>{tr("辅助回复")}</span>
            <span>{tr("人工接管")}</span>
          </div>
          <p className="public-ai-preview-visitor">{tr("怎么申请售后服务？")}</p>
          <div className="public-ai-preview-answer">
            <strong>
              <RobotOutlined />
              {tr("AI客服")}
            </strong>
            <p>{tr("我已查阅售后知识库，请提供订单号，我来帮你确认处理方式。")}</p>
            <small>
              <CheckOutlined />
              {tr("已读取相关知识库")}
            </small>
          </div>
        </div>
      </div>
      <figcaption>{tr("产品界面示意 · 非真实业务资料")}</figcaption>
    </figure>
  );
}
