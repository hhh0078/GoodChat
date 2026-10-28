/** 文件作用说明：在独立Worker中触发接待提醒核验，减轻后台主线程定时器限流，冻结及睡眠由恢复补查兜底。 */
const reminderClock = globalThis as unknown as { postMessage: (value: unknown) => void };
setInterval(() => reminderClock.postMessage({ type: "attention-check" }), 2000);
export {};
