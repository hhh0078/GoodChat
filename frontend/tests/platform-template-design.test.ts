/** 文件作用说明：验证平台包上下架保留完整资源，内联与组件模板仍按各自表单保存。 */
import assert from "node:assert/strict";
import test from "node:test";
import { platformTemplateDesign } from "../src/features/platform/template-design.ts";

test("平台静态包改名或上下架时忽略旧内联字段和表单模式，只保留独立包引用", () => {
  const id = "10000000-0000-4000-8000-000000000001";
  for (const mode of ["code", "schema", undefined])
    assert.deepEqual(
      platformTemplateDesign(
        mode,
        { html: "过期代码", design_json: "损坏JSON" },
        { platform_package_id: id },
      ),
      { platform_package_id: id },
    );
});

test("内联代码与组件模板保存原始内容，损坏JSON提供明确错误", () => {
  assert.deepEqual(platformTemplateDesign("code", { html: "<main>咨询</main>" }), {
    html: "<main>咨询</main>",
    css: "",
    javascript: "",
  });
  assert.deepEqual(platformTemplateDesign("schema", { design_json: '{"schema_version":1}' }), {
    schema_version: 1,
  });
  assert.throws(() => platformTemplateDesign("schema", { design_json: "broken" }), /合法 JSON/);
});
