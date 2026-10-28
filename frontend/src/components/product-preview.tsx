/** 文件作用说明：用可缩放的服务端原生界面示意展示接待、接入、分配和装修能力，所有人物与消息均为示例。 */
import Image from "next/image";
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
  if (kind !== "workspace") {
    return (
      <figure className={`product-illustration product-illustration-${kind}`}>
        <div className="product-mini-chrome">
          <span className="product-mini-brand">
            <MessageOutlined />
          </span>
          <strong>
            {kind === "links" ? "接入链接" : kind === "routing" ? "分组与分配" : "品牌页面装修"}
          </strong>
          <span className="product-mini-label">GoodChat</span>
        </div>
        {kind === "links" ? (
          <div className="product-link-demo">
            <div className="product-mini-title">
              <strong>每个入口，各司其职</strong>
              <span>独立链接 · 统一接待</span>
            </div>
            {[
              ["品牌官网", "售前咨询", "官网访客"],
              ["手机商城", "订单售后", "商城客户"],
              ["线下门店", "门店服务", "到店客户"],
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
                  已启用
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
              <strong>新的客户咨询</strong>
              <small>来自你的业务入口</small>
            </div>
            <div className="product-route-connector" />
            <div className="product-route-rule">
              <ApartmentOutlined />
              <div>
                <strong>找到合适的接待人</strong>
                <small>业务组 · 技能 · 在线状态 · 接待容量</small>
              </div>
            </div>
            <div className="product-route-connector" />
            <div className="product-route-agents">
              {["林可", "陈安", "周一"].map((name, index) => (
                <div key={name}>
                  <DemoAvatar name={name} variant={index} />
                  <strong>{name}</strong>
                  <small>{index === 0 ? "本次接待" : "团队成员"}</small>
                  {index === 0 && <CheckOutlined />}
                </div>
              ))}
            </div>
          </div>
        ) : (
          <div className="product-design-demo">
            <div className="product-design-tools">
              <strong>品牌页面设计</strong>
              <span className="selected">
                <SkinOutlined /> 样式
              </span>
              <span>
                <MessageOutlined /> 欢迎语
              </span>
              <span>
                <TeamOutlined /> 联系信息
              </span>
              <span>
                <LinkOutlined /> 安装代码
              </span>
            </div>
            <div className="product-design-stage">
              <Image
                src="/images/cobalt-service-architecture.png"
                alt=""
                fill
                sizes="(max-width:768px) 100vw, 60vw"
              />
              <span className="product-design-stage-label">页面预览</span>
              <div className="product-design-phone">
                <span className="product-mini-brand">
                  <MessageOutlined />
                </span>
                <strong>欢迎咨询 GoodChat</strong>
                <small>我们很高兴为你提供帮助</small>
                <p>你好，有什么可以帮助你？</p>
                <span className="product-mini-submit">开始对话</span>
              </div>
            </div>
            <div className="product-design-properties">
              <strong>主题设置</strong>
              <span>主色调</span>
              <div className="product-color-swatches" aria-hidden="true">
                <i />
                <i />
                <i />
                <i />
              </div>
              <span>品牌名称</span>
              <div className="product-property-value">GoodChat</div>
              <span>欢迎语</span>
              <div className="product-property-value">你好，有什么可以帮助你？</div>
            </div>
          </div>
        )}
        <figcaption>产品界面示意 · 非真实业务资料</figcaption>
      </figure>
    );
  }
  return (
    <figure className="product-illustration product-workspace">
      <div className="product-window-bar">
        <strong>
          <MessageOutlined /> 客服工作台
        </strong>
        <span className="product-status">
          <i />
          团队协作，有序接待
        </span>
      </div>
      <div className="product-workspace-body">
        <aside className="product-preview-nav" aria-hidden="true">
          <span className="product-mini-brand">
            <MessageOutlined />
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
            <strong>全部咨询</strong>
            <span>3</span>
          </div>
          <div className="product-search-placeholder">搜索客户与会话</div>
          <div className="product-inbox-tabs">
            <strong>接待中</strong>
            <span>待分配</span>
            <span>已结束</span>
          </div>
          {[
            ["林可", "想了解一下你们的服务", "刚刚"],
            ["陈安", "好的，谢谢你的帮助", "10:24"],
            ["周一", "请问如何开始使用？", "10:18"],
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
            当前在线，准备好接待
          </div>
        </div>
        <div className="product-preview-conversation">
          <div className="product-conversation-heading">
            <div>
              <strong>林可</strong>
              <small>
                <i />
                来自品牌官网 · 售前咨询
              </small>
            </div>
            <span className="product-mini-action">转接</span>
            <span className="product-mini-action">结束会话</span>
          </div>
          <div className="product-conversation-messages">
            <span className="product-date-divider">今天 10:24</span>
            <div className="product-demo-message">
              <DemoAvatar name="林可" />
              <div>
                <small>林可</small>
                <p>你好，想了解一下你们的服务。</p>
              </div>
            </div>
            <div className="product-demo-message own">
              <DemoAvatar name="客服小顾" />
              <div>
                <small>客服小顾</small>
                <p>您好，很高兴为您解答。</p>
                <em>已读</em>
              </div>
            </div>
          </div>
          <div className="product-preview-composer">
            <div>
              <strong>回复客户</strong>
              <span>内部备注</span>
            </div>
            <p>输入消息，开启一段好的对话…</p>
            <footer>
              <span>
                <MessageOutlined /> <LinkOutlined />
              </span>
              <span className="product-mini-submit">
                发送 <ArrowRightOutlined />
              </span>
            </footer>
          </div>
        </div>
        <aside className="product-preview-contact">
          <strong>客户资料</strong>
          <div className="product-contact-identity">
            <DemoAvatar name="林可" />
            <strong>林可</strong>
            <small>新客户</small>
          </div>
          <dl>
            <dt>咨询来源</dt>
            <dd>品牌官网</dd>
            <dt>接待分组</dt>
            <dd>售前咨询</dd>
            <dt>客户标签</dt>
            <dd>
              <span>产品咨询</span>
            </dd>
          </dl>
        </aside>
      </div>
      <figcaption>产品界面示意 · 示例人物与聊天内容</figcaption>
    </figure>
  );
}
