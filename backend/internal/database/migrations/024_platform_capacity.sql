-- 文件作用说明：保存总管理员录入的已完成业务压测基线；环境规格变化时估算失效，可删除错误或演示记录。
CREATE TABLE platform_capacity_baseline(
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 tested_people integer NOT NULL CHECK(tested_people BETWEEN 1 AND 10000000),
 safety_factor double precision NOT NULL DEFAULT 0.7 CHECK(safety_factor BETWEEN 0.3 AND 0.9),
 test_date date NOT NULL,
 notes text NOT NULL CHECK(length(notes) BETWEEN 1 AND 2000),
 resource_fingerprint text NOT NULL CHECK(length(resource_fingerprint)=64),
 resource_spec jsonb NOT NULL CHECK(jsonb_typeof(resource_spec)='object'),
 recorded_by uuid NOT NULL REFERENCES users(id),
 updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE platform_capacity_baseline IS '平台单例业务压测基线；仅总管理员接口可写，属于管理员录入而非系统自动验证';
COMMENT ON COLUMN platform_capacity_baseline.singleton IS '固定为真，保证全平台只有一份当前压测记录';
COMMENT ON COLUMN platform_capacity_baseline.tested_people IS '已完成压测的同时在线人数，不向更大未测并发外推';
COMMENT ON COLUMN platform_capacity_baseline.safety_factor IS '并发安全系数，默认0.7，范围0.3至0.9';
COMMENT ON COLUMN platform_capacity_baseline.test_date IS '实际完成业务压测的日历日期';
COMMENT ON COLUMN platform_capacity_baseline.notes IS '管理员填写的测试场景、消息频率及结果说明';
COMMENT ON COLUMN platform_capacity_baseline.resource_fingerprint IS '记录时的监控范围、有效CPU及内存规格SHA256，变化后基线失效';
COMMENT ON COLUMN platform_capacity_baseline.resource_spec IS '记录时实际采集的资源规格，不接受客户端覆盖';
COMMENT ON COLUMN platform_capacity_baseline.recorded_by IS '录入压测记录的总管理员账号';
COMMENT ON COLUMN platform_capacity_baseline.updated_at IS '最近一次录入或重新校准时间';
