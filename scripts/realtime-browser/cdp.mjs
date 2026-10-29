/** 文件作用说明：为独立正常Chrome临时profile提供极小原生CDP客户端，不经过Playwright且不设置focus/visibility模拟。 */
import { readFile, mkdtemp, mkdir, open } from "node:fs/promises";
import { join } from "node:path";
import { spawn } from "node:child_process";
import { setTimeout as delay } from "node:timers/promises";
/** connectNative 读取仅本测试profile的调试端点，连接时不把能力URL写入报告。 */
export async function connectNative(profile) {
  let lines;
  for (let n = 0; n < 40; n++) {
    try {
      lines = (await readFile(profile + "/DevToolsActivePort", "utf8"))
        .trim()
        .split("\n");
      break;
    } catch {
      await delay(200);
    }
  }
  if (!lines) throw Error("独立Chrome尚未生成调试端点");
  if (
    !/^\d+$/.test(lines[0]) ||
    !/^\/devtools\/browser\/[a-f0-9-]+$/i.test(lines[1])
  )
    throw Error("独立Chrome调试端点格式不正确");
  const socket = new WebSocket("ws://127.0.0.1:" + lines[0] + lines[1]);
  await new Promise((resolve, reject) => {
    socket.addEventListener("open", resolve, { once: true });
    socket.addEventListener("error", () => reject(Error("独立CDP连接失败")), {
      once: true,
    });
  });
  let serial = 0;
  const pending = new Map();
  socket.addEventListener("close", () => {
    for (const call of pending.values()) {
      clearTimeout(call.timeout);
      call.reject(Error("独立CDP连接已关闭"));
    }
    pending.clear();
  });
  socket.addEventListener("message", (event) => {
    const item = JSON.parse(String(event.data));
    if (item.id) {
      const p = pending.get(item.id);
      if (p) {
        pending.delete(item.id);
        clearTimeout(p.timeout);
        item.error
          ? p.reject(Error("CDP " + item.error.message))
          : p.resolve(item.result);
      }
    }
  });
  /** send 向指定扁平会话发送原生CDP命令并等待对应响应。 */
  function send(method, params = {}, sessionId) {
    const id = ++serial;
    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        pending.delete(id);
        reject(Error("独立CDP命令超时"));
      }, 30000);
      pending.set(id, { resolve, reject, timeout });
      socket.send(
        JSON.stringify({
          id,
          method,
          params,
          ...(sessionId ? { sessionId } : {}),
        }),
      );
    });
  }
  /** evaluate 只读取或安装本测试探针，不覆写浏览器原生可见性属性。 */
  async function evaluate(sessionId, expression) {
    const value = await send(
      "Runtime.evaluate",
      { expression, returnByValue: true, awaitPromise: true },
      sessionId,
    );
    if (value.exceptionDetails) throw Error("探针脚本执行失败");
    return value.result.value;
  }
  return {
    send,
    evaluate,
    close: () => socket.close(),
  };
}

/** launchChrome 仅启动本工具自己的临时profile，保留正常Chrome后台策略，日志只写私有输出目录。 */
export async function launchChrome(
  output,
  executable = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
) {
  await mkdir(output, { recursive: true, mode: 0o700 });
  const profile = await mkdtemp(join(output, "chrome-profile-"));
  const log = await open(join(output, "chrome-launch.log"), "a", 0o600);
  const child = spawn(
    executable,
    [
      "--user-data-dir=" + profile,
      "--remote-debugging-port=0",
      "--no-first-run",
      "--no-default-browser-check",
      "about:blank",
    ],
    { stdio: ["ignore", log.fd, log.fd] },
  );
  await new Promise((resolve, reject) => {
    child.once("spawn", resolve);
    child.once("error", () =>
      reject(Error("正常Chrome启动失败，请用--chrome指定已安装浏览器")),
    );
  });
  return {
    profile,
    /** close 只回收本工具启动的进程，不影响其他Chrome或Playwright会话。 */ async close() {
      if (child.exitCode === null && !child.killed) child.kill("SIGTERM");
      await log.close();
    },
  };
}

/** probeLifecycle 仅用自有空白页验证原生hidden和短冻结计时，绝不调用应用或覆写可见性getter。 */
export async function probeLifecycle(cdp) {
  const target = (await cdp.send("Target.getTargets")).targetInfos.find(
    (t) => t.type === "page",
  );
  const cover = await cdp.send("Target.createTarget", { url: "about:blank" });
  const session = (
    await cdp.send("Target.attachToTarget", {
      targetId: target.targetId,
      flatten: true,
    })
  ).sessionId;
  await cdp.send("Page.enable", {}, session);
  await cdp.send("Runtime.enable", {}, session);
  await cdp.evaluate(
    session,
    'window.probeTicks=0;window.probeChanges=[];setInterval(()=>window.probeTicks++,100);for(const type of ["freeze","resume"])document.addEventListener(type,()=>window.probeChanges.push(type));true',
  );
  await cdp.send("Target.activateTarget", { targetId: cover.targetId });
  await delay(300);
  const before = await cdp.evaluate(
    session,
    "({visibility:document.visibilityState,hidden:document.hidden,ticks:window.probeTicks,wall:Date.now()})",
  );
  if (before.visibility !== "hidden")
    throw Error("短probe没有真实hidden，禁止开始正式验收");
  await cdp.send("Page.setWebLifecycleState", { state: "frozen" }, session);
  await delay(2200);
  await cdp.send("Page.setWebLifecycleState", { state: "active" }, session);
  const after = await cdp.evaluate(
    session,
    "({visibility:document.visibilityState,hidden:document.hidden,ticks:window.probeTicks,wall:Date.now(),events:window.probeChanges})",
  );
  const proof = {
    before,
    after,
    elapsedMs: after.wall - before.wall,
    tickDelta: after.ticks - before.ticks,
    nativeChrome: true,
    playwrightAttached: false,
  };
  if (
    proof.elapsedMs < 2100 ||
    proof.tickDelta > 1 ||
    !after.events.includes("freeze") ||
    !after.events.includes("resume") ||
    after.visibility !== "hidden"
  )
    throw Error("短probe真实冻结计时校验失败");
  return proof;
}
