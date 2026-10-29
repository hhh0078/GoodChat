/** 文件作用说明：在 Node VM 验证水合前 SVG 扩展兼容脚本的精确范围、节点保留及短期观察清理。 */
import assert from "node:assert/strict";
import test from "node:test";
import { runInNewContext } from "node:vm";
import {
  browserSymbolCompatibilityScript,
  stopBrowserSymbolCompatibility,
} from "../src/lib/browser-symbol-compat.ts";

const phoenixID = "__PHOENIX_THEMED_EMPTY_SYMBOL_DEFS_ID__";
const stopKey = "__goodchatStopBrowserSymbolCompat";

/** ElementNode 提供测试所需的只读几何、SVG 签名和真实节点重排语义，不安装 DOM 依赖。 */
class ElementNode {
  tagName: string;
  id: string;
  namespaceURI = "http://www.w3.org/1999/xhtml";
  parentElement: ElementNode | null = null;
  children: ElementNode[] = [];
  style = { width: "0px", height: "0px" };
  rect = { width: 0, height: 0 };
  definitions = false;
  appends = 0;

  /** constructor 建立指定标签和标识的独立节点。 */
  constructor(tagName: string, id = "") {
    this.tagName = tagName;
    this.id = id;
  }

  /** firstElementChild 返回首个直接子元素。 */
  get firstElementChild(): ElementNode | null {
    return this.children[0] || null;
  }

  /** nextElementSibling 根据当前父节点顺序返回下一兄弟元素。 */
  get nextElementSibling(): ElementNode | null {
    const siblings = this.parentElement?.children || [];
    return siblings[siblings.indexOf(this) + 1] || null;
  }

  /** appendChild 移动同一对象而非复制，保留其内部定义与所有引用。 */
  appendChild(node: ElementNode): ElementNode {
    if (node.parentElement)
      node.parentElement.children = node.parentElement.children.filter((item) => item !== node);
    node.parentElement = this;
    this.children.push(node);
    this.appends++;
    return node;
  }

  /** getBoundingClientRect 返回可独立于内联样式设置的实际尺寸。 */
  getBoundingClientRect() {
    return this.rect;
  }

  /** querySelector 只模拟脚本要求的定义标签查询。 */
  querySelector(selector: string) {
    return selector === "defs, symbol" && this.definitions ? { tagName: "defs" } : null;
  }
}

/** symbols 创建保留精确供应商标识和 SVG definitions 的零尺寸容器。 */
function symbols(): ElementNode {
  const node = new ElementNode("DIV", phoenixID);
  const svg = new ElementNode("svg");
  svg.namespaceURI = "http://www.w3.org/2000/svg";
  svg.definitions = true;
  node.appendChild(svg);
  return node;
}

/** fixture 在隔离 VM 中执行与 beforeInteractive 完全相同的脚本文本并记录观察范围。 */
function fixture(children: ElementNode[], withBody = true) {
  const body = new ElementNode("BODY");
  for (const child of children) body.appendChild(child);
  body.appends = 0;
  const root = new ElementNode("HTML");
  if (withBody) root.appendChild(body);
  const events = new Map<string, Set<() => void>>();
  const observers: Observer[] = [];

  /** Observer 记录激活目标，仅在测试明确通知目标变化时触发。 */
  class Observer {
    callback: () => void;
    target: unknown;
    options: { childList?: boolean; subtree?: boolean } = {};
    active = false;

    /** constructor 保留回调，供 VM 使用原生观察器相同的构造方式。 */
    constructor(callback: () => void) {
      this.callback = callback;
      observers.push(this);
    }

    /** observe 记录当前唯一目标和选项。 */
    observe(target: unknown, options: { childList?: boolean; subtree?: boolean }) {
      this.target = target;
      this.options = options;
      this.active = true;
    }

    /** disconnect 停止接收后续变化。 */
    disconnect() {
      this.active = false;
    }
  }

  const document = {
    body: withBody ? body : null,
    documentElement: root,
    /** addEventListener 记录 parser 尚未提供 body 时的备用就绪回调。 */
    addEventListener(name: string, listener: () => void) {
      if (!events.has(name)) events.set(name, new Set());
      events.get(name)!.add(listener);
    },
    /** removeEventListener 撤销已就绪或已水合的监听。 */
    removeEventListener(name: string, listener: () => void) {
      events.get(name)?.delete(listener);
    },
  };
  const window: Record<string, unknown> = {};
  const context = { document, window, MutationObserver: Observer };
  runInNewContext(browserSymbolCompatibilityScript, context);
  return {
    body,
    root,
    document,
    window,
    observers,
    events,
    /** notify 模拟指定父元素的直接插入，不把新注册观察器重复计入本次通知。 */
    notify(target: ElementNode) {
      for (const observer of [...observers])
        if (observer.active && observer.target === target) observer.callback();
    },
  };
}

