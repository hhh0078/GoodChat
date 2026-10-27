/** 文件作用说明：从真实模板结构呈现只读缩略内容，代码模板仅展示类型信息并保持脚本隔离。 */
import { Avatar, Tag } from "antd";
import { CodeOutlined, FileZipOutlined, MessageOutlined } from "@ant-design/icons";
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

/** TemplatePreview 展示模板已有品牌和组件内容，不执行代码或伪造实际聊天预览。 */
export function TemplatePreview({ template }: { template: Template }) {
  const design = template.design;
  const accent = /^#[\da-f]{6}$/i.test(design?.theme?.primary_color || "")
    ? design.theme!.primary_color
    : "#3c7958";
  if (template.render_mode === "code") {
    return (
      <div className="studio-template-cover studio-code-cover">
        {design?.package_id ? <FileZipOutlined /> : <CodeOutlined />}
        <strong>{design?.package_id ? "静态网站资源包" : "HTML · CSS · JavaScript"}</strong>
        <p>完整代码通过独立来源预览</p>
        <Tag>{design?.package_id ? "多文件代码模板" : "内联代码模板"}</Tag>
      </div>
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
