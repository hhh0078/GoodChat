/** 文件作用说明：在一个应用容器内监督Go、Next.js和可信网关进程，任何进程退出时整体停止。 */
import { spawn } from "node:child_process";

const processes = [];
let stopping = false;

/** 启动一个业务子进程，并在异常退出时关闭其他子进程。 */
function launch(command, args, env = {}) {
  const child = spawn(command, args, { stdio: "inherit", env: { ...process.env, ...env } });
  processes.push(child);
  child.on("error", () => stop(1));
  child.on("exit", (code) => { if (!stopping) stop(code || 1); });
  return child;
}

/** 向全部进程传递停止信号，超时后强制退出，避免孤儿服务继续工作。 */
function stop(code = 0) {
  if (stopping) return;
  stopping = true;
  for (const child of processes) child.kill("SIGTERM");
  const deadline = setTimeout(() => { for (const child of processes) child.kill("SIGKILL"); process.exit(code); }, 10000);
  Promise.all(processes.map((child) => new Promise((resolve) => child.exitCode !== null ? resolve() : child.once("exit", resolve)))).then(() => { clearTimeout(deadline); process.exit(code); });
}

process.on("SIGTERM", () => stop(0));
process.on("SIGINT", () => stop(0));
launch("/app/goodchat", []);
launch(process.execPath, ["/app/frontend/server.js"], { PORT: "3001", HOSTNAME: "127.0.0.1" });
launch(process.execPath, ["/app/deploy/gateway.mjs"]);
