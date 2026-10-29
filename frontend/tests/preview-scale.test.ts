/** 文件作用说明：验证完整设备预览同时适配狭窄与矮容器，不重复扣内边距或强制最小缩放造成溢出。 */
import assert from "node:assert/strict";
import test from "node:test";
import { previewScale } from "../src/features/assistant/preview-scale.ts";

test("宽容器但高度有限时完整显示手机，页头与输入框无需外层滚动", () => {
  const scale = previewScale(1000, 500, 390, 780);
  assert.equal(scale, 500 / 780);
  assert.ok(390 * scale <= 1000);
  assert.ok(Math.abs(780 * scale - 500) < 0.001);
});

test("窄容器按宽度缩放桌面预览，内容区域不再次扣除padding", () => {
  const scale = previewScale(324, 640, 1080, 760);
  assert.equal(scale, 324 / 1080);
  assert.equal(1080 * scale, 324);
  assert.ok(760 * scale <= 640);
  assert.equal(previewScale(1400, 1000, 1080, 760), 1);
});

test("隐藏或尚未布局的容器不显示溢出画面，极小容器也不设置强制缩放下限", () => {
  assert.equal(previewScale(0, 500, 390, 780), 0);
  assert.equal(previewScale(390, 0, 390, 780), 0);
  assert.equal(previewScale(Number.NaN, 500, 390, 780), 0);
  assert.equal(previewScale(390, 500, 0, 780), 0);
  assert.equal(previewScale(20, 20, 390, 780), 20 / 780);
});