test("精确扩展容器移至body尾，原节点、SVG定义和应用顺序全部保留", () => {
  const external = symbols();
  const svg = external.firstElementChild;
  const metadata = new ElementNode("DIV", "metadata");
  const app = new ElementNode("DIV", "app");
  const state = fixture([external, metadata, app]);
  assert.deepEqual(state.body.children, [metadata, app, external]);
  assert.equal(external.firstElementChild, svg);
  assert.equal(external.id, phoenixID);
  assert.equal(state.body.appends, 1);
  state.notify(state.body);
  assert.equal(state.body.appends, 1, "自身重排不能引起观察循环");
  assert.equal(state.observers[0].target, state.body);
  assert.equal(state.observers[0].options.childList, true);
  assert.equal(state.observers[0].options.subtree, undefined);
});

test("未知标识、非零尺寸、普通图形及业务子树都不会被移动", () => {
  const variants = [
    /** 改成业务标识，不能被供应商规则匹配。 */
    (node: ElementNode) => {
      node.id = "business-symbols";
    },
    /** 内联尺寸不为零，即使当前没有布局也不能移动。 */
    (node: ElementNode) => {
      node.style.width = "1px";
    },
    /** 有实际可见尺寸时不能移动。 */
    (node: ElementNode) => {
      node.rect.height = 1;
    },
    /** 普通SVG图形没有definitions签名。 */
    (node: ElementNode) => {
      node.firstElementChild!.definitions = false;
    },
    /** HTML中同名标签不能冒充SVG命名空间。 */
    (node: ElementNode) => {
      node.firstElementChild!.namespaceURI = "http://www.w3.org/1999/xhtml";
    },
    /** 混有其它内容的容器不属于单一SVG definitions。 */
    (node: ElementNode) => {
      node.appendChild(new ElementNode("INPUT"));
    },
  ];
  for (const change of variants) {
    const external = symbols();
    change(external);
    const app = new ElementNode("DIV", "app");
    const state = fixture([external, app]);
    assert.deepEqual(state.body.children, [external, app]);
    assert.equal(state.body.appends, 0);
  }
  const nested = symbols();
  const app = new ElementNode("DIV", "app");
  app.appendChild(nested);
  const state = fixture([app, new ElementNode("DIV")]);
  assert.equal(nested.parentElement, app);
  assert.equal(state.body.appends, 0);
});

test("等待parser完成body后安置，水合完成即断开所有监听", () => {
  const external = symbols();
  const app = new ElementNode("DIV", "app");
  const state = fixture([external, app], false);
  assert.equal(state.observers[0].target, state.root);
  state.document.body = state.body;
  state.root.appendChild(state.body);
  state.notify(state.root);
  assert.deepEqual(state.body.children, [app, external]);
  assert.equal(state.observers[0].active, false);
  assert.equal(state.observers[1].target, state.body);
  assert.equal(state.events.get("DOMContentLoaded")?.size, 0);
  (state.window[stopKey] as () => void)();
  assert.ok(state.observers.every((observer) => !observer.active));
  assert.equal(state.window[stopKey], undefined);
  state.body.children = [external, app];
  state.notify(state.body);
  assert.deepEqual(state.body.children, [external, app]);
  assert.doesNotThrow(stopBrowserSymbolCompatibility);
});

test("唯一元素或已在尾部的扩展容器不重复重排", () => {
  const external = symbols();
  const state = fixture([external]);
  assert.equal(state.body.appends, 0);
  const app = new ElementNode("DIV", "app");
  state.body.appendChild(app);
  state.notify(state.body);
  assert.deepEqual(state.body.children, [app, external]);
  assert.equal(state.body.appends, 2);
  state.notify(state.body);
  assert.equal(state.body.appends, 2);
});
