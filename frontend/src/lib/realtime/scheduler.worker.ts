/** 文件作用说明：在浏览器Worker中调度实时连接检查，减少主界面繁忙对心跳调度的影响，冻结与系统休眠后仍由恢复流程兜底。 */
const workerClock = globalThis as unknown as { postMessage: (value: unknown) => void };
setInterval(() => workerClock.postMessage({ type: "tick" }), 5000);
export {};
