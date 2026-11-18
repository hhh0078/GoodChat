/** 文件作用说明：为官网渐进增强滚动出场和点击反馈，保留服务端正文并遵循减少动态偏好。 */
"use client";

import { usePathname } from "next/navigation";
import { useEffect } from "react";

/** PublicMotion 在各官网路由复用可见性动画与真实控件的点击反馈，卸载时释放观察器和事件。 */
export function PublicMotion() {
  const pathname = usePathname();
  useEffect(() => {
    const root = document.querySelector<HTMLElement>(".public-site");
    if (!root) return;
    const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
    const selector = [
      ".public-header",
      ".public-hero-copy > *",
      ".public-hero-product",
      ".cobalt-hero-callout",
      ".public-ai-overview > header",
      ".public-ai-capability-grid > article",
      ".public-content-heading",
      ".public-pricing-heading",
      ".cobalt-connect-copy",
      ".cobalt-connect-list > article",
      ".cobalt-design-showcase",
      ".cobalt-free-content > *",
      ".public-ai-faq > *",
      ".page-heading",
      ".public-docs-section",
      ".public-docs-cta",
      ".public-toc",
      ".public-plan",
      ".public-upgrade-note",
      ".download-page > h1",
      ".download-intro",
      ".download-platform",
      ".download-help",
      ".public-footer-brand",
      ".footer-links",
    ].join(",");
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (!entry.isIntersecting) continue;
          entry.target.setAttribute("data-public-visible", "true");
          observer.unobserve(entry.target);
        }
      },
      { threshold: 0.08 },
    );

    /** discover 发现服务端正文及异步加载卡片，只为尚未进入视口的元素准备出场动画。 */
    function discover() {
      if (!root) return;
      root.querySelectorAll<HTMLElement>(selector).forEach((element, index) => {
        if (element.hasAttribute("data-public-reveal")) return;
        element.setAttribute("data-public-reveal", "true");
        element.style.setProperty("--public-reveal-delay", `${(index % 4) * 55}ms`);
        if (element.getBoundingClientRect().top < window.innerHeight || preference.matches) {
          element.setAttribute("data-public-visible", "true");
        } else observer.observe(element);
      });
    }

    /** syncPreference 动态减少偏好开启时立即显示正文，关闭后仍保持已显示内容稳定。 */
    function syncPreference() {
      if (!preference.matches) return;
      root
        ?.querySelectorAll("[data-public-reveal]")
        .forEach((element) => element.setAttribute("data-public-visible", "true"));
    }

    /** clickFeedback 为链接、按钮和折叠标题提供一次点击反馈，不拦截原有导航和键盘行为。 */
    function clickFeedback(event: MouseEvent) {
      if (preference.matches || !(event.target instanceof Element)) return;
      const target = event.target.closest<HTMLElement>("a,button,summary");
      if (!target || !root?.contains(target) || target.matches(":disabled,[aria-disabled=true]"))
        return;
      target.animate(
        [{ filter: "brightness(1)" }, { filter: "brightness(0.87)" }, { filter: "brightness(1)" }],
        { duration: 320, easing: "ease-out" },
      );
    }
    discover();
    const mutations = new MutationObserver(discover);
    mutations.observe(root, { childList: true, subtree: true });
    preference.addEventListener("change", syncPreference);
    root.addEventListener("click", clickFeedback);
    return () => {
      observer.disconnect();
      mutations.disconnect();
      preference.removeEventListener("change", syncPreference);
      root.removeEventListener("click", clickFeedback);
      root.querySelectorAll("[data-public-reveal]").forEach((element) => {
        element.removeAttribute("data-public-reveal");
        element.removeAttribute("data-public-visible");
      });
    };
  }, [pathname]);
  return null;
}
