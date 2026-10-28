/** 文件作用说明：验证统计曲线空态、真实端点与非法数值边界，防止平滑插值越过非负绘图区。 */
import assert from "node:assert/strict";
import test from "node:test";
import { smoothTrendPath } from "../src/lib/trend-curve.ts";

test("统计曲线保留端点并把所有控制点限制在真实样本区间", () => {
  assert.equal(smoothTrendPath([], 100, 100, 1), "");
  assert.equal(
    smoothTrendPath([0, 10, 0], 100, 100, 10),
    "M 0 100 C 25 100, 25 0, 50 0 C 75 0, 75 100, 100 100",
  );
  assert.equal(smoothTrendPath([NaN, -4, Infinity], 100, 100, 10).includes("NaN"), false);
  assert.equal(smoothTrendPath([5], 100, 100, 10), "M 50 50");
});
