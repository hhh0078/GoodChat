/** 文件作用说明：验证AI装修按提交次数禁用，区分未配置、平台停用、当日用尽和可提交。 */
import assert from "node:assert/strict";
import test from "node:test";
import { aiSubmissionState, type AIConfiguration } from "../src/lib/ai-configuration.ts";
const ready: AIConfiguration = {
  configured: true,
  enabled: true,
  daily_limit: 20,
  used_today: 1,
  remaining_today: 19,
  timezone: "Asia/Shanghai",
};
test("AI配置未读或模型未配置时禁止提交，并保留各自原因", () => {
  assert.equal(aiSubmissionState(null).allowed, false);
  assert.equal(aiSubmissionState(null).reason, "unknown");
  assert.equal(
    aiSubmissionState({ ...ready, configured: false, enabled: false }).reason,
    "unconfigured",
  );
});
test("当日次数为零与已用尽分别展示，剩余正数才可提交", () => {
  assert.equal(
    aiSubmissionState({ ...ready, enabled: false, daily_limit: 0, remaining_today: 0 }).reason,
    "disabled",
  );
  assert.equal(
    aiSubmissionState({ ...ready, used_today: 20, remaining_today: 0 }).reason,
    "exhausted",
  );
  assert.equal(aiSubmissionState({ ...ready, enabled: false }).allowed, false);
  assert.equal(aiSubmissionState(ready).allowed, true);
});
