/** 文件作用说明：集中声明受限桌面桥接，页面不能读取设备凭证或执行系统命令。 */
import { Call, Events } from "@wailsio/runtime";
export type Startup = {
  version: string;
  default_server: string;
  server: string;
};
export type State = {
  scope?: string;
  status: string;
  detail: string;
  pending: number;
  failed: number;
};
export type Pending = {
  id: string;
  conversation: string;
  body: { content?: string; message_type?: string };
  state: string;
  error: string;
};

/** invoke 只调用桌面入口明确暴露的方法。 */
export function invoke<T>(method: string, ...args: unknown[]): Promise<T> {
  return Call.ByName(`main.Desktop.${method}`, ...args) as Promise<T>;
}

/** on 订阅原生事件并返回解绑方法，界面重建时不残留旧订阅。 */
export function on<T>(name: string, receive: (value: T) => void): () => void {
  return Events.On(name, (event) => receive(event.data as T));
}

/** message 输出桥接错误的可读说明。 */
export function message(problem: unknown): string {
  if (typeof problem === "string") return problem;
  if (problem && typeof problem === "object" && "message" in problem)
    return String(problem.message);
  return "操作未完成，请检查连接后重试。";
}
