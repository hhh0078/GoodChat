-- 文件作用说明：以咨询当前接待指针和冻结消息序号确定接待前段，消除事务创建时间排序导致的历史泄漏；修复已知错误起点时仅提高边界。
CREATE OR REPLACE FUNCTION record_assignment_message_boundary() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
DECLARE watermark bigint; current_assignment uuid; previous_end bigint; previous_count bigint; unknown_count bigint; BEGIN
SELECT next_event_seq,current_assignment_id INTO watermark,current_assignment FROM conversations WHERE id=NEW.conversation_id AND tenant_id=NEW.tenant_id;
IF TG_OP='INSERT' THEN
 SELECT count(*),count(*) FILTER(WHERE visibility_through_seq IS NULL) INTO previous_count,unknown_count
 FROM conversation_assignments WHERE conversation_id=NEW.conversation_id AND tenant_id=NEW.tenant_id;
 IF previous_count=0 THEN
  NEW.visibility_after_seq:=0;
 ELSE
  -- 分配和转接在持有咨询锁后释放当前段，指针明确表示实际前段，事务开始时间不能表示锁的获取顺序。
  SELECT visibility_through_seq INTO previous_end FROM conversation_assignments
  WHERE id=current_assignment AND conversation_id=NEW.conversation_id AND tenant_id=NEW.tenant_id AND released_at IS NOT NULL;
  IF previous_end IS NULL THEN
   -- 超时回队列可先清空指针；冻结序号单调可靠，有任意未知段则从当前水位开始，不猜测顺序。
   IF unknown_count=0 THEN
    SELECT max(visibility_through_seq) INTO previous_end FROM conversation_assignments WHERE conversation_id=NEW.conversation_id AND tenant_id=NEW.tenant_id;
   END IF;
  END IF;
  NEW.visibility_after_seq:=coalesce(previous_end,watermark);
 END IF;
 IF NEW.released_at IS NOT NULL THEN NEW.visibility_through_seq:=watermark; END IF;
ELSIF OLD.released_at IS NULL AND NEW.released_at IS NOT NULL THEN
 NEW.visibility_through_seq:=watermark;
END IF;
RETURN NEW;
END $$;
COMMENT ON FUNCTION record_assignment_message_boundary() IS '按当前接待指针或可靠冻结序号记录真实接待边界，不使用事务开始时间或随机编号推断顺序，未知段保守隐藏';

DO $$ DECLARE tenant uuid; previous_context text:=current_setting('app.tenant_id',true); BEGIN
FOR tenant IN SELECT id FROM tenants LOOP
PERFORM set_config('app.tenant_id',tenant::text,true);
-- 释放水位来自咨询锁内的单调消息序号；只修复已有明确起点，NULL旧边界继续未知，不向任何历史段扩大可见范围。
WITH corrected AS (
 SELECT a.id,(SELECT max(previous.visibility_through_seq) FROM conversation_assignments previous
 WHERE previous.tenant_id=a.tenant_id AND previous.conversation_id=a.conversation_id AND previous.id<>a.id
 AND (previous.visibility_through_seq<=a.visibility_through_seq OR (a.visibility_through_seq IS NULL AND a.id=c.current_assignment_id AND a.released_at IS NULL))) AS minimum_after
 FROM conversation_assignments a JOIN conversations c ON c.tenant_id=a.tenant_id AND c.id=a.conversation_id
 WHERE a.tenant_id=tenant AND a.visibility_after_seq IS NOT NULL
)
UPDATE conversation_assignments a SET visibility_after_seq=greatest(a.visibility_after_seq,corrected.minimum_after)
FROM corrected WHERE a.tenant_id=tenant AND a.id=corrected.id AND corrected.minimum_after>a.visibility_after_seq;
END LOOP;
PERFORM set_config('app.tenant_id',coalesce(previous_context,''),true);
END $$;
