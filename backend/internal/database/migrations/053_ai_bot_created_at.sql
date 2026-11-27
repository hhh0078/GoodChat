-- 文件作用说明：记录机器人的固定创建时间，新增配置不改变未明确绑定入口与个人助手的既有回退选择。
-- 历史配置统一取本次迁移时间，继续以原编号排序；之后新增的配置排在它们之后，编辑不改变顺序。
ALTER TABLE ai_bots ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();
-- 新配置使用实际插入时间，较早开启但等待成员锁的事务不能把新机器人排到旧配置之前。
ALTER TABLE ai_bots ALTER COLUMN created_at SET DEFAULT clock_timestamp();
COMMENT ON COLUMN ai_bots.created_at IS '机器人创建时间；历史配置统一为迁移时间，隐式选择优先较早配置且不受编辑影响';
