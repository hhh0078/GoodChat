-- 文件作用说明：为异步导出冻结任务提交时的系统文案语言，保留原有统计时区。
ALTER TABLE chat_export_jobs ADD COLUMN locale text NOT NULL DEFAULT 'zh-CN';
COMMENT ON COLUMN chat_export_jobs.locale IS '创建导出任务时冻结的表头及系统标签语言';
