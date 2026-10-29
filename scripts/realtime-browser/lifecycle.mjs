/** 文件作用说明：正常Chrome原生CDP下并行验收真实隐藏超过五分钟与真实冻结超过70秒，各冻结/后台期间通过正式API发送55条并按服务端消息及DOM逐编号对账；不经Playwright、不伪造getter。 */
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { resolve, join } from "node:path";
import { parseArgs } from "node:util";
import { setTimeout as delay } from "node:timers/promises";
import { randomUUID } from "node:crypto";
import { connectNative, launchChrome, probeLifecycle } from "./cdp.mjs";
const { values } = parseArgs({
  options: {
    fixture: { type: "string" },
    output: { type: "string" },
    chrome: { type: "string" },
    probe: { type: "boolean" },
    help: { type: "boolean" },
  },
});
if (values.help) {
  console.log(
    "用法：node scripts/realtime-browser/lifecycle.mjs --fixture <私有fixture.json> --output <私有目录> [--chrome <已安装Chrome路径>]；短自检：--probe --output <私有目录>",
  );
  process.exit(0);
}
if (!values.output || (!values.probe && !values.fixture))
  throw Error("必须显式提供--output，以及正式验收的--fixture");
const dir = resolve(values.output) + "/";
await mkdir(dir, { recursive: true, mode: 0o700 });
let fixture = null;
if (!values.probe) {
  try {
    fixture = JSON.parse(await readFile(resolve(values.fixture), "utf8"));
  } catch {
    throw Error("无法读取或解析私有fixture；未输出其内容");
  }
}
const owner = fixture?.accounts?.find((a) => a.role === "owner"),
  base = fixture?.base_url,
  reference = fixture?.links?.load;
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i;
if (fixture) {
  let origin;
  try {
    origin = new URL(base);
  } catch {
    throw Error("fixture本机来源格式不正确");
  }
  if (
    origin.hostname !== "127.0.0.1" ||
    origin.protocol !== "http:" ||
    origin.origin !== base ||
    origin.username ||
    origin.password
  )
    throw Error("工具只允许显式http://127.0.0.1本机来源，拒绝重定向和外部服务");
  if (
    !String(fixture.name || "").startsWith("演示数据") ||
    !uuid.test(fixture.tenant_id) ||
    !owner ||
    !uuid.test(owner.member_id) ||
    !String(owner.cookie || "").match(/^[A-Za-z0-9_-]{43}$/) ||
    !reference ||
    reference.tenant_id !== fixture.tenant_id ||
    !uuid.test(reference.service_group_id) ||
    !uuid.test(reference.routing_pool_id)
  )
    throw Error("必须使用ops生成的独立演示租户fixture，不接受普通业务租户");
  const meResponse = await fetch(base + "/api/v1/auth/me", {
    headers: {
      Cookie: "support_session=" + owner.cookie,
      "X-Tenant-ID": fixture.tenant_id,
    },
    redirect: "error",
  });
  const me = await meResponse.json().catch(() => ({}));
  const member = me.data?.memberships?.find(
    (m) => m.id === owner.member_id && m.tenant_id === fixture.tenant_id,
  );
  if (
    !meResponse.ok ||
    !member ||
    member.role !== "owner" ||
    !member.can_serve ||
    member.tenant_name !== fixture.name ||
    !member.tenant_name.startsWith("演示数据")
  )
    throw Error("真实登录身份不匹配独立演示租户owner；尚未创建任何演示入口");
}
const chrome = await launchChrome(dir, values.chrome);
let cdp;
const contexts = [],
  targets = [];
