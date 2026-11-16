/** 文件作用说明：校验九语目录、前后端同源性、变量和源码消息覆盖，禁止缺失翻译静默进入发布。 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";
import { createTranslator } from "next-intl";
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const locales = ["zh-CN", "en", "zh-TW", "ja", "ko", "es", "pt-BR", "fr", "de"];
/** read 读取受版本控制的 JSON 语言目录。 */
/** read 读取项目内受控 JSON 目录。 */
const read = (name) => JSON.parse(fs.readFileSync(name, "utf8"));
const backend = Object.fromEntries(
  locales.map((locale) => [
    locale,
    read(path.join(root, "backend/internal/localization/catalogs", `${locale}.json`)),
  ]),
);
const baseline = backend["zh-CN"];
const failures = [];

/** key 使用与 Go 和浏览器一致的 UTF-8 FNV 消息标识。 */
function key(source) {
  let value = 2166136261;
  for (const byte of Buffer.from(source)) value = Math.imul(value ^ byte, 16777619) >>> 0;
  return `m${value.toString(16).padStart(8, "0")}`;
}

/** icu 对前端字面量进行相同的保留字符转义，仅放行命名变量。 */
function icu(source) {
  return source
    .replaceAll("'", "''")
    .split(/(\{p\d+\})/)
    .map((part) =>
      /^\{p\d+\}$/.test(part)
        ? part
        : part
            .replaceAll("{", "'{'")
            .replaceAll("}", "'}'")
            .replaceAll("<", "'<'")
            .replaceAll(">", "'>'"),
    )
    .join("");
}

/** variables 读取需要在各语言中严格保留的命名变量及 printf 格式参数。 */
function variables(source) {
  return source.match(/\{p\d+\}|%(?:\[\d+\])?[-+#0 ]*\d*(?:\.\d+)?[sdvTf]/g) || [];
}

for (const locale of locales) {
  const browser = read(path.join(root, "frontend/src/i18n/messages", `${locale}.json`));
  if (Object.keys(backend[locale]).length !== Object.keys(baseline).length)
    failures.push(`${locale}: backend catalog size`);
  if (Object.keys(browser).length !== Object.keys(baseline).length)
    failures.push(`${locale}: frontend catalog size`);
  let formattingKey = "";
  const format = createTranslator({
    locale,
    messages: browser,
    onError: (problem) => failures.push(`${locale}: ${formattingKey}: ${problem.message}`),
  });
  for (const [id, source] of Object.entries(baseline)) {
    const text = backend[locale][id];
    if (typeof text !== "string") {
      failures.push(`${locale}: missing ${id}`);
      continue;
    }
    if (key(source) !== id || browser[id] !== icu(text))
      failures.push(`${locale}: source mismatch ${id}`);
    if (JSON.stringify(variables(source).sort()) !== JSON.stringify(variables(text).sort()))
      failures.push(`${locale}: variables ${id}`);
    const params = Object.fromEntries(
      [...source.matchAll(/\{(p\d+)\}/g)].map((match) => [match[1], "sample"]),
    );
    formattingKey = id;
    if (browser[id]) format(id, params);
  }
}

/** files 只遍历实际源码，忽略依赖、生成目录与测试夹具。 */
function files(directory) {
  return fs
    .readdirSync(directory, { withFileTypes: true })
    .flatMap((entry) =>
      entry.isDirectory()
        ? files(path.join(directory, entry.name))
        : [path.join(directory, entry.name)],
    );
}
for (const file of [
  ...files(path.join(root, "frontend/src")),
  ...files(path.join(root, "desktop/frontend/src")),
].filter((file) => /\.tsx?$/.test(file))) {
  const source = ts.createSourceFile(
    file,
    fs.readFileSync(file, "utf8"),
    ts.ScriptTarget.Latest,
    true,
  );
  /** visit 校验明确标记为系统文案的调用，避免误扫用户字段和代码示例。 */
  function visit(node) {
    if (
      ts.isCallExpression(node) &&
      ts.isIdentifier(node.expression) &&
      ["tr", "translate"].includes(node.expression.text)
    ) {
      const argument = node.arguments[0];
      if (
        argument &&
        ts.isStringLiteralLike(argument) &&
        /\p{Script=Han}/u.test(argument.text) &&
        !Object.hasOwn(baseline, key(argument.text))
      )
        failures.push(`${path.relative(root, file)}: missing ${argument.text}`);
    }
    ts.forEachChild(node, visit);
  }
  visit(source);
}
if (failures.length) {
  process.stderr.write(failures.join("\n") + "\n");
  process.exitCode = 1;
} else
  process.stdout.write(
    `九种语言共 ${Object.keys(baseline).length} 条文案，目录、变量、ICU 格式和源码覆盖检查通过。\n`,
  );
