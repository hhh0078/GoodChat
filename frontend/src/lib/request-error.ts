/** 文件作用说明：将请求错误发送到统一确认弹窗，合并并发失败，避免多个页面重复覆盖。 */
import { ApiError, errorMessage } from "./api";
export type RequestFailure = { key: string; content: string; retry?: () => void };
const pending: RequestFailure[] = [];
let listener: (() => void) | undefined;
/** showRequestError 保留安全错误说明与可选重试，重复错误在确认前只保留一份。 */
export function showRequestError(error: unknown, retry?: () => void) {
  const content = errorMessage(error);
  const key =
    error instanceof ApiError ? `${error.status}:${error.code}:${error.message}` : content;
  if (pending.some((item) => item.key === key)) return;
  pending.push({ key, content, retry });
  listener?.();
}
/** subscribeRequestErrors 订阅当前待确认错误，组件卸载后释放监听。 */
export function subscribeRequestErrors(update: () => void) {
  listener = update;
  update();
  return () => {
    if (listener === update) listener = undefined;
  };
}
/** currentRequestError 获取队首错误，不在未确认时自动消失。 */
export function currentRequestError() {
  return pending[0];
}
/** acknowledgeRequestError 仅移除用户已经确认的错误。 */
export function acknowledgeRequestError() {
  pending.shift();
  listener?.();
}
