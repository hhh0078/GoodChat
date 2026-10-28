/** 文件作用说明：渐进加载官网双环三维动画，处理暂停、可见性、减少动态偏好及静态图回退。 */
"use client";

import Image from "next/image";
import { useEffect, useRef, useState } from "react";
import { Button } from "antd";
import { PauseOutlined, CaretRightOutlined } from "@ant-design/icons";

type RingsScene = ReturnType<typeof import("./hero-rings-scene").createHeroRingsScene>;
type SceneState = "loading" | "ready" | "static" | "fallback";

/** HeroRings 用独立三维环体替换静态主视觉，保留原图作为服务端和设备能力回退。 */
export function HeroRings() {
  const hostRef = useRef<HTMLDivElement>(null);
  const pausedRef = useRef(false);
  const reconcileRef = useRef<(() => void) | null>(null);
  const [paused, setPaused] = useState(false);
  const [state, setState] = useState<SceneState>("loading");

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
    let disposed = false;
    let failed = false;
    let loading = false;
    let inView = false;
    let scene: RingsScene | null = null;
    let frame = 0;
    let elapsed = 0;
    let lastTimestamp = 0;
    let lastRendered = 0;

    /** stop 停止预约帧并重置时间基准，恢复时不跳过暂停期间。 */
    function stop() {
      cancelAnimationFrame(frame);
      frame = 0;
      lastTimestamp = 0;
      lastRendered = 0;
    }

    /** fallback 在设备不支持或上下文丢失时恢复原图，避免空白主视觉。 */
    function fallback() {
      if (disposed || failed) return;
      failed = true;
      stop();
      host?.removeEventListener("webglcontextlost", fallback, true);
      scene?.dispose();
      scene = null;
      host?.replaceChildren();
      setState("fallback");
    }

    /** resize 使用真实容器比例与受限像素密度，不改变文档布局。 */
    function resize() {
      if (!scene || !host || host.clientWidth === 0 || host.clientHeight === 0) return;
      const maxRatio = window.matchMedia("(max-width: 768px)").matches ? 1.25 : 1.5;
      scene.resize(
        host.clientWidth,
        host.clientHeight,
        Math.min(window.devicePixelRatio, maxRatio),
      );
      scene.render(elapsed);
    }

    /** animate 最多每秒绘制三十帧，只有可见且未暂停时继续预约。 */
    function animate(timestamp: number) {
      frame = 0;
      if (
        !scene ||
        disposed ||
        !inView ||
        document.hidden ||
        preference.matches ||
        pausedRef.current
      )
        return;
      if (lastTimestamp) elapsed += Math.min((timestamp - lastTimestamp) / 1000, 0.1);
      lastTimestamp = timestamp;
      if (!lastRendered || timestamp - lastRendered >= 1000 / 30) {
        try {
          scene.render(elapsed);
          lastRendered = timestamp;
        } catch {
          fallback();
          return;
        }
      }
      frame = requestAnimationFrame(animate);
    }

    /** initialize 仅在首屏可见时加载三维模块，首帧成功前保持静态原图。 */
    async function initialize() {
      if (loading || scene || disposed || failed || !host?.clientWidth || !host.clientHeight)
        return;
      loading = true;
      try {
        const { createHeroRingsScene } = await import("./hero-rings-scene");
        if (disposed || preference.matches || failed) return;
        scene = createHeroRingsScene(host);
        host.addEventListener("webglcontextlost", fallback, true);
        resize();
        if (failed) return;
        setState("ready");
        reconcile();
      } catch {
        fallback();
      } finally {
        loading = false;
      }
    }

    /** reconcile 同步系统偏好、用户暂停与可见性，不在后台持续消耗绘制资源。 */
    function reconcile() {
      if (disposed || failed) return;
      if (preference.matches) {
        stop();
        host?.removeEventListener("webglcontextlost", fallback, true);
        scene?.dispose();
        scene = null;
        setState("static");
        return;
      }
      if (!inView || document.hidden || pausedRef.current) {
        stop();
        return;
      }
      if (!scene) {
        void initialize();
      } else if (!frame) {
        frame = requestAnimationFrame(animate);
      }
    }

    const observer = new IntersectionObserver(
      ([entry]) => {
        inView = entry?.isIntersecting ?? false;
        reconcile();
      },
      { threshold: 0.05 },
    );
    const sizes = new ResizeObserver(() => {
      try {
        resize();
        reconcile();
      } catch {
        fallback();
      }
    });
    observer.observe(host);
    sizes.observe(host);
    preference.addEventListener("change", reconcile);
    document.addEventListener("visibilitychange", reconcile);
    reconcileRef.current = reconcile;
    reconcile();

    return () => {
      disposed = true;
      reconcileRef.current = null;
      stop();
      observer.disconnect();
      sizes.disconnect();
      preference.removeEventListener("change", reconcile);
      document.removeEventListener("visibilitychange", reconcile);
      host.removeEventListener("webglcontextlost", fallback, true);
      scene?.dispose();
    };
  }, []);

  /** togglePaused 在点击当下停止预约帧，避免等待界面状态提交期间又绘制一帧。 */
  function togglePaused() {
    const next = !pausedRef.current;
    pausedRef.current = next;
    reconcileRef.current?.();
    setPaused(next);
  }

  return (
    <>
      <div className="cobalt-hero-rings" data-scene-state={state} aria-hidden="true">
        <Image
          className="cobalt-rings-fallback"
          src="/images/cobalt-communication-rings.png"
          alt=""
          width={1600}
          height={1100}
          priority
          sizes="(max-width: 768px) 90vw, 65vw"
        />
        <div ref={hostRef} className="cobalt-rings-canvas" />
      </div>
      {state === "ready" && (
        <Button
          className="cobalt-hero-motion-control"
          icon={paused ? <CaretRightOutlined /> : <PauseOutlined />}
          aria-label={paused ? "播放3D动画" : "暂停3D动画"}
          aria-pressed={paused}
          onClick={togglePaused}
        >
          {paused ? "播放动画" : "暂停动画"}
        </Button>
      )}
    </>
  );
}
