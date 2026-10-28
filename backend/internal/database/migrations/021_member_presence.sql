-- 文件作用说明：只依赖身份表保存成员真实在线租约区间；多标签合并续租，不伪造启用前历史。
CREATE TABLE member_presence_intervals(
 id bigserial PRIMARY KEY,
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 membership_id uuid NOT NULL,
 started_at timestamptz NOT NULL,
 last_seen_at timestamptz NOT NULL,
 online_until timestamptz NOT NULL,
 CHECK(last_seen_at>=started_at AND online_until>=last_seen_at),
 FOREIGN KEY(tenant_id,membership_id) REFERENCES tenant_memberships(tenant_id,id)
);
COMMENT ON TABLE member_presence_intervals IS '成员在线租约历史；同成员重叠心跳合并为一个区间，断线最多保留45秒租约，不回填启用前历史';
COMMENT ON COLUMN member_presence_intervals.id IS '在线区间自增编号';
COMMENT ON COLUMN member_presence_intervals.tenant_id IS '在线区间所属租户';
COMMENT ON COLUMN member_presence_intervals.membership_id IS '在线区间所属成员';
COMMENT ON COLUMN member_presence_intervals.started_at IS '本次连续在线区间开始时间';
COMMENT ON COLUMN member_presence_intervals.last_seen_at IS '最近一次真实探活成功时间';
COMMENT ON COLUMN member_presence_intervals.online_until IS '最近探活延长的租约截止时间，统计时裁剪至当前时刻';
CREATE INDEX member_presence_latest ON member_presence_intervals(tenant_id,membership_id,online_until DESC);
CREATE INDEX member_presence_period ON member_presence_intervals(tenant_id,online_until,started_at);
ALTER TABLE member_presence_intervals ENABLE ROW LEVEL SECURITY;
ALTER TABLE member_presence_intervals FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON member_presence_intervals USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