const report = {
  environment: {
    normalChrome: true,
    playwrightAttached: false,
    focusOrVisibilityOverrides: false,
  },
  freeze: {},
  background: {},
  progress: [],
};
/** clock 使用单调时钟计算真实持续时长，不能用调快测试时钟代替。 */
const clock = () => performance.now();
const runTag = Date.now().toString(36);
/** api 只对独立测试租户调用正式HTTP，身份和票据不进入日志。 */
async function api(method, path, body, token) {
  const response = await fetch(base + "/api/v1" + path, {
    method,
    redirect: "error",
    headers: {
      Origin: base,
      ...(token
        ? { Authorization: "Bearer " + token }
        : {
            Cookie: "support_session=" + owner.cookie,
            "X-Tenant-ID": fixture.tenant_id,
          }),
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  const envelope = await response.json().catch(() => ({}));
  if (!response.ok)
    throw Error(
      "原生测试HTTP " +
        response.status +
        " " +
        String(envelope.code || "request_failed"),
    );
  return envelope.data;
}
/** evaluate 读取单个探针页的原生DOM事实。 */
const evaluate = (page, expression) => cdp.evaluate(page.session, expression);
/** waitUntil 等待真实DOM达到条件，不手动派发页面生命周期事件。 */
async function waitUntil(check, label, timeout = 30000) {
  const started = clock();
  while (clock() - started < timeout) {
    const value = await check();
    if (value) return value;
    await delay(150);
  }
  throw Error("等待超时：" + label);
}
/** visitorPage 在独立Chrome浏览器上下文自然建立访客身份，不复用root访客或改接待意图。 */
async function visitorPage(label) {
  const entry = await api("POST", "/tenant/access-links", {
    name: "演示数据·原生" + label + "-" + runTag,
    service_group_id: reference.service_group_id,
    routing_pool_id: reference.routing_pool_id,
    agent_mode: "prefer",
    reception_mode: "manual",
    status: "active",
    channel_tags: ["演示数据", "native-" + label],
    required_skill_ids: [],
    optional_skill_ids: [],
  });
  const { browserContextId } = await cdp.send("Target.createBrowserContext", {
    disposeOnDetach: false,
  });
  contexts.push(browserContextId);
  const { targetId } = await cdp.send("Target.createTarget", {
    url: "about:blank",
    browserContextId,
  });
  targets.push(targetId);
  const { sessionId: session } = await cdp.send("Target.attachToTarget", {
    targetId,
    flatten: true,
  });
  await cdp.send("Page.enable", {}, session);
  await cdp.send("Runtime.enable", {}, session);
  await cdp.send(
    "Page.addScriptToEvaluateOnNewDocument",
    {
      source:
        'window.__nativeTicks=0;window.__nativeLifecycle=[];setInterval(()=>window.__nativeTicks++,100);for(const type of ["visibilitychange","freeze","resume"])document.addEventListener(type,()=>window.__nativeLifecycle.push({type,visibility:document.visibilityState,wall:Date.now()}));',
    },
    session,
  );
  const page = { label, targetId, session, entry, expected: [] };
  await cdp.send(
    "Page.navigate",
    {
      url:
        base +
        "/c/" +
        entry.entry_key +
        "?姓名=" +
        encodeURIComponent("演示数据·原生" + label + "-" + runTag),
    },
    session,
  );
  const key =
    "goodchat.visitor-session.v1." +
    encodeURIComponent(entry.service_space_id) +
    "." +
    encodeURIComponent(entry.entry_key);
  const identity = await waitUntil(async () => {
    try {
      return await evaluate(
        page,
        "location.origin===" +
          JSON.stringify(base) +
          "?(()=>{const s=JSON.parse(localStorage.getItem(" +
          JSON.stringify(key) +
          ')||"null");return s?.visitor_token&&s?.conversation_id?{token:s.visitor_token,id:s.conversation_id}:null})():null',
      );
    } catch {
      return null;
    }
  }, "新访客真实初始化");
  page.token = identity.token;
  page.conversation = identity.id;
  await api(
    "POST",
    "/conversations/" + page.conversation + "/assignments/claim",
    {},
  );
  await waitUntil(
    () =>
      evaluate(
        page,
        'document.body.innerText.includes("正在接待")&&document.body.innerText.includes("实时连接已建立")',
      ),
    "访客实际接待且同步",
  );
  return page;
}
/** nativeState 记录真正的可见性与探针计时，绝不覆写浏览器属性。 */
const nativeState = (page) =>
  evaluate(
    page,
    "({visibility:document.visibilityState,hidden:document.hidden,ticks:window.__nativeTicks,lifecycle:window.__nativeLifecycle,wall:Date.now()})",
  );
/** sendBatch 在目标页无法执行JS或处于后台时，经在线服务正式发送55条并保存原始消息身份。 */
async function sendBatch(page) {
  for (let index = 0; index < 55; index++) {
    const client = randomUUID();
    const saved = await api(
      "POST",
      "/conversations/" + page.conversation + "/messages",
      {
        client_message_id: client,
        content: "演示数据·原生" + page.label + "·后台消息" + index,
      },
    );
    page.expected.push({ id: saved.id, seq: saved.seq, client });
  }
}
/** reconcile 将正式HTTP数据库投影、消息DOM按每个客户端编号交叉核验，不能只数总行数。 */
async function reconcile(page) {
  await cdp.send("Browser.setWindowBounds", {
    windowId: page.windowId,
    bounds: { windowState: "normal" },
  });
  await cdp.send("Target.activateTarget", { targetId: page.targetId });
  const clients = page.expected.map((item) => item.client);
  const dom = await waitUntil(async () => {
    const value = await evaluate(
      page,
      "(()=>{const expected=" +
        JSON.stringify(clients) +
        ';const nodes=[...document.querySelectorAll(".message-row[data-client-message-id]")];const ids=nodes.map(n=>n.getAttribute("data-client-message-id"));return {ready:expected.every(id=>ids.filter(found=>found===id).length===1),matched:expected.filter(id=>ids.includes(id)).length,unique:ids.length===new Set(ids).size,visibility:document.visibilityState,status:document.body.innerText.includes("实时连接已建立")}})()',
    );
    return value.ready && value.status ? value : null;
  }, page.label + "逐条恢复");
  const data = await api(
    "GET",
    "/conversations/" + page.conversation + "/messages?limit=100",
    undefined,
    page.token,
  );
  const matched = page.expected.filter((expected) =>
    data.items.some(
      (item) =>
        item.id === expected.id &&
        item.client_message_id === expected.client &&
        item.seq === expected.seq,
    ),
  );
  if (matched.length !== 55 || !dom.unique)
    throw Error("服务端/DOM逐编号对账不一致");
  const screenshot = await cdp.send(
    "Page.captureScreenshot",
    { format: "png" },
    page.session,
  );
  await writeFile(
    dir + "native-" + page.label + "-recovered.png",
    Buffer.from(screenshot.data, "base64"),
    { mode: 0o600 },
  );
  return {
    expected: 55,
    serverMatched: matched.length,
    domMatched: dom.matched,
    allIDsExactlyOnce: dom.unique,
    connected: dom.status,
  };
}
/** saveProgress 只保存脱敏状态，便于长测期间给root提供可验证进度。 */
async function saveProgress(stage) {
  report.progress.push({ stage, wall: Date.now() });
  await writeFile(
    dir + "native-lifecycle-progress.json",
    JSON.stringify(report, null, 2),
    { mode: 0o600 },
  );
  console.log(JSON.stringify({ stage, wall: Date.now() }));
}
try {
  cdp = await connectNative(chrome.profile);
  if (values.probe) {
    const proof = await probeLifecycle(cdp);
    await writeFile(
      join(dir, "probe-result.json"),
      JSON.stringify(proof, null, 2),
      { mode: 0o600 },
    );
    console.log(JSON.stringify(proof));
  } else {
    await saveProgress("starting");
    const frozen = await visitorPage("freeze"),
      background = await visitorPage("background");
    const cover = await cdp.send("Target.createTarget", { url: "about:blank" });
    targets.push(cover.targetId);
    await cdp.send("Target.activateTarget", { targetId: cover.targetId });
    await delay(250);
    for (const candidate of [frozen, background]) {
      const found = await cdp.send("Browser.getWindowForTarget", {
        targetId: candidate.targetId,
      });
      candidate.windowId = found.windowId;
      await cdp.send("Browser.setWindowBounds", {
        windowId: candidate.windowId,
        bounds: { windowState: "minimized" },
      });
    }
    await delay(350);
    const backgroundStart = clock(),
      freezeStart = clock();
    const beforeFreeze = await nativeState(frozen),
      beforeBackground = await nativeState(background);
    if (
      beforeFreeze.visibility !== "hidden" ||
      beforeBackground.visibility !== "hidden"
    )
      throw Error("正式计时前页面没有真实hidden");
    report.freeze.before = beforeFreeze;
    report.background.before = beforeBackground;
    report.background.samples = [];
    await cdp.send(
      "Page.setWebLifecycleState",
      { state: "frozen" },
      frozen.session,
    );
    await saveProgress("both-native-hidden-freeze-started");
    await sendBatch(frozen);
    await sendBatch(background);
    await saveProgress("55-each-persisted-while-hidden");
    while (clock() - freezeStart < 71000) {
      await delay(Math.min(10000, 71000 - (clock() - freezeStart)));
      const state = await nativeState(background);
      report.background.samples.push(state);
      if (state.visibility !== "hidden")
        throw Error("自然后台页计时中意外可见");
    }
    const contact = await api("GET", "/conversations/" + frozen.conversation);
    report.freeze.contactStillOnlineWhileJSFrozen = contact.contact_online;
    await cdp.send(
      "Page.setWebLifecycleState",
      { state: "active" },
      frozen.session,
    );
    const afterFreeze = await nativeState(frozen),
      frozenMs = clock() - freezeStart;
    const tickDelta = afterFreeze.ticks - beforeFreeze.ticks;
    if (
      frozenMs < 70000 ||
      tickDelta > 2 ||
      !afterFreeze.lifecycle.some((event) => event.type === "freeze")
    )
      throw Error("冻结时长或计时器停止证据不成立");
    report.freeze = {
      ...report.freeze,
      after: afterFreeze,
      elapsedMs: Math.round(frozenMs),
      tickDelta,
      ...(await reconcile(frozen)),
    };
    await cdp.send("Target.activateTarget", { targetId: cover.targetId });
    await saveProgress("freeze-70s-recovered-and-reconciled");
    while (clock() - backgroundStart < 305000) {
      await delay(Math.min(15000, 305000 - (clock() - backgroundStart)));
      const state = await nativeState(background);
      report.background.samples.push(state);
      if (state.visibility !== "hidden")
        throw Error("自然后台页计时中意外可见");
      if (report.background.samples.length % 4 === 0)
        await saveProgress(
          "background-still-hidden-" +
            Math.floor((clock() - backgroundStart) / 1000) +
            "s",
        );
    }
    const afterBackground = await nativeState(background),
      hiddenMs = clock() - backgroundStart;
    if (
      hiddenMs < 300000 ||
      afterBackground.visibility !== "hidden" ||
      afterBackground.lifecycle.some((event) => event.type === "freeze")
    )
      throw Error("自然隐藏五分钟证据不成立");
    report.background = {
      ...report.background,
      after: afterBackground,
      hiddenMs: Math.round(hiddenMs),
      allSamplesHidden: report.background.samples.every(
        (sample) => sample.visibility === "hidden",
      ),
      ...(await reconcile(background)),
    };
    await saveProgress("completed");
    await writeFile(
      dir + "native-lifecycle-result.json",
      JSON.stringify(report, null, 2),
      { mode: 0o600 },
    );
    console.log(
      JSON.stringify({
        completed: true,
        freeze: {
          elapsedMs: report.freeze.elapsedMs,
          tickDelta: report.freeze.tickDelta,
          serverMatched: report.freeze.serverMatched,
          domMatched: report.freeze.domMatched,
        },
        background: {
          hiddenMs: report.background.hiddenMs,
          allSamplesHidden: report.background.allSamplesHidden,
          serverMatched: report.background.serverMatched,
          domMatched: report.background.domMatched,
        },
      }),
    );
  }
} catch (error) {
  report.failure = String(error.message || error);
  await writeFile(
    dir + "native-lifecycle-result.json",
    JSON.stringify(report, null, 2),
    { mode: 0o600 },
  );
  console.error(report.failure);
  process.exitCode = 1;
} finally {
  if (cdp) {
    for (const context of contexts)
      await cdp
        .send("Target.disposeBrowserContext", { browserContextId: context })
        .catch(() => {});
    for (const target of targets)
      await cdp
        .send("Target.closeTarget", { targetId: target })
        .catch(() => {});
    await cdp.send("Browser.close").catch(() => {});
    cdp.close();
  }
  await chrome.close();
}
