/** 文件作用说明：用可缩放的服务端原生界面示意展示接待、接入、分配和装修能力，所有人物与消息均为示例。 */
import { useTranslator } from "@/i18n/use-translator";
import Image from "next/image";
import { BrandMark } from "./brand-mark";
import {
  ApartmentOutlined,
  ArrowRightOutlined,
  CheckOutlined,
  LinkOutlined,
  MessageOutlined,
  SkinOutlined,
  TeamOutlined,
} from "./public-icons";
type PreviewKind = "workspace" | "links" | "routing" | "design";
/**
 * DemoAvatar 仅在明确标注的营销示意图中展示虚构头像，不用于真实客户或客服资料。
 * name 为示例名称，variant 用于选择示例图片或首字占位。
 */
function DemoAvatar({ name, variant = 0 }: { name: string; variant?: number }) {
  const source =
    variant === 0
      ? "/images/cobalt-avatar-woman.png"
      : variant === 1
        ? "/images/cobalt-avatar-man.png"
        : "";
  return (
    <span className={`product-avatar tone-${variant}`}>
      {source ? (
        <Image src={source} alt="" width={36} height={36} sizes="36px" />
      ) : (
        name.slice(0, 1)
      )}
    </span>
  );
}
/** ProductPreview 按产品能力输出清晰的静态示意，并明确区分示例与真实业务数据。 */
export function ProductPreview({ kind = "workspace" }: { kind?: PreviewKind }) {
  const tr = useTranslator();
  if (kind !== "workspace") {
    return (
      <figure className={`product-illustration product-illustration-${kind}`}>
        <div className="product-mini-chrome">
          <span className="product-mini-brand">
            <BrandMark inverted />
          </span>
          <strong>
            {kind === "links"
              ? tr("接入链接")
              : kind === "routing"
                ? tr("分组与分配")
                : tr("品牌页面装修")}
          </strong>
          <span className="product-mini-label">GoodChat</span>
        </div>
        {kind === "links" ? (
          <div className="product-link-demo">
            <div className="product-mini-title">
              <strong>{tr("每个入口，各司其职")}</strong>
              <span>{tr("独立链接 \u00B7 统一接待")}</span>
            </div>
            {[
              [tr("品牌官网"), tr("售前咨询"), tr("官网访客")],
              [tr("手机商城"), tr("订单售后"), tr("商城客户")],
              [tr("线下门店"), tr("门店服务"), tr("到店客户")],
            ].map(([title, group, source]) => (
              <div className="product-link-row" key={title}>
                <span className="product-mini-icon">
                  <LinkOutlined />
                </span>
                <div>
                  <strong>{title}</strong>
                  <small>
                    {group} · {source}
                  </small>
                </div>
                <span className="product-status">
                  <i />
                  {tr("已启用 ")}
                </span>
              </div>
            ))}
            <div className="product-link-address">
              goodchat / c / your-brand <LinkOutlined />
            </div>
          </div>
        ) : kind === "routing" ? (
          <div className="product-routing-demo">
            <div className="product-route-node">
              <MessageOutlined />
              <strong>{tr("新的客户咨询")}</strong>
              <small>{tr("来自你的业务入口")}</small>
            </div>
            <div className="product-route-connector" />
            <div className="product-route-rule">
              <ApartmentOutlined />
              <div>
                <strong>{tr("找到合适的接待人")}</strong>
                <small>{tr("业务组 \u00B7 技能 \u00B7 在线状态 \u00B7 接待容量")}</small>
              </div>
            </div>
            <div className="product-route-connector" />
            <div className="product-route-agents">
              {[tr("林可"), tr("陈安"), tr("周一")].map((name, index) => (
                <div key={name}>
                  <DemoAvatar name={name} variant={index} />
                  <strong>{name}</strong>
                  <small>{index === 0 ? tr("本次接待") : tr("团队成员")}</small>
                  {index === 0 && <CheckOutlined />}
                </div>
              ))}
            </div>
          </div>
        ) : (
          <div className="product-design-demo">
            <div className="product-design-tools">
              <strong>{tr("品牌页面设计")}</strong>
              <span className="selected">
                <SkinOutlined />
                {tr(" 样式 ")}
              </span>
              <span>
                <MessageOutlined />
                {tr(" 欢迎语 ")}
              </span>
              <span>
                <TeamOutlined />
                {tr(" 联系信息 ")}
              </span>
              <span>
                <LinkOutlined />
                {tr(" 安装代码 ")}
              </span>
            </div>
            <div className="product-design-stage">
              <Image
                src="/images/cobalt-service-architecture.png"
                alt=""
                fill
                sizes="(max-width:768px) 100vw, 60vw"
              />
              <span className="product-design-stage-label">{tr("页面预览")}</span>
              <div className="product-design-phone">
                <span className="product-mini-brand">
                  <BrandMark inverted />
                </span>
                <strong>{tr("欢迎咨询 GoodChat")}</strong>
                <small>{tr("我们很高兴为你提供帮助")}</small>
                <p>{tr("你好，有什么可以帮助你？")}</p>
                <span className="product-mini-submit">{tr("开始对话")}</span>
              </div>
            </div>
            <div className="product-design-properties">
              <strong>{tr("主题设置")}</strong>
              <span>{tr("主色调")}</span>
              <div className="product-color-swatches" aria-hidden="true">
                <i />
                <i />
                <i />
                <i />
              </div>
              <span>{tr("品牌名称")}</span>
              <div className="product-property-value">GoodChat</div>
              <span>{tr("欢迎语")}</span>
              <div className="product-property-value">{tr("你好，有什么可以帮助你？")}</div>
            </div>
          </div>
        )}
        <figcaption>{tr("产品界面示意 \u00B7 非真实业务资料")}</figcaption>
      </figure>
    );
  }
  return (
    <figure className="product-illustration product-workspace">
      <div className="product-window-bar">
        <strong>
          <MessageOutlined />
          {tr(" 客服工作台 ")}
        </strong>
        <span className="product-status">
          <i />
          {tr("团队协作，有序接待 ")}
        </span>
      </div>
      <div className="product-workspace-body">
        <aside className="product-preview-nav" aria-hidden="true">
          <span className="product-mini-brand">
            <BrandMark inverted />
          </span>
          <span className="selected">
            <MessageOutlined />
          </span>
          <TeamOutlined />
          <LinkOutlined />
          <SkinOutlined />
        </aside>
        <div className="product-preview-inbox">
          <div className="product-inbox-heading">
            <strong>{tr("全部咨询")}</strong>
            <span>3</span>
          </div>
          <div className="product-search-placeholder">{tr("搜索客户与会话")}</div>
          <div className="product-inbox-tabs">
            <strong>{tr("接待中")}</strong>
            <span>{tr("待分配")}</span>
            <span>{tr("已结束")}</span>
          </div>
          {[
            [tr("林可"), tr("想了解一下你们的服务"), tr("刚刚")],
            [tr("陈安"), tr("好的，谢谢你的帮助"), "10:24"],
            [tr("周一"), tr("请问如何开始使用？"), "10:18"],
          ].map(([name, text, time], index) => (
            <div className={`product-inbox-item ${index === 0 ? "selected" : ""}`} key={name}>
              <DemoAvatar name={name} variant={index} />
              <div>
                <strong>
                  {name}
                  <time>{time}</time>
                </strong>
                <p>{text}</p>
              </div>
            </div>
          ))}
          <div className="product-inbox-footer">
            <i />
            {tr("当前在线，准备好接待 ")}
          </div>
        </div>
        <div className="product-preview-conversation">
          <div className="product-conversation-heading">
            <div>
              <strong>{tr("林可")}</strong>
              <small>
                <i />
                {tr("来自品牌官网 \u00B7 售前咨询 ")}
              </small>
            </div>
            <span className="product-mini-action">{tr("转接")}</span>
            <span className="product-mini-action">{tr("结束会话")}</span>
          </div>
          <div className="product-conversation-messages">
            <span className="product-date-divider">{tr("今天 10:24")}</span>
            <div className="product-demo-message">
              <DemoAvatar name={tr("林可")} />
              <div>
                <small>{tr("林可")}</small>
                <p>{tr("你好，想了解一下你们的服务。")}</p>
              </div>
            </div>
            <div className="product-demo-message own">
              <DemoAvatar name={tr("客服小顾")} />
              <div>
                <small>{tr("客服小顾")}</small>
                <p>{tr("您好，很高兴为您解答。")}</p>
                <em>{tr("已读")}</em>
              </div>
            </div>
          </div>
          <div className="product-preview-composer">
            <div>
              <strong>{tr("回复客户")}</strong>
              <span>{tr("内部备注")}</span>
            </div>
            <p>{tr("输入消息，开启一段好的对话\u2026")}</p>
            <footer>
              <span>
                <MessageOutlined /> <LinkOutlined />
              </span>
              <span className="product-mini-submit">
                {tr("发送 ")}
                <ArrowRightOutlined />
              </span>
            </footer>
          </div>
        </div>
        <aside className="product-preview-contact">
          <strong>{tr("客户资料")}</strong>
          <div className="product-contact-identity">
            <DemoAvatar name={tr("林可")} />
            <strong>{tr("林可")}</strong>
            <small>{tr("新客户")}</small>
          </div>
          <dl>
            <dt>{tr("咨询来源")}</dt>
            <dd>{tr("品牌官网")}</dd>
            <dt>{tr("接待分组")}</dt>
            <dd>{tr("售前咨询")}</dd>
            <dt>{tr("客户标签")}</dt>
            <dd>
              <span>{tr("产品咨询")}</span>
            </dd>
          </dl>
        </aside>
      </div>
      <figcaption>{tr("产品界面示意 \u00B7 示例人物与聊天内容")}</figcaption>
    </figure>
  );
}
