/** 文件作用说明：以可暂停、逐帧切换的交互卡片演示 AI 客服处理咨询、读取知识与人工协作的过程。 */
"use client";

import { useTranslator } from "@/i18n/use-translator";
import {
  ArrowLeftOutlined,
  ArrowRightOutlined,
  CheckOutlined,
  FileTextOutlined,
  MessageOutlined,
  PauseOutlined,
  CaretRightOutlined,
  PictureOutlined,
  RobotOutlined,
  TeamOutlined,
} from "@ant-design/icons";
import { useEffect, useRef, useState } from "react";

/** PublicAIDemo 保留服务端可读文案，仅在卡片可见且用户允许动态时自动播放五步业务示意。 */
export function PublicAIDemo() {
  const tr = useTranslator();
  const host = useRef<HTMLElement>(null);
  const [scene, setScene] = useState(0);
  const [step, setStep] = useState(0);
  const [paused, setPaused] = useState(false);
  const [visible, setVisible] = useState(false);
  const [reduced, setReduced] = useState(false);
  const scenarios = [
    {
      label: tr("订单售后"),
      icon: FileTextOutlined,
      question: tr("怎么申请售后服务？"),
      intent: tr("订单售后"),
      knowledge: tr("售后服务指南"),
      excerpt: tr("申请售后时，请先提供订单号，确认订单信息后再告知处理方式。"),
      reply: tr("我已查阅售后知识库，请提供订单号，我来帮你确认处理方式。"),
    },
    {
      label: tr("访客图片识别"),
      icon: PictureOutlined,
      question: tr("这个提示是什么意思？"),
      intent: tr("访客图片识别"),
      knowledge: tr("常见问题指南"),
      excerpt: tr("登录链接过期时，可以返回登录页面，重新获取验证邮件。"),
      reply: tr("图片提示登录链接已过期，请返回登录页面，重新获取一封验证邮件。"),
    },
    {
      label: tr("人工接管"),
      icon: TeamOutlined,
      question: tr("我想让人工客服帮我处理。"),
      intent: tr("人工接管"),
      knowledge: tr("人工协作规则"),
      excerpt: tr("客户需要人工帮助时发起转接，客服接管后暂停 AI 自动回复。"),
      reply: tr("正在为你转接人工客服，对话记录会保留，方便客服继续处理。"),
    },
  ];
  const steps = [
    { title: tr("收到咨询"), text: tr("将消息与当前对话关联"), icon: MessageOutlined },
    { title: tr("识别需求"), text: tr("结合消息内容与对话上下文"), icon: RobotOutlined },
    { title: tr("读取知识"), text: tr("按需读取相关业务资料"), icon: FileTextOutlined },
    { title: tr("生成回复"), text: tr("依据业务知识组织回答"), icon: RobotOutlined },
    { title: tr("完成处理"), text: tr("发送回复，或交由人工接手"), icon: CheckOutlined },
  ];
  const current = scenarios[scene];
  const playing = !paused && visible && !reduced;

  useEffect(() => {
    const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
    let inView = false;
    /** reconcile 同步视口、后台状态与系统偏好，隐藏时不安排自动切帧。 */
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
    const timer = window.setTimeout(() => setStep((value) => (value + 1) % 5), 4400);
    return () => window.clearTimeout(timer);
  }, [playing, step, scene]);

  /** selectStep 手动选择后暂停轮播，确保客户有时间阅读所选处理动作。 */
  function selectStep(next: number) {
    setPaused(true);
    setStep((next + 5) % 5);
  }

  /** selectScene 切换示例并从第一步开始，保留用户的播放偏好。 */
  function selectScene(next: number) {
    setScene(next);
    setStep(0);
  }

  return (
    <figure className="product-illustration ai-demo" id="ai-demo" ref={host}>
      <div className="ai-demo-toolbar">
        <strong>
          <RobotOutlined />
          {tr("AI客服")}
          <span>LIVE DEMO</span>
        </strong>
        <span className="ai-demo-status">
          <i />
          {tr("产品演示")}
        </span>
      </div>
      <div className="ai-demo-scenarios" role="group" aria-label={tr("切换演示场景")}>
        {scenarios.map((item, index) => (
          <button
            type="button"
            key={item.label}
            aria-pressed={scene === index}
            onClick={() => selectScene(index)}
          >
            <item.icon />
            {item.label}
            <ArrowRightOutlined />
          </button>
        ))}
      </div>
      <div className="ai-demo-body">
        <aside className="ai-demo-rail">
          <div className="ai-demo-overline">AI WORKFLOW</div>
          <h3>{tr("从提问到回应")}</h3>
          <ol aria-label={tr("AI 处理步骤")}>
            {steps.map((item, index) => (
              <li key={item.title} data-active={step === index} data-done={step > index}>
                <button
                  type="button"
                  aria-current={step === index ? "step" : undefined}
                  onClick={() => selectStep(index)}
                >
                  <span className="ai-demo-step-number">
                    {step > index ? <CheckOutlined /> : `0${index + 1}`}
                  </span>
                  <span>
                    <strong>{item.title}</strong>
                    <small>{item.text}</small>
                  </span>
                </button>
              </li>
            ))}
          </ol>
          <div className="ai-demo-source">
            <FileTextOutlined />
            <span>
              {tr("知识库")}
              <strong>{current.knowledge}</strong>
            </span>
          </div>
        </aside>
        <div className="ai-demo-stage">
          <div className="ai-demo-visitor">
            <span className="ai-demo-overline">CUSTOMER</span>
            <p>{current.question}</p>
            {scene === 1 && (
              <div className="ai-demo-image">
                <PictureOutlined />
                <span>{tr("登录链接已过期")}</span>
              </div>
            )}
          </div>
          <div className="ai-demo-frames" aria-live={paused ? "polite" : "off"}>
            {steps.map((item, index) => (
              <div
                key={index}
                className="ai-demo-frame"
                data-active={step === index}
                aria-hidden={step !== index}
              >
                <div className="ai-demo-frame-heading">
                  <span className="ai-demo-action-icon">
                    <item.icon />
                  </span>
                  <div>
                    <span className="ai-demo-overline">AI ACTION / 0{index + 1}</span>
                    <h4>{item.title}</h4>
                  </div>
                  <span className="ai-demo-frame-count">0{index + 1} / 05</span>
                </div>
                {index === 0 && (
                  <div className="ai-demo-action-content">
                    <p>{tr("将消息与当前对话关联")}</p>
                    <div className="ai-demo-message">
                      <MessageOutlined />
                      <span>{current.question}</span>
                      <CheckOutlined />
                    </div>
                    <div className="ai-demo-signal">
                      <i />
                      <i />
                      <i />
                      <span>{tr("准备处理咨询")}</span>
                    </div>
                  </div>
                )}
                {index === 1 && (
                  <div className="ai-demo-action-content">
                    <p>{tr("结合消息内容与对话上下文")}</p>
                    <div className="ai-demo-intent">
                      <current.icon />
                      <strong>{current.intent}</strong>
                      <CheckOutlined />
                    </div>
                    <div className="ai-demo-tags">
                      <span>{tr("当前消息")}</span>
                      <span>{tr("对话上下文")}</span>
                      {scene === 1 && <span>{tr("图片内容")}</span>}
                    </div>
                  </div>
                )}
                {index === 2 && (
                  <div className="ai-demo-action-content">
                    <p>{tr("AI 按需读取相关知识")}</p>
                    <div className="ai-demo-document">
                      <div>
                        <FileTextOutlined />
                        <strong>{current.knowledge}</strong>
                        <CheckOutlined />
                      </div>
                      <p>{current.excerpt}</p>
                    </div>
                  </div>
                )}
                {index === 3 && (
                  <div className="ai-demo-action-content">
                    <p>{tr("依据业务知识组织回答")}</p>
                    <div className="ai-demo-draft">
                      <span>{tr("回复草稿")}</span>
                      <p>{current.reply}</p>
                    </div>
                    <small className="ai-demo-reference">
                      <FileTextOutlined />
                      {current.knowledge}
                    </small>
                  </div>
                )}
                {index === 4 && (
                  <div className="ai-demo-action-content">
                    <div className="ai-demo-reply">
                      <strong>
                        <RobotOutlined />
                        {tr("AI客服")}
                      </strong>
                      <p>{current.reply}</p>
                    </div>
                    <div className="ai-demo-result">
                      {scene === 2 ? <TeamOutlined /> : <CheckOutlined />}
                      <strong>
                        {scene === 2 ? tr("人工接管后，AI 暂停回复") : tr("已发送回复")}
                      </strong>
                    </div>
                  </div>
                )}
              </div>
            ))}
          </div>
          <div className="ai-demo-playback">
            <span>{tr("逐步查看 AI 的处理动作")}</span>
            <div>
              <button type="button" onClick={() => selectStep(step - 1)} aria-label={tr("上一步")}>
                <ArrowLeftOutlined />
              </button>
              <button
                type="button"
                className="ai-demo-play"
                aria-pressed={!paused && !reduced}
                disabled={reduced}
                onClick={() => setPaused((value) => !value)}
                aria-label={
                  reduced
                    ? tr("已遵循减少动态偏好，可手动逐步查看")
                    : paused
                      ? tr("播放演示")
                      : tr("暂停演示")
                }
              >
                {paused || reduced ? <CaretRightOutlined /> : <PauseOutlined />}
                {reduced ? tr("产品演示") : paused ? tr("播放演示") : tr("暂停演示")}
              </button>
              <button type="button" onClick={() => selectStep(step + 1)} aria-label={tr("下一步")}>
                <ArrowRightOutlined />
              </button>
            </div>
          </div>
        </div>
      </div>
      <figcaption>
        {reduced ? `${tr("已遵循减少动态偏好，可手动逐步查看")} · ` : ""}
        {tr("产品界面示意 · 非真实业务资料")}
      </figcaption>
    </figure>
  );
}
