/** 文件作用说明：提供产品、场景、套餐、帮助与隐私条款的服务端公开页面。 */
import type { Metadata } from "next";
import Image from "next/image";
import { notFound } from "next/navigation";
import { Button } from "antd";

const pages: Record<
  string,
  { title: string; lead: string; sections: { title: string; text: string; points?: string[] }[] }
> = {
  features: {
    title: "让服务流程完整连接",
    lead: "客户入口、客服分配、沟通记录和页面设计，放进同一个工作空间。",
    sections: [
      {
        title: "从多个链接接入",
        text: "为售前、售后、门店和投放建立独立链接，分别配置来源、接待组、分配池与页面。手机和电脑使用相同业务规则。",
        points: ["独立聊天链接与二维码", "网站嵌入与访客资料参数", "来源与会话记录分别保留"],
      },
      {
        title: "按团队规则安排接待",
        text: "结合组别、人员状态和容量分配咨询，提供自动分配、人工领取、指定客服与转接。只有具备权限的成员才能访问对应会话。",
      },
      {
        title: "把页面做成自己的品牌",
        text: "使用基础设置、可视化布局或代码装修创建页面。AI 创作保存在本租户模板中，经预览校验后发布，已有版本可回滚。",
      },
    ],
  },
  solutions: {
    title: "为不同业务安排接待",
    lead: "用入口和接待范围组织咨询，让客户进入合适的服务流程。",
    sections: [
      {
        title: "网站与 App 业务",
        text: "网站通过聊天链接或嵌入入口发起咨询。需要传入业务账号和订单时，由商户后端提供可信身份，公开手机号不作为历史访问凭证。",
      },
      {
        title: "售前与售后团队",
        text: "分别建立售前、售后服务空间和入口，配置客服组与分配池。客服可以查看允许范围内的来源、备注与聊天记录。",
      },
      {
        title: "多品牌与门店",
        text: "不同品牌或门店使用独立链接与装修。平台管理租户，租户维护成员、客户与私有模板，各家的业务数据分别授权。",
      },
    ],
  },
  help: {
    title: "从第一个聊天链接开始",
    lead: "注册、配置、分享链接，然后让团队开始接待。",
    sections: [
      {
        title: "1 创建工作空间",
        text: "使用邮箱验证码完成注册，填写租户名称并设置密码。注册人是租户所有者兼管理员，可独立设置是否参与接待。",
      },
      {
        title: "2 添加接待成员",
        text: "在控制台邀请管理员或客服。受邀成员验证相同邮箱后加入当前租户，不会重复创建另一家租户。",
      },
      {
        title: "3 创建并分享入口",
        text: "选择客服组和分配池，创建接入链接。可以复制链接、二维码或网站嵌入代码，随后在另一浏览器中测试访客咨询。",
      },
      {
        title: "4 装修与发布",
        text: "选择平台模板，手动编辑或用 AI 创建租户私有模板。预览电脑与手机效果，发布后绑定入口；保存草稿不会直接改变线上页面。",
      },
      {
        title: "收不到验证码时",
        text: "先核对邮箱与垃圾邮件目录。等待重发间隔后再获取；页面显示服务器受理不等于收件箱送达。服务不可用时会明确提示，不会返回通用验证码。",
      },
    ],
  },
  privacy: {
    title: "隐私政策",
    lead: "本页说明 GoodChat 网页服务涉及的信息和管理方式。正式运营主体、联系渠道及保留期限将在上线配置时公布。",
    sections: [
      {
        title: "我们处理的信息",
        text: "账号邮箱、显示名称和所属租户用于登录及权限管理；访客提交的消息、联系方式、附件和来源用于对应租户接待。必要的操作与安全日志用于故障排查和防止越权。",
      },
      {
        title: "租户数据与访问",
        text: "租户成员依据角色和接待权限访问业务数据。客户公开填写的手机号、昵称或链接参数不会直接授予历史聊天访问权。",
      },
      {
        title: "AI 页面设计",
        text: "AI 装修处理租户主动提交的设计说明和素材，不默认提交真实聊天记录。使用外部模型前应检查其服务条款与数据处理方式。",
      },
      {
        title: "管理与删除",
        text: "账号与租户管理员通过控制台管理资料和访问权限。保留、导出、删除和停用规则以平台公布的政策及适用合同为准。浏览器中的访客身份可通过清理浏览器数据移除，但这不会自动删除服务端记录。",
      },
    ],
  },
  terms: {
    title: "服务条款",
    lead: "使用 GoodChat 前，请了解账号、租户管理与内容发布的基本约定。正式商业条款将在运营信息配置后公布。",
    sections: [
      {
        title: "账号与权限",
        text: "请使用本人或组织授权的邮箱创建账号，妥善保管密码与验证码。租户管理员负责成员角色、接待范围和对外链接配置。公开注册不能获取平台管理员权限。",
      },
      {
        title: "聊天与页面内容",
        text: "租户对其上传的品牌素材、聊天页面与自定义代码拥有相应使用授权，并对发布内容负责。装修需要通过运行与权限校验，不得绕过访问控制或滥用其他租户资料。",
      },
      {
        title: "服务范围与费用",
        text: "具体能力、席位、空间、AI 用量和有效期以控制台与约定为准。尚未上线的原生客户端、第三方渠道和支付能力不作为当前已交付服务。",
      },
      {
        title: "通知与可用性",
        text: "服务可发送验证码、邀请及必要业务通知。邮件、浏览器通知和外部 AI 受对应渠道能力影响，页面会区分失败与结果不确定状态。维护和异常处理以实际通知为准。",
      },
    ],
  },
};
/** generateStaticParams 只生成已定义的公开信息页面。 */
export function generateStaticParams() {
  return Object.keys(pages).map((slug) => ({ slug }));
}
/** generateMetadata 为每个公开页面提供独立标题和规范地址。 */
export async function generateMetadata({
  params,
}: {
  params: Promise<{ slug: string }>;
}): Promise<Metadata> {
  const { slug } = await params;
  const page = pages[slug];
  if (!page) return {};
  return {
    title: page.title,
    description: page.lead,
    alternates: { canonical: `/${slug}` },
    openGraph: { title: page.title, description: page.lead, url: `/${slug}` },
  };
}
/** PublicContentPage 输出可被搜索引擎读取的产品与服务说明。 */
export default async function PublicContentPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const page = pages[slug];
  if (!page) notFound();
  const illustrated = slug === "features" || slug === "solutions";
  const legal = slug === "privacy" || slug === "terms";
  const previews = [
    { path: "/images/access-links-preview.png", label: "接入链接与分享界面示意" },
    { path: "/images/routing-preview.png", label: "团队接待与分配规则界面示意" },
    { path: "/images/editor-preview.png", label: "品牌页面装修界面示意" },
  ];
  return (
    <div className="public-width public-content" data-public-page={slug}>
      <header className="public-content-heading">
        <div className="public-overline">GOODCHAT / {slug.toUpperCase()}</div>
        <h1>{page.title}</h1>
        <p className="page-lead">{page.lead}</p>
      </header>
      <article className={`public-docs-layout ${legal ? "public-legal" : ""}`}>
        <nav className="public-toc" aria-label="本页目录">
          <strong>{slug === "help" ? "起步指南" : legal ? page.title : "内容目录"}</strong>
          {page.sections.map((section, index) => (
            <a href={`#section-${index + 1}`} key={section.title}>
              {section.title}
            </a>
          ))}
        </nav>
        <div className="public-docs-body">
          {page.sections.map((section, index) => (
            <section
              className={`public-docs-section ${illustrated ? "has-preview" : ""}`}
              id={`section-${index + 1}`}
              key={section.title}
            >
              <div className="public-docs-copy">
                <span className="public-section-number">{String(index + 1).padStart(2, "0")}</span>
                <div>
                  <h2>{section.title}</h2>
                  <p>{section.text}</p>
                  {section.points && (
                    <ul>
                      {section.points.map((point) => (
                        <li key={point}>{point}</li>
                      ))}
                    </ul>
                  )}
                </div>
              </div>
              {illustrated && previews[index] && (
                <figure className="public-detail-preview">
                  <Image
                    src={previews[index].path}
                    alt={previews[index].label}
                    width={1488}
                    height={1056}
                    sizes="(max-width: 768px) calc(100vw - 40px), 40vw"
                  />
                  <figcaption>界面示意 · 非真实业务资料</figcaption>
                </figure>
              )}
            </section>
          ))}
          {!legal && (
            <div className="public-docs-cta">
              <Button type="primary" size="large" href="/register">
                创建工作空间
              </Button>
            </div>
          )}
        </div>
      </article>
    </div>
  );
}
