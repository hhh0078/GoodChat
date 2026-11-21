/** 文件作用说明：连续追加富消息演示，模拟访客输入、选项、表单及媒体话术，所有交互仅保存在当前页面。 */
"use client";
import { useTranslator } from "@/i18n/use-translator";
import {
  CheckOutlined,
  DownloadOutlined,
  FileTextOutlined,
  PictureOutlined,
  PlusOutlined,
  SendOutlined,
  TeamOutlined,
  UserOutlined,
} from "@ant-design/icons";
import Image from "next/image";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { BrandMark } from "./brand-mark";

type RichKind = "options" | "form" | "form_result" | "file" | "audio" | "video";
type FormValues = { order: string; details: string; contact: string };
type ConversationItem =
  | { kind: "separator"; text: string }
  | { kind: "action" }
  | {
      kind: "message";
      role: "visitor" | "ai";
      text: string;
      source?: string;
      recognition?: boolean;
      image?: boolean;
      handoff?: boolean;
      rich?: RichKind;
      choice?: boolean;
    };

/** PublicAIDemo 呈现完整回复过程及本地可操作示例，不调用真实会话、AI、上传或表单接口。 */
export function PublicAIDemo() {
  const tr = useTranslator();
  const host = useRef<HTMLElement>(null),
    viewport = useRef<HTMLDivElement>(null),
    following = useRef(true);
  const [shown, setShown] = useState(1),
    [draftLength, setDraftLength] = useState(0);
  const [visible, setVisible] = useState(false),
    [reduced, setReduced] = useState(false);
  const [editing, setEditing] = useState<number | null>(null);
  const [choices, setChoices] = useState<Record<number, string>>({});
  const [forms, setForms] = useState<Record<number, Partial<FormValues>>>({});
  const options = [tr("耳罩脱落"), tr("无法开机"), tr("其他问题")];
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
    { kind: "message", role: "ai", rich: "options", text: tr("请选择需要处理的问题。") },
    { kind: "message", role: "visitor", choice: true, text: options[0] },
    { kind: "message", role: "ai", rich: "form", text: tr("请提供订单号，我来帮你确认申请方式。") },
    { kind: "action" },
    { kind: "action" },
    { kind: "action" },
    { kind: "message", role: "visitor", rich: "form_result", text: tr("已提交") },
    { kind: "message", role: "ai", rich: "file", text: tr("售后申请说明") },
    { kind: "message", role: "ai", rich: "audio", text: tr("语音指引") },
    { kind: "message", role: "ai", rich: "video", text: tr("视频指引") },
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
  const optionIndex = items.findIndex((item) => item.kind === "message" && item.rich === "options");
  const formIndex = items.findIndex((item) => item.kind === "message" && item.rich === "form");
  const count = reduced ? items.length : shown,
    first = Math.max(0, count - 60);
  const history = Array.from({ length: count - first }, (_, index) => first + index);
  const next = items[shown % items.length];
  const nextText =
    next.kind === "message" ? (next.choice ? choices[shown - 1] || options[0] : next.text) : "";
  const writing = next.kind === "message" && next.role === "visitor" && !next.rich && !next.image;
  const draft = writing ? Array.from(nextText).slice(0, draftLength).join("") : "";
  const ready = writing && draftLength >= Array.from(nextText).length;
  const playing = visible && !reduced && editing === null;
  const typing = playing && next.kind === "message" && next.role === "ai";

  /** advance 追加当前示例消息并清空输入中的模拟文字，不清空已有聊天记录。 */
  function advance() {
    setShown((value) => value + 1);
    setDraftLength(0);
  }

  useEffect(() => {
    const preference = matchMedia("(prefers-reduced-motion: reduce)");
    let inView = false;
    /** reconcile 同步首屏可见性、标签页状态与系统低动效偏好。 */
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
    // 选项出现后预留阅读与点击时间，再模拟默认选择，避免自动输入抢走可交互窗口。
    const delay = writing
      ? next.choice && draftLength === 0
        ? 2400
        : ready
          ? 650
          : 55
      : next.kind === "action"
        ? 1500
        : next.kind === "separator"
          ? 1800
          : next.rich
            ? 2900
            : 2100;
    const timer = setTimeout(() => {
      if (writing && !ready) setDraftLength((value) => value + 1);
      else advance();
    }, delay);
    return () => clearTimeout(timer);
  }, [
    playing,
    shown,
    draftLength,
    writing,
    ready,
    next.kind,
    next.kind === "message" ? next.rich : undefined,
  ]);
  useEffect(() => {
    /** prune 只保留仍在历史窗口中的用户演示输入，避免长时间循环累积状态。 */
    const prune = <T,>(values: Record<number, T>) =>
      Object.fromEntries(Object.entries(values).filter(([id]) => Number(id) >= first));
    if (first) {
      setChoices(prune);
      setForms(prune);
    }
  }, [first]);
  useLayoutEffect(() => {
    const list = viewport.current;
    if (list && following.current)
      list.scrollTo({
        top: list.scrollHeight,
        behavior: reduced || shown === 2 ? "auto" : "smooth",
      });
  }, [shown, reduced, typing]);
  /** updateFollowing 回到底部时恢复跟随，程序平滑滚动不会误判为用户正在向上翻阅。 */
  function updateFollowing() {
    const list = viewport.current;
    if (list && list.scrollHeight - list.clientHeight - list.scrollTop < 60)
      following.current = true;
  }
  /** valuesFor 读取演示表单手工输入或分阶段自动填写值，结果仍只留在当前页面。 */
  function valuesFor(id: number): FormValues {
    const phase = count - id - 1;
    const optionId = id - formIndex + optionIndex;
    return {
      order: phase >= 1 ? "GC20261122018" : "",
      details: phase >= 2 ? choices[optionId] || options[0] : "",
      contact: "",
      ...forms[id],
    };
  }
  /** richContent 以真实可操作控件展示选项、必选/选填表单、结果及本地媒体示例。 */
  function richContent(kind: RichKind, id: number) {
    const active = !reduced && shown <= id + 4 && id >= shown - 4;
    if (kind === "options") {
      const selected = choices[id] || (count > id + 1 ? options[0] : "");
      return (
        <div className="ai-demo-options" role="group" aria-label={tr("快捷回复")}>
          {options.map((option) => (
            <button
              type="button"
              key={option}
              aria-pressed={selected === option}
              disabled={count > id + 1 || reduced}
              onClick={() => {
                setChoices((old) => ({ ...old, [id]: option }));
                setShown(id + 2);
                setDraftLength(0);
              }}
            >
              {option}
              {selected === option && <CheckOutlined />}
            </button>
          ))}
        </div>
      );
    }
    if (kind === "form") {
      const values = valuesFor(id),
        submitted = count >= id + 4;
      return (
        <form
          className="ai-demo-form"
          aria-label={tr("售后信息")}
          onFocusCapture={() => {
            if (active && !submitted) setEditing(id);
          }}
          onBlurCapture={(event) => {
            if (!event.currentTarget.contains(event.relatedTarget)) setEditing(null);
          }}
          onSubmit={(event) => {
            event.preventDefault();
            setForms((old) => ({ ...old, [id]: values }));
            setEditing(null);
            setShown(id + 5);
            setDraftLength(0);
          }}
        >
          <strong>{tr("售后信息")}</strong>
          {(
            [
              ["order", tr("订单号"), true],
              ["details", tr("问题描述"), true],
              ["contact", tr("联系方式"), false],
            ] as const
          ).map(([name, label, required]) => (
            <label key={name}>
              <span>
                {label}
                <small>{tr(required ? "必填" : "选填")}</small>
              </span>
              <input
                name={name}
                required={required}
                maxLength={80}
                value={values[name]}
                disabled={submitted || !active}
                autoComplete="off"
                onChange={(event) =>
                  setForms((old) => ({ ...old, [id]: { ...values, [name]: event.target.value } }))
                }
              />
            </label>
          ))}
          <button type="submit" disabled={submitted || !active}>
            {submitted ? (
              <>
                <CheckOutlined /> {tr("已提交")}
              </>
            ) : (
              tr("保存")
            )}
          </button>
        </form>
      );
    }
    if (kind === "form_result") {
      const formId = id - (id % items.length) + formIndex,
        values = valuesFor(formId);
      return (
        <dl className="ai-demo-result">
          {[
            [tr("订单号"), values.order],
            [tr("问题描述"), values.details],
            ...(values.contact ? [[tr("联系方式"), values.contact]] : []),
          ].map(([label, value]) => (
            <div key={label}>
              <dt>{label}</dt>
              <dd>{value}</dd>
            </div>
          ))}
        </dl>
      );
    }
    if (kind === "file")
      return (
        <a className="ai-demo-file" href="/examples/after-sales-guide.txt" download>
          <FileTextOutlined />
          <span>
            {tr("售后申请说明")}
            <small>TXT</small>
          </span>
          <DownloadOutlined />
        </a>
      );
    if (kind === "audio")
      return (
        <audio
          className="ai-demo-audio"
          controls
          preload="none"
          src="/examples/after-sales-voice.wav"
          aria-label={tr("语音指引")}
        />
      );
    return (
      <video
        className="ai-demo-video"
        controls
        playsInline
        preload="none"
        poster="/images/ai-demo-headphones.png"
        src="/examples/after-sales-video.webm"
        aria-label={tr("视频指引")}
      />
    );
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
        onWheel={(event) => {
          if (event.deltaY < 0) following.current = false;
        }}
        onTouchStart={() => {
          following.current = false;
        }}
        onKeyDown={(event) => {
          if (["ArrowUp", "PageUp", "Home"].includes(event.key)) following.current = false;
        }}
        role="log"
        aria-live="off"
        aria-label={tr("AI客服对话演示")}
        tabIndex={0}
        data-message-count={count}
      >
        <ol className="ai-conversation-history">
          {history.map((id) => {
            const item = items[id % items.length];
            if (item.kind === "action") return null;
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
                data-message-type={item.rich || (item.image ? "image" : "text")}
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
                    <p>{item.choice ? choices[id - 1] || options[0] : item.text}</p>
                    {item.rich && richContent(item.rich, id)}
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
      <div className="ai-demo-composer">
        <PlusOutlined aria-hidden="true" />
        <input aria-label={tr("消息内容")} readOnly value={draft} placeholder={tr("输入消息…")} />
        <button
          type="button"
          aria-label={tr("发送消息")}
          disabled={!ready || reduced}
          onClick={advance}
        >
          <SendOutlined />
        </button>
      </div>
      <figcaption>{tr("产品演示 · 非真实咨询")}</figcaption>
    </figure>
  );
}
