/** 文件作用说明：统一个人语言、时区的时间与数字格式，同时允许报表显式指定企业时区。 */
import { currentPreferences } from "./runtime.ts";

/** dateFormatter 使用个人显示时区，传入业务时区时保留明确的统计口径。 */
export function dateFormatter(options: Intl.DateTimeFormatOptions = {}) {
  const preferences = currentPreferences();
  return new Intl.DateTimeFormat(preferences.locale, {
    timeZone: preferences.timezone,
    ...options,
  });
}

/** numberFormatter 每次格式化读取当前语言，避免模块初始化时永久固定中文。 */
export function numberFormatter(options: Intl.NumberFormatOptions = {}) {
  return {
    format: (value: number | bigint) =>
      new Intl.NumberFormat(currentPreferences().locale, options).format(value),
  };
}
