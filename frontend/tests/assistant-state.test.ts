/** 文件作用说明：验证 AI 助手入口权限、图片上传边界和失败任务的真实状态展示。 */
import test from "node:test";
import assert from "node:assert/strict";
import {
  activeAssistantJob,
  assistantAssetProblem,
  assistantJobLabel,
  canUseAssistant,
} from "../src/features/assistant/state.ts";

test("AI 助手只对当前租户管理员和平台管理员开放", () => {
  assert.equal(canUseAssistant(false, "owner"), true);
  assert.equal(canUseAssistant(false, "admin"), true);
  assert.equal(canUseAssistant(true, "agent"), true);
  assert.equal(canUseAssistant(false, "agent"), false);
  assert.equal(canUseAssistant(false, "member"), false);
  assert.equal(canUseAssistant(false), false);
  assert.equal(canUseAssistant(false, "superadmin"), false);
});

test("参考图片在请求前拒绝空文件、非图片、超限和第五张", () => {
  assert.equal(assistantAssetProblem({ type: "image/png", size: 1024 }, 0), "");
  assert.equal(assistantAssetProblem({ type: "image/webp", size: 10 * 1024 * 1024 }, 3), "");
  assert.match(assistantAssetProblem({ type: "image/svg+xml", size: 1024 }, 0), /PNG/);
  assert.match(assistantAssetProblem({ type: "image/png", size: 0 }, 0), /不能为空/);
  assert.match(
    assistantAssetProblem({ type: "image/png", size: 10 * 1024 * 1024 + 1 }, 0),
    /10MiB/,
  );
  assert.match(assistantAssetProblem({ type: "image/jpeg", size: 1024 }, 4), /4 张/);
});

test("任务失败与取消不能呈现完成，阶段进度由服务端实际字段决定", () => {
  for (const status of ["queued", "running", "validating", "unknown"])
    assert.equal(activeAssistantJob({ id: "job", status }), true);
  for (const status of ["succeeded", "failed", "cancelled"])
    assert.equal(activeAssistantJob({ id: "job", status }), false);
  assert.equal(activeAssistantJob(null), false);
  assert.equal(
    assistantJobLabel({ id: "job", status: "running", phase: "images" }),
    "正在生成与处理图片",
  );
  assert.equal(
    assistantJobLabel({ id: "job", status: "running", phase: "package" }),
    "校验并打包模板",
  );
  assert.equal(assistantJobLabel({ id: "job", status: "failed", phase: "package" }), "生成未完成");
  assert.equal(assistantJobLabel({ id: "job", status: "succeeded" }), "AI 已回复");
  assert.equal(
    assistantJobLabel({
      id: "job",
      status: "succeeded",
      design: { package_id: "package" },
      applied: false,
    }),
    "候选模板已保留",
  );
});
