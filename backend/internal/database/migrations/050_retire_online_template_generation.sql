-- 文件作用说明：停用已下线的在线装修代码生成任务，保留历史模板、发布快照与用量记录，防止旧任务阻止模板操作。
UPDATE ai_design_jobs SET status='cancelled',error_code='feature_retired',error_message='在线代码生成已下线，请使用模板ZIP和外部AI编辑',completed_at=coalesce(completed_at,now()) WHERE status IN('queued','running','validating');
UPDATE assistant_jobs SET status='cancelled',error_code='feature_retired',error_message='在线代码生成已下线，请使用模板ZIP和外部AI编辑',completed_at=coalesce(completed_at,now()) WHERE status IN('queued','running','validating');
DELETE FROM ai_design_queue;
DELETE FROM assistant_queue;
