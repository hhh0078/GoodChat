/** 文件作用说明：按真实行业制作模板设计封面，结构化模板展示已有组件，所有代码保持隔离执行。 */
import { Avatar } from "antd";
import {
  ArrowRightOutlined,
  BankOutlined,
  CustomerServiceOutlined,
  HomeOutlined,
  MedicineBoxOutlined,
  MessageOutlined,
  ReadOutlined,
  ShoppingOutlined,
} from "@ant-design/icons";
import type { CSSProperties } from "react";
import type { Template } from "./templates";

const blockNames: Record<string, string> = {
  welcome: "欢迎说明",
  text: "文字说明",
  chat: "聊天区域",
  links: "服务链接",
  faq: "常见问题",
  image: "图片内容",
};
const industryCovers = [
  { key: "finance", name: "金融咨询", caption: "TRUST & CARE", Icon: BankOutlined },
  {
    key: "commerce",
    name: "电商售后",
    caption: "GOOD THINGS, GOOD SERVICE",
    Icon: ShoppingOutlined,
  },
  { key: "education", name: "教育咨询", caption: "A NEW CHAPTER", Icon: ReadOutlined },
  { key: "medical", name: "医疗服务", caption: "CARE WITH YOU", Icon: MedicineBoxOutlined },
  { key: "property", name: "房产顾问", caption: "A PLACE TO CALL HOME", Icon: HomeOutlined },
  {
    key: "business",
    name: "企业服务",
    caption: "FROM IDEA TO IMPACT",
    Icon: CustomerServiceOutlined,
  },
];
const customCover = {
  key: "custom",
  name: "专属设计",
  caption: "YOUR BRAND, YOUR SERVICE",
  Icon: MessageOutlined,
};

/** TemplatePreview 展示模板已有品牌和组件内容，不执行代码或伪造实际聊天预览。 */
export function TemplatePreview({
  template,
  onPreview,
}: {
  template: Template;
  onPreview?: () => void;
}) {
  const design = template.design;
  const accent = /^#[\da-f]{6}$/i.test(design?.theme?.primary_color || "")
    ? design.theme!.primary_color
    : "#3c7958";
  if (template.render_mode === "code") {
    const cover = industryCovers.find((item) => item.key === template.industry_key) || customCover;
    const Icon = cover.Icon;
    const className = `studio-template-cover studio-industry-cover industry-${cover.key}${onPreview ? " is-interactive" : ""}`;
    const content = (
      <>
        <span className="studio-cover-heading">
          <span>{cover.name}</span>
          <small>客服模板</small>
        </span>
        <span className="studio-cover-emblem" aria-hidden="true">
          <Icon />
        </span>
        <span className="studio-cover-copy">
          <small>{cover.caption}</small>
          <strong>{template.name || "专属客服页面"}</strong>
        </span>
        <span className="studio-cover-footer">
          <small>设计封面</small>
          <span>
            {onPreview ? (
              <>
                点击预览完整页面 <ArrowRightOutlined />
              </>
            ) : (
              "品牌在线服务"
            )}
          </span>
        </span>
      </>
    );
    return onPreview ? (
      <button
        type="button"
        className={className}
        onClick={onPreview}
        aria-label={`${template.name || "专属客服页面"}，预览完整页面`}
      >
        {content}
      </button>
    ) : (
      <div className={className}>{content}</div>
    );
  }
  return (
    <div className="studio-template-cover" style={{ "--template-accent": accent } as CSSProperties}>
      <div className="studio-template-document">
        <header>
          <Avatar
            src={design?.brand?.logo_url || undefined}
            size={30}
            style={{ background: "var(--template-accent)" }}
          >
            {(design?.brand?.title || String(template.name || "在线咨询")).slice(0, 1)}
          </Avatar>
          <div>
            <strong>{design?.brand?.title || String(template.name || "在线咨询")}</strong>
            {design?.brand?.subtitle && <small>{design.brand.subtitle}</small>}
          </div>
        </header>
        <div className="studio-template-blocks">
          {(design?.blocks || []).slice(0, 3).map((block) => (
            <div key={block.id} className={block.type === "chat" ? "studio-template-chat" : ""}>
              {block.type === "chat" && <MessageOutlined />}
              <strong>{block.title || blockNames[block.type] || block.type}</strong>
              {block.content && <p>{block.content}</p>}
            </div>
          ))}
          {!design?.blocks?.length && <p className="muted">尚未添加页面组件</p>}
        </div>
        <footer>模板结构预览</footer>
      </div>
    </div>
  );
}
