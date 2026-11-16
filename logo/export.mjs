/** 文件作用说明：从矢量母版统一导出GoodChat各尺寸、形状和格式，并同步网站及未来桌面打包所用图标。 */
import assert from "node:assert/strict";
import { readFile, writeFile, mkdir, copyFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { spawnSync } from "node:child_process";

const directory = path.dirname(fileURLToPath(import.meta.url));
const root = path.dirname(directory);
const require = createRequire(path.join(root, "frontend/package.json"));
const sharp = require("sharp");
const blue = "#165DFF";
const ink = "#111A2C";
const source = await readFile(
  path.join(directory, "source/goodchat-mark.svg"),
  "utf8",
);
const typeSource = await readFile(
  path.join(directory, "source/goodchat-type.svg"),
  "utf8",
);
const shape = source.match(/<path\b[^>]*\/>/)[0];
const typeShape = typeSource.match(/<path\b[^>]*\/>/)[0];
const [, , , typeWidth, typeHeight] = typeSource
  .match(/viewBox="([\d.]+) ([\d.]+) ([\d.]+) ([\d.]+)"/)
  .map(Number);
const files = [];

/** save 只在Logo目录内生成交付文件，并登记可复核的文件清单。 */
async function save(name, content) {
  const target = path.join(directory, name);
  await mkdir(path.dirname(target), { recursive: true });
  await writeFile(target, content);
  files.push(name);
  return target;
}

/** svg 生成带中文作用说明的独立SVG，所有字标均已转曲。 */
function svg(width, height, body) {
  return `<!-- 文件作用说明：GoodChat官方品牌素材，由logo/export.mjs从矢量母版生成。 -->\n<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}"><title>GoodChat</title>${body}</svg>\n`;
}

/** mark 按同一母版放置标志，透明区域不使用白色图层遮挡。 */
function mark(x, y, size, color) {
  return `<svg x="${x}" y="${y}" width="${size}" height="${size}" viewBox="104 132 1038 1038">${shape.replace(blue, color)}</svg>`;
}

/** wordmark 用已转曲字形生成横版或上下组合，避免字体缺失导致版式漂移。 */
function wordmark(color, textColor, stacked = false) {
  const textWidth = stacked ? 380 : (typeWidth * 72) / typeHeight;
  const scale = textWidth / typeWidth;
  const width = stacked ? 512 : Math.ceil(180 + textWidth);
  const height = stacked ? 512 : 160;
  const x = stacked ? (width - textWidth) / 2 : 162;
  const y = stacked ? 390 : (height - typeHeight * scale) / 2;
  return svg(
    width,
    height,
    mark(stacked ? 80 : 0, stacked ? 12 : 8, stacked ? 352 : 144, color) +
      `<g transform="translate(${x} ${y}) scale(${scale})">${typeShape.replace("currentColor", textColor)}</g>`,
  );
}

/** png 通过现有Sharp高分辨率栅格化，不添加新的图像处理依赖。 */
async function png(name, content, width, height = width) {
  const buffer = await sharp(Buffer.from(content), { density: 300 })
    .resize(width, height)
    .png({ compressionLevel: 9 })
    .toBuffer();
  const meta = await sharp(buffer).metadata();
  assert.equal(meta.width, width);
  assert.equal(meta.height, height);
  return save(name, buffer);
}

const variants = {
  "goodchat-mark": svg(1024, 1024, mark(0, 0, 1024, blue)),
  "goodchat-mark-white": svg(1024, 1024, mark(0, 0, 1024, "#FFFFFF")),
  "goodchat-mark-black": svg(1024, 1024, mark(0, 0, 1024, "#111111")),
  "goodchat-square": svg(
    1024,
    1024,
    `<path fill="${blue}" d="M0 0H1024V1024H0Z"/>` +
      mark(96, 96, 832, "#FFFFFF"),
  ),
  "goodchat-rounded": svg(
    1024,
    1024,
    `<rect width="1024" height="1024" rx="224" fill="${blue}"/>` +
      mark(96, 96, 832, "#FFFFFF"),
  ),
  "goodchat-circle": svg(
    1024,
    1024,
    `<circle cx="512" cy="512" r="512" fill="${blue}"/>` +
      mark(156, 156, 712, "#FFFFFF"),
  ),
  "goodchat-rounded-light": svg(
    1024,
    1024,
    `<rect width="1024" height="1024" rx="224" fill="#FFFFFF"/>` +
      mark(96, 96, 832, blue),
  ),
  "goodchat-rounded-dark": svg(
    1024,
    1024,
    `<rect width="1024" height="1024" rx="224" fill="${ink}"/>` +
      mark(96, 96, 832, "#FFFFFF"),
  ),
  "goodchat-maskable": svg(
    1024,
    1024,
    `<path fill="${blue}" d="M0 0H1024V1024H0Z"/>` +
      mark(180, 180, 664, "#FFFFFF"),
  ),
  "goodchat-horizontal": wordmark(blue, ink),
  "goodchat-horizontal-white": wordmark("#FFFFFF", "#FFFFFF"),
  "goodchat-stacked": wordmark(blue, ink, true),
  "goodchat-stacked-white": wordmark("#FFFFFF", "#FFFFFF", true),
};
for (const [name, content] of Object.entries(variants)) {
  await save(`svg/${name}.svg`, content);
  const horizontal = name.includes("horizontal");
  const sizes = horizontal
    ? [32, 48, 64, 128, 256]
    : name.includes("stacked")
      ? [128, 256, 512, 1024]
      : [16, 24, 32, 48, 64, 96, 128, 180, 192, 256, 512, 1024];
  const ratio = horizontal
    ? Number(content.match(/viewBox="0 0 ([\d.]+) 160"/)[1]) / 160
    : 1;
  for (const size of sizes)
    await png(
      `png/${name}/${name}-${size}.png`,
      content,
      Math.round(size * ratio),
      size,
    );
}
await png(
  "png/goodchat-mark/goodchat-mark-2048.png",
  variants["goodchat-mark"],
  2048,
);
for (const size of [128, 256, 512]) {
  await save(
    `webp/goodchat-mark-${size}.webp`,
    await sharp(Buffer.from(variants["goodchat-mark"]), { density: 300 })
      .resize(size, size)
      .webp({ lossless: true })
      .toBuffer(),
  );
}

/** ico 将常用尺寸PNG装入标准ICO目录，保留透明通道并避免额外ICO依赖。 */
async function ico() {
  const sizes = [16, 24, 32, 48, 64, 128, 256];
  const images = await Promise.all(
    sizes.map((size) =>
      readFile(
        path.join(
          directory,
          `png/goodchat-rounded/goodchat-rounded-${size}.png`,
        ),
      ),
    ),
  );
  const header = Buffer.alloc(6 + sizes.length * 16);
  header.writeUInt16LE(1, 2);
  header.writeUInt16LE(sizes.length, 4);
  let offset = header.length;
  sizes.forEach((size, index) => {
    const entry = 6 + index * 16;
    header[entry] = header[entry + 1] = size === 256 ? 0 : size;
    header.writeUInt16LE(1, entry + 4);
    header.writeUInt16LE(32, entry + 6);
    header.writeUInt32LE(images[index].length, entry + 8);
    header.writeUInt32LE(offset, entry + 12);
    offset += images[index].length;
  });
  await save("icons/GoodChat.ico", Buffer.concat([header, ...images]));
  assert.equal(header.readUInt16LE(4), 7);
}
await ico();

const iconset = path.join(directory, "icons/GoodChat.iconset");
await mkdir(iconset, { recursive: true });
for (const size of [16, 32, 128, 256, 512]) {
  for (const density of [1, 2]) {
    const pixels = size * density;
    const name = `icons/GoodChat.iconset/icon_${size}x${size}${density === 2 ? "@2x" : ""}.png`;
    await copyFile(
      path.join(
        directory,
        `png/goodchat-rounded/goodchat-rounded-${pixels}.png`,
      ),
      path.join(directory, name),
    );
    files.push(name);
  }
}
if (process.platform === "darwin") {
  const result = spawnSync(
    "iconutil",
    ["-c", "icns", iconset, "-o", path.join(directory, "icons/GoodChat.icns")],
    { encoding: "utf8" },
  );
  assert.equal(result.status, 0, result.stderr);
  files.push("icons/GoodChat.icns");
}

const horizontalWidth = Number(
  variants["goodchat-horizontal"].match(/viewBox="0 0 ([\d.]+) 160"/)[1],
);
const shareScale = 800 / horizontalWidth;
const social = svg(
  1200,
  630,
  `<rect width="1200" height="630" fill="#FFFFFF"/><g transform="translate(200 ${(630 - 160 * shareScale) / 2}) scale(${shareScale})">${variants["goodchat-horizontal"].replace(/<!--[\s\S]*?-->/, "").replace("<svg xmlns=", '<svg x="0" y="0" xmlns=')}</g>`,
);
await png("social/goodchat-share-1200x630.png", social, 1200, 630);

const web = path.join(root, "frontend/public/logo");
await mkdir(web, { recursive: true });
for (const name of [
  "goodchat-mark",
  "goodchat-mark-white",
  "goodchat-rounded",
  "goodchat-horizontal",
])
  await copyFile(
    path.join(directory, `svg/${name}.svg`),
    path.join(web, `${name}.svg`),
  );
for (const [sourceName, destination] of [
  ["png/goodchat-rounded/goodchat-rounded-32.png", "favicon-32.png"],
  ["png/goodchat-rounded/goodchat-rounded-48.png", "favicon-48.png"],
  ["png/goodchat-square/goodchat-square-180.png", "apple-touch-icon.png"],
  ["png/goodchat-square/goodchat-square-192.png", "icon-192.png"],
  ["png/goodchat-square/goodchat-square-512.png", "icon-512.png"],
  ["png/goodchat-maskable/goodchat-maskable-512.png", "icon-maskable-512.png"],
  ["social/goodchat-share-1200x630.png", "goodchat-share.png"],
])
  await copyFile(path.join(directory, sourceName), path.join(web, destination));
await copyFile(
  path.join(directory, "svg/goodchat-rounded.svg"),
  path.join(root, "frontend/public/favicon.svg"),
);
await copyFile(
  path.join(directory, "icons/GoodChat.ico"),
  path.join(root, "frontend/public/favicon.ico"),
);
await copyFile(
  path.join(directory, "png/goodchat-rounded/goodchat-rounded-1024.png"),
  path.join(root, "desktop/icon.png"),
);
await copyFile(
  path.join(directory, "png/goodchat-rounded/goodchat-rounded-512.png"),
  path.join(root, "docs/media/goodchat-icon.png"),
);

const catalog = Object.entries(variants)
  .map(
    ([name]) =>
      `<figure class="${name.includes("white") ? "inverse" : ""}"><img src="svg/${name}.svg" alt="${name}"><figcaption>${name}</figcaption></figure>`,
  )
  .join("");
await save(
  "preview.html",
  `<!-- 文件作用说明：离线浏览Logo交付版本，深色底仅用于看清反白图形。 --><!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>GoodChat Logo素材</title><style>body{font:16px system-ui;margin:40px;background:#f5f7fa;color:${ink}}main{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:20px}figure{margin:0;background:white;border-radius:16px;padding:24px;display:grid;place-items:center;gap:20px}img{width:100%;height:160px;object-fit:contain}figcaption{font-size:12px}.inverse{background:${ink};color:white}</style><h1>GoodChat Logo</h1><p>钴蓝 #165DFF · 所有SVG及字标均已转曲</p><main>${catalog}</main></html>`,
);
await save(
  "manifest.json",
  JSON.stringify(
    {
      description: "GoodChat标志各形状与尺寸交付清单",
      color: blue,
      source: "source/goodchat-mark.svg",
      generated_by: "node logo/export.mjs",
      files,
    },
    null,
    2,
  ) + "\n",
);
console.log(
  `已生成${files.length}个Logo交付文件，网站资源及桌面源图标已同步。`,
);
