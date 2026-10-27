/** 文件作用说明：执行代码装修初稿，验证真实输入状态、本人气泡、引用与大序号已读展示。 */
import assert from "node:assert/strict";
import test from "node:test";
import vm from "node:vm";
import { starterCodeDesign } from "../src/features/decoration/starter-code.ts";

/** ElementStub 提供初稿所需的最小文档节点，记录文本而不解释租户 HTML。 */
class ElementStub {
  children: ElementStub[] = [];
  textContent = "";
  className = "";
  disabled = false;
  hidden = false;
  value = "";
  onclick: (() => unknown) | null = null;
  scrollHeight = 100;
  scrollTop = 0;
  clientHeight = 100;
  /** append 记录安全文本节点，便于断言聊天内容。 */
  append(node: ElementStub) {
    this.children.push(node);
  }
  /** replaceChildren 清理上次渲染结果。 */
  replaceChildren() {
    this.children = [];
  }
  /** addEventListener 忽略本用例不触发的输入事件。 */
  addEventListener() {}
  /** focus 保持初稿焦点操作可在无浏览器环境执行。 */
  focus() {}
}

/** mountStarter 执行真实生成代码，并记录白名单SDK调用用于初稿交互验证。 */
function mountStarter() {
  const nodes = new Map<string, ElementStub>();
  const reads: string[] = [];
  const sent: { text: string; id: string; reply?: string }[] = [];
  const withdrawn: string[] = [];
  let subscriber: (state: Record<string, unknown>) => void = () => {};
  const code = starterCodeDesign();
  const context = vm.createContext({
    window: {
      GoodChat: {
        subscribe: (handler: typeof subscriber) => {
          subscriber = handler;
        },
        ready: () => Promise.resolve(),
        read: async (seq: string) => {
          reads.push(seq);
          return { accepted: true };
        },
        sendText: async (text: string, id: string, reply?: string) => {
          sent.push({ text, id, reply });
        },
        withdrawMessage: async (id: string) => {
          withdrawn.push(id);
        },
        typing: async () => {},
      },
    },
    document: {
      visibilityState: "visible",
      getElementById: (id: string) => {
        if (!nodes.has(id)) nodes.set(id, new ElementStub());
        return nodes.get(id);
      },
      createElement: () => new ElementStub(),
      addEventListener: () => {},
    },
    crypto,
  });
  new vm.Script(String(code.javascript)).runInContext(context);
  return {
    nodes,
    reads,
    sent,
    withdrawn,
    render: (state: Record<string, unknown>) => subscriber(state),
  };
}

test("代码初稿显示输入、本人角色和引用，已读游标始终按字符串传输", async () => {
  const { nodes, reads, render } = mountStarter();
  render({
    connected: true,
    is_typing: true,
    peer_read_seq: "9007199254740993",
    conversation: { status: "active" },
    messages: [
      {
        id: "message-a",
        is_own: true,
        sender_kind: "visitor",
        sender_name: "访客",
        created_at: "2026-10-27T08:00:00Z",
        seq: "9007199254740993",
        content: "<script>作为文本展示</script>",
        reply: { content: "引用内容", withdrawn: false },
      },
    ],
  });
  await Promise.resolve();
  assert.equal(nodes.get("typing")?.textContent, "客服正在输入…");
  const bubble = nodes.get("messages")?.children[0];
  assert.equal(bubble?.className, "own");
  assert.match(bubble?.children[0]?.textContent || "", /^我 · 访客 ·/);
  assert.equal(bubble?.children[1]?.textContent, "引用：引用内容");
  assert.equal(bubble?.children[2]?.textContent, "<script>作为文本展示</script>");
  assert.equal(bubble?.children[3]?.textContent, "对方已读");
  assert.deepEqual(reads, ["9007199254740993"]);
  render({
    connected: true,
    is_typing: false,
    conversation: { status: "closed" },
    messages: [],
  });
  assert.equal(nodes.get("typing")?.textContent, "");
  assert.equal(nodes.get("text")?.disabled, true);
});
test("代码初稿引用沿用目标ID，撤回必须先行内联确认", async () => {
  const { nodes, sent, withdrawn, render } = mountStarter();
  render({
    connected: true,
    conversation: { status: "active" },
    messages: [
      {
        id: "message-own",
        is_own: true,
        sender_kind: "visitor",
        content: "原消息",
        seq: "1",
        created_at: new Date().toISOString(),
      },
    ],
  });
  const actions = nodes.get("messages")?.children[0]?.children.at(-1);
  assert.equal(actions?.className, "message-actions");
  await actions?.children[0]?.onclick?.();
  assert.equal(nodes.get("reply")?.hidden, false);
  assert.equal(nodes.get("reply-text")?.textContent, "引用：原消息");
  nodes.get("text")!.value = "引用回复";
  await nodes.get("send")?.onclick?.();
  assert.equal(sent.length, 1);
  assert.equal(sent[0].text, "引用回复");
  assert.equal(sent[0].reply, "message-own");
  assert.match(sent[0].id, /^[\da-f-]{36}$/i);
  assert.equal(nodes.get("reply")?.hidden, true);
  await actions?.children[1]?.onclick?.();
  assert.deepEqual(withdrawn, []);
  assert.equal(actions?.children[1]?.textContent, "确认撤回");
  await actions?.children[1]?.onclick?.();
  assert.deepEqual(withdrawn, ["message-own"]);
});
