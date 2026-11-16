-- 文件作用说明：冻结 AI 创作任务的默认输出语言，后台重启或用户切换语言不改变排队任务目标。
ALTER TABLE assistant_jobs ADD COLUMN target_locale text NOT NULL DEFAULT 'zh-CN';
COMMENT ON COLUMN assistant_jobs.target_locale IS '提交时冻结的默认用户可见说明及新页面文案语言，用户明确指定内容语言时按其要求';
ALTER TABLE ai_design_jobs ADD COLUMN target_locale text NOT NULL DEFAULT 'zh-CN';
COMMENT ON COLUMN ai_design_jobs.target_locale IS '旧版AI装修任务提交时冻结的默认输出语言，不改写用户原始需求';
