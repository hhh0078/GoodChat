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
    running = (async () => {
      while (pending && !stopped) {
        pending = false;
        await run();
      }
    })().finally(() => {
      running = null;
    });
    return running;
  }
  return {
    trigger,
    stop: () => {
      stopped = true;
      pending = false;
    },
  };
}
