/** 文件作用说明：验证资源包文件树保留真实相对路径、稳定分组且不展示异常穿越路径。 */
import assert from "node:assert/strict";
import test from "node:test";
import { buildPackageTree } from "../src/lib/package-tree.ts";
test("多目录资源树保留中文文件和嵌套模块的原始路径", () => {
  const nodes = buildPackageTree([
    { path: "index.html" },
    { path: "assets/logo.png" },
    { path: "assets/js/main.mjs" },
    { path: "内容/说明.txt" },
  ]);
  assert.equal(nodes.at(-1)?.key, "file:index.html");
  const assets = nodes.find((node) => node.key === "dir:assets");
  assert.equal(assets?.children?.[0].key, "dir:assets/js");
  assert.equal(assets?.children?.[0].children?.[0].key, "file:assets/js/main.mjs");
  assert.equal(
    nodes.find((node) => node.title === "内容")?.children?.[0].key,
    "file:内容/说明.txt",
  );
});
test("异常路径不会生成可用于源码请求的文件节点", () => {
  assert.deepEqual(
    buildPackageTree([
      { path: "../private" },
      { path: "/absolute" },
      { path: "a\\b" },
      { path: "a//b" },
      { path: "./index.html" },
    ]),
    [],
  );
});
