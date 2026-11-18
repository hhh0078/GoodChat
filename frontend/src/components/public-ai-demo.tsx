/** 文件作用说明：按聊天视觉稿逐条追加示例消息，自动衔接场景并保留最近对话，限制卡片高度。 */
"use client";
import { useTranslator } from "@/i18n/use-translator";
import { FileTextOutlined, PictureOutlined, TeamOutlined, UserOutlined } from "@ant-design/icons";
import Image from "next/image";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { BrandMark } from "./brand-mark";

type ConversationItem =
  | { kind: "separator"; text: string }
  | {
      kind: "message";
      role: "visitor" | "ai";
      text: string;
      source?: string;
      recognition?: boolean;
      image?: boolean;
      handoff?: boolean;
    };

/** PublicAIDemo 用连续对话呈现知识回复、上下文理解、图片识别和人工协作，不操作真实会话。 */
export function PublicAIDemo() {
  const tr = useTranslator();
  const host = useRef<HTMLElement>(null);
  const viewport = useRef<HTMLDivElement>(null);
  const following = useRef(true);
  const [shown, setShown] = useState(2);
  const [visible, setVisible] = useState(false);
  const [reduced, setReduced] = useState(false);
  const items: ConversationItem[] = [
    { kind: "separator", text: tr("订单售后") },
    { kind: "message", role: "visitor", text: tr("刚收到耳机，拆开包装后还能退吗？") },
    {
      kind: "message",
      role: "ai",
      source: tr("已查阅《退换货政策》"),
      text: tr("可以拆封检查。请保留原包装、配件和赠品，商品完好且不影响二次销售即可申请。"),
    },
    { kind: "message", role: "visitor", text: tr("好的，怎么提交申请？") },
    { kind: "message", role: "ai", text: tr("请提供订单号，我来帮你确认申请方式。") },
    { kind: "separator", text: tr("访客图片识别") },
    {
      kind: "message",
      role: "visitor",
      image: true,
      text: tr("收到的耳机就是这样，可以帮我看一下吗？"),
    },
    {
      kind: "message",
      role: "ai",
      source: tr("已识别图片内容"),
      recognition: true,
      text: tr("看到左侧耳罩有脱落。建议先停止使用，我可以为你转接人工核实售后。"),
    },
    { kind: "message", role: "visitor", text: tr("帮我转人工吧。") },
    {
      kind: "message",
      role: "ai",
      handoff: true,
      text: tr("好的，正在转接。照片和这段对话会一起保留，客服可以接着处理。"),
    },
  ];
  // 只回收八段对话以前的屏幕外记录，当前消息和前一段对话连续保留。
  const count = reduced ? 5 : shown;
  const first = Math.max(0, count - 40);
  const history = Array.from({ length: count - first }, (_, index) => first + index);
  const next = items[shown % items.length];
  const playing = visible && !reduced;
  const typing = playing && next.kind === "message" && next.role === "ai";
  const delay =
    next.kind === "separator" ? 5200 : next.role === "ai" ? 2400 : shown % 5 === 1 ? 800 : 3400;

  useEffect(() => {
    const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
    let inView = false;
    /** reconcile 同步视口、后台状态与系统偏好，不可见时不继续追加消息。 */
    function reconcile() {
      setReduced(preference.matches);
      setVisible(inView && !document.hidden);
    }
    const observer = new IntersectionObserver(
      ([entry]) => {
        inView = entry?.isIntersecting ?? false;
        reconcile();
      },
      { threshold: 0.15 },
    );
    if (host.current) observer.observe(host.current);
    preference.addEventListener("change", reconcile);
    document.addEventListener("visibilitychange", reconcile);
    reconcile();
    return () => {
      observer.disconnect();
      preference.removeEventListener("change", reconcile);
      document.removeEventListener("visibilitychange", reconcile);
    };
  }, []);

  useEffect(() => {
    if (!playing) return;
    const timer = window.setTimeout(() => setShown((value) => value + 1), delay);
    return () => window.clearTimeout(timer);
  }, [playing, shown, delay]);

  useLayoutEffect(() => {
    const list = viewport.current;
    if (list && following.current)
      list.scrollTo({
        top: list.scrollHeight,
        behavior: reduced || shown === 2 ? "auto" : "smooth",
      });
  }, [shown, reduced, typing]);

  /** updateFollowing 用户向上阅读历史时保留位置，回到底部后再跟随新消息。 */
  function updateFollowing() {
    const list = viewport.current;
    if (list) following.current = list.scrollHeight - list.clientHeight - list.scrollTop < 60;
  }

  return (
    <figure className="product-illustration ai-conversation" id="ai-demo" ref={host}>
      <header className="ai-conversation-header">
        <span className="ai-conversation-brand">
          <BrandMark inverted size={24} />
        </span>
        <strong>GoodChat AI</strong>
        <span className="ai-conversation-online">
          <span aria-hidden="true" />
          {tr("在线")}
        </span>
      </header>
      <div
        className="ai-conversation-scroll"
        ref={viewport}
        onScroll={updateFollowing}
        role="log"
        aria-live="off"
        aria-label={tr("AI客服对话演示")}
        tabIndex={0}
        data-message-count={count}
      >
        <ol className="ai-conversation-history">
          {history.map((id) => {
            const item = items[id % items.length];
            if (item.kind === "separator")
              return (
                <li className="ai-conversation-divider" key={id} data-conversation-entry={id}>
                  <span>{item.text}</span>
                </li>
              );
            return (
              <li
                className={"ai-conversation-message is-" + item.role}
                key={id}
                data-conversation-entry={id}
              >
                <span className={"ai-conversation-avatar is-" + item.role} aria-hidden="true">
                  {item.role === "ai" ? <BrandMark inverted size={20} /> : <UserOutlined />}
                </span>
                <div className="ai-conversation-content">
                  <span className="ai-conversation-name">
                    {item.role === "ai" ? "GoodChat AI" : tr("访客")}
                  </span>
                  <div className="ai-conversation-bubble">
                    {item.source && (
                      <span className="ai-conversation-source">
                        {item.recognition ? <PictureOutlined /> : <FileTextOutlined />}
                        {item.source}
                      </span>
                    )}
                    {item.image && (
                      <Image
                        className="ai-conversation-photo"
                        src="/images/ai-demo-headphones.png"
                        alt={tr("示例照片：左侧耳罩脱落的白色耳机")}
                        width={160}
                        height={120}
                        sizes="160px"
                      />
                    )}
                    <p>{item.text}</p>
                    {item.handoff && (
                      <span className="ai-conversation-source is-handoff">
                        <TeamOutlined />
                        {tr("人工接管后，AI 暂停回复")}
                      </span>
                    )}
                  </div>
                </div>
              </li>
            );
          })}
          {typing && (
            <li className="ai-conversation-typing" aria-label={tr("生成回复")}>
              <span />
              <span />
              <span />
            </li>
          )}
        </ol>
      </div>
      <figcaption>{tr("产品演示 · 非真实咨询")}</figcaption>
    </figure>
  );
}
