/** 文件作用说明：通过 next-intl 的请求或客户端上下文创建隔离的文案翻译器。 */
import { useLocale, useMessages } from "next-intl";
import { createTextTranslator, type Messages, type Translator } from "./runtime";

/** useTranslator 在服务端同步组件和客户端组件中使用相同消息目录。 */
export function useTranslator(): Translator {
  return createTextTranslator(useLocale(), useMessages() as Messages);
}
