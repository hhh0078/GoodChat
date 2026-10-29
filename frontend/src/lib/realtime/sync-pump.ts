/** 文件作用说明：合并实时事件和轮询触发的补拉，保留进行中收到的唤醒，防止消息恰好落在同步末尾时遗漏。 */
/** createSyncPump 串行执行补拉，并使并发调用等待直到所有已收到的唤醒都完成。 */
export function createSyncPump(run: () => Promise<void>) {
  let pending = false;
  let running: Promise<void> | null = null;
  let stopped = false;
  /** trigger 记录唤醒后启动或复用唯一补拉任务。 */
  function trigger(): Promise<void> {
    if (stopped) return Promise.resolve();
    pending = true;
    if (running) return running;
    let finished = false;
    const task = (async () => {
      try {
        while (pending && !stopped) {
          pending = false;
          await run();
        }
      } finally {
        // 清理与最后一次pending检查在同一微任务内完成，不能留下已退出但仍可复用的旧Promise。
        finished = true;
        running = null;
      }
    })();
    // run可能同步抛错；此时finally已经清理，不能重新挂回一个已拒绝的任务。
    if (!finished) running = task;
    return task;
  }
  return {
    trigger,
    stop: () => {
      stopped = true;
      pending = false;
    },
  };
}
