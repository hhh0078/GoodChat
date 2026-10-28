/** 文件作用说明：验证发送动效按真实消息布局定位、取消时恢复消息，并遵守系统减少动态效果偏好。 */
import assert from "node:assert/strict";
import test from "node:test";
import { animateMessageFlight } from "../src/lib/message-flight.ts";

/** fixture 构造只含定位与动画API的文档，记录测量结果而不依赖网络或真实消息服务。 */
function fixture(reduced = false) {
  const names = [
    "window",
    "document",
    "requestAnimationFrame",
    "cancelAnimationFrame",
    "getComputedStyle",
    "CSS",
  ];
  const previous = new Map(
    names.map((name) => [name, Object.getOwnPropertyDescriptor(globalThis, name)]),
  );
  const frames = new Map<number, FrameRequestCallback>();
  let serial = 0;
  const overlays: MockNode[] = [],
    styles: MockNode[] = [];
  let finish: () => void = () => {};
  const animations: { frames: Keyframe[]; options: KeyframeAnimationOptions }[] = [];
  /** MockNode 记录浮层生命周期和动画关键帧，删除操作不触碰真实文档。 */
  class MockNode {
    style: Record<string, string> = {};
    textContent = "";
    className = "";
    removed = false;
    /** setAttribute 接受动画副本的无障碍隐藏标记。 */
    setAttribute() {}
    /** remove 标记浮层或动态样式已被清理。 */
    remove() {
      this.removed = true;
    }
    /** animate 保留关键帧并延迟完成，让测试检查动画中的真实消息隐藏状态。 */
    animate(values: Keyframe[], options: KeyframeAnimationOptions) {
      animations.push({ frames: values, options });
      return {
        finished: new Promise<void>((resolve) => {
          finish = resolve;
        }),
        cancel() {},
      };
    }
  }
  const values: Record<string, unknown> = {
    window: { matchMedia: () => ({ matches: reduced }) },
    document: {
      createElement: () => new MockNode(),
      head: { append: (node: MockNode) => styles.push(node) },
      body: { append: (node: MockNode) => overlays.push(node) },
    },
    requestAnimationFrame: (callback: FrameRequestCallback) => {
      frames.set(++serial, callback);
      return serial;
    },
    cancelAnimationFrame: (id: number) => frames.delete(id),
    getComputedStyle: () => ({
      backgroundColor: "white",
      color: "black",
      borderRadius: "12px",
      font: "14px system-ui",
      padding: "8px",
    }),
    CSS: { escape: (value: string) => value },
  };
  for (const name of names)
    Object.defineProperty(globalThis, name, {
      configurable: true,
      writable: true,
      value: values[name],
    });
  const scroller = { scrollTop: 0, scrollHeight: 640 };
  const target = {
    getClientRects: () => [{}],
    getBoundingClientRect: () => ({ left: 100, top: 80, width: 220, height: 48 }),
  };
  const root = {
    dataset: {} as Record<string, string>,
    isConnected: true,
    querySelector: (selector: string) => (selector === ".chat-messages" ? scroller : target),
  };
  const input = { getBoundingClientRect: () => ({ left: 10, top: 400, width: 320, height: 40 }) };
  return {
    root: root as unknown as HTMLElement,
    input: input as unknown as HTMLElement,
    scroller,
    styles,
    overlays,
    animations,
    finish: () => finish(),
    flush: () => {
      for (const [id, callback] of [...frames]) {
        frames.delete(id);
        callback(0);
      }
    },
    restore: () => {
      for (const name of names) {
        const descriptor = previous.get(name);
        if (descriptor) Object.defineProperty(globalThis, name, descriptor);
        else Reflect.deleteProperty(globalThis, name);
      }
    },
  };
}

test("发送动效定位到同编号消息，完成后恢复真实气泡", async () => {
  const f = fixture();
  try {
    animateMessageFlight(f.root, f.input, "11111111-2222-4333-8444-555555555555", "真实发送文字");
    f.flush();
    assert.equal(f.scroller.scrollTop, 640);
    assert.equal(f.overlays[0].style.left, "100px");
    assert.equal(f.overlays[0].style.top, "80px");
    assert.equal(f.animations[0].frames[0].transform, "translate(-78px, 328px) scale(.96)");
    assert.equal(f.animations[0].options.duration, 260);
    assert.match(f.styles[0].textContent, /11111111-2222-4333-8444-555555555555/);
    f.finish();
    await Promise.resolve();
    assert.equal(f.overlays[0].removed, true);
    assert.equal(f.styles[0].removed, true);
    assert.equal(f.root.dataset.flightScope, undefined);
  } finally {
    f.restore();
  }
});

test("会话取消或减少动态效果时不留下浮层或隐藏消息", () => {
  for (const reduced of [false, true]) {
    const f = fixture(reduced);
    try {
      const cancel = animateMessageFlight(
        f.root,
        f.input,
        "11111111-2222-4333-8444-555555555555",
        "文字",
      );
      cancel();
      f.flush();
      assert.equal(f.overlays.length, 0);
      assert.equal(f.styles.length, 0);
    } finally {
      f.restore();
    }
  }
});
