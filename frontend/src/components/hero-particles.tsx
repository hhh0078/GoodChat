/** 文件作用说明：按用户指定MotionLab粒子系统制作钴蓝Three.js首屏背景，离屏、后台及减少动画时停绘。 */
"use client";
import { useEffect, useRef } from "react";

/** HeroParticles 延迟载入现有Three.js，限制粒子、帧率和像素比，并在卸载时释放显卡资源。 */
export function HeroParticles() {
  const host = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const node = host.current;
    if (!node) return;
    const preference = matchMedia("(prefers-reduced-motion: reduce)");
    let disposed = false,
      inView = false,
      frame = 0,
      last = 0,
      frames = 0;
    let initializing = false;
    let draw: ((time: number) => void) | undefined;
    let release: (() => void) | undefined;
    const pointer = { x: 0, y: 0 };
    /** render 仅在首屏可见时以最高30FPS绘制，时间使用增量以免恢复时跳动。 */
    function render(time: number) {
      if (disposed || !inView || document.hidden || preference.matches || !draw) {
        frame = 0;
        return;
      }
      frame = requestAnimationFrame(render);
      if (time - last < 1000 / 30) return;
      draw(Math.min((time - last) / 1000, 0.05));
      last = time;
      node!.dataset.frames = String(++frames);
    }
    /** reconcile 统一响应滚动可见性、系统偏好与标签页状态。 */
    function reconcile() {
      cancelAnimationFrame(frame);
      frame = 0;
      last = performance.now();
      if (preference.matches) {
        node!.dataset.state = "static";
        draw?.(0);
      } else if (!draw && !initializing) {
        void initialize();
      } else if (draw && inView && !document.hidden) {
        node!.dataset.state = "ready";
        frame = requestAnimationFrame(render);
      }
    }
    /** move 将指针位置映射为轻微视差，不拦截正文、链接或聊天表单交互。 */
    function move(event: PointerEvent) {
      if (preference.matches) return;
      const rect = node!.getBoundingClientRect();
      pointer.x = (event.clientX - rect.left) / rect.width - 0.5;
      pointer.y = (event.clientY - rect.top) / rect.height - 0.5;
    }
    /** leave 在指针移开时缓慢恢复粒子云中心。 */
    function leave() {
      pointer.x = 0;
      pointer.y = 0;
    }
    const parent = node.parentElement;
    const observer = new IntersectionObserver(([entry]) => {
      inView = !!entry?.isIntersecting;
      reconcile();
    });
    observer.observe(node);
    document.addEventListener("visibilitychange", reconcile);
    preference.addEventListener("change", reconcile);
    parent?.addEventListener("pointermove", move);
    parent?.addEventListener("pointerleave", leave);
    /** initialize 在正文渲染后按需加载粒子场景，失败保留CSS点阵背景。 */
    async function initialize() {
      if (preference.matches || initializing || disposed) {
        if (preference.matches) node!.dataset.state = "static";
        return;
      }
      initializing = true;
      try {
        const THREE = await import("three");
        if (disposed) return;
        const renderer = new THREE.WebGLRenderer({
          alpha: true,
          antialias: false,
          powerPreference: "low-power",
        });
        const scene = new THREE.Scene();
        const camera = new THREE.PerspectiveCamera(48, 1, 0.1, 80);
        camera.position.z = 12;
        const geometry = new THREE.BufferGeometry();
        const count = innerWidth < 700 ? 260 : 900;
        const positions = new Float32Array(count * 3);
        for (let i = 0; i < count; i++) {
          positions[i * 3] = (Math.random() - 0.5) * 27;
          positions[i * 3 + 1] = (Math.random() - 0.5) * 15;
          positions[i * 3 + 2] = (Math.random() - 0.5) * 12;
        }
        geometry.setAttribute("position", new THREE.BufferAttribute(positions, 3));
        const material = new THREE.PointsMaterial({
          color: "#4675cf",
          size: 0.045,
          transparent: true,
          opacity: 0.52,
          depthWrite: false,
        });
        const particles = new THREE.Points(geometry, material);
        scene.add(particles);
        node!.append(renderer.domElement);
        /** resize 同步实际容器，移动端不使用高倍像素比。 */
        function resize() {
          const width = node!.clientWidth,
            height = node!.clientHeight;
          if (!width || !height) return;
          renderer.setPixelRatio(Math.min(devicePixelRatio, width < 700 ? 1 : 1.25));
          renderer.setSize(width, height);
          camera.aspect = width / height;
          camera.updateProjectionMatrix();
          renderer.render(scene, camera);
        }
        const sizeObserver = new ResizeObserver(resize);
        sizeObserver.observe(node!);
        resize();
        draw = (delta) => {
          particles.rotation.y += delta * 0.025;
          particles.rotation.x += (pointer.y * 0.08 - particles.rotation.x) * 0.035;
          particles.position.x += (pointer.x * 0.3 - particles.position.x) * 0.035;
          renderer.render(scene, camera);
        };
        /** lost 停止不可继续绘制的WebGL上下文，退回安静的静态点阵。 */
        function lost(event: Event) {
          event.preventDefault();
          cancelAnimationFrame(frame);
          draw = undefined;
          node!.dataset.state = "fallback";
        }
        renderer.domElement.addEventListener("webglcontextlost", lost);
        release = () => {
          sizeObserver.disconnect();
          renderer.domElement.removeEventListener("webglcontextlost", lost);
          geometry.dispose();
          material.dispose();
          renderer.dispose();
          renderer.domElement.remove();
        };
        reconcile();
      } catch {
        node!.dataset.state = "fallback";
        release?.();
      }
    }
    void initialize();
    return () => {
      disposed = true;
      cancelAnimationFrame(frame);
      observer.disconnect();
      document.removeEventListener("visibilitychange", reconcile);
      preference.removeEventListener("change", reconcile);
      parent?.removeEventListener("pointermove", move);
      parent?.removeEventListener("pointerleave", leave);
      release?.();
    };
  }, []);
  return <div className="hero-particles" aria-hidden="true" ref={host} data-state="loading" />;
}
