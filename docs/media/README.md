<!-- 文件作用说明：说明公开文档图标和真实截图的来源、压缩更新方法、隐私检查与中英文共用规则。 -->
# Documentation media · 文档素材

[English README](../../README.md) · [中文 README](../../README.zh-CN.md) · [Template manifest](templates/manifest.json) · [Feature screenshot manifest](screenshots/manifest.json)

## English

- `goodchat-icon.png` reuses the application's existing Ant Design `MessageFilled` vector and current `--brand: #1455ff` color. It is a transparent 96 × 96 PNG, not a new logo design. The source is `frontend/node_modules/@ant-design/icons-svg/lib/asn/MessageFilled.js`; the related application component is `frontend/src/components/brand.tsx`.
- `screenshots/` contains real application screenshots and contact sheets made only from real captures. `templates/` contains the seven desktop/mobile template pairs. These show Chinese UI and demonstration conversations, not generated design mockups.
- The feature set contains 19 regular captures preserved at their source dimensions (including cropped dialogs), plus two native captures. Both native sources are 3024 × 1654; exactly 112 px were removed from the top to exclude browser debugging chrome. The remaining 3024 × 1542 pixels were proportionally reduced to 1600 × 816 for `template-library.webp`, and 1512 × 771 for `workspace.webp`. No capture was enlarged. The workspace uses `workspace-native.jpg` to preserve the rendered customer sidebar; the incomplete-layer `workspace.jpg` capture is excluded. The AI image uses `ai-assistant.jpg`; the unrelated-tab native capture is excluded.
- Screenshots contain local acceptance/demo data. `ai-assistant.webp` shows a new conversation's empty state, not successful generation. `template-editor.webp` shows the actual rendered finance preview with a selected text element. Monitoring's manually entered baseline of **100** and its derived estimate are demonstration data; they are not load-test results or performance acceptance evidence. Team/member and response statistics are acceptance samples too.
- Capture with an authorized demonstration workspace. Before publishing, inspect the full image for personal information, visitor identifiers, access-link keys, preview tokens, API keys, email addresses, phone numbers, private messages, and browser debugging chrome. Prefer recapturing with clean demo data; crop or redact any remaining private details before compression. Author contact details intentionally published in the project README are separate from screenshot data.
- Share the same image files across English and Chinese docs. Retain readable text and aspect ratios; do not enlarge a capture. Template WebP files use quality 84 and keep their original dimensions. Each of the 14 individual template files is under 140 KiB.
- To update a screenshot from the repository root, use the existing Sharp installation as below. Inspect the output at native size, update affected contact sheets, and refresh source/output dimensions, byte counts, SHA-256 values, and compression ratios in the relevant asset manifest. Never commit private evidence JSON, authentication files, or signed preview URLs.

```sh
# 文件作用说明：使用既有 Sharp 将已检查的真实截图压缩为 WebP，不放大原图。
node -e 'require("./frontend/node_modules/sharp")(process.argv[1]).webp({quality:84,effort:6}).toFile(process.argv[2])' /absolute/path/to/clean-capture.png docs/media/screenshots/example.webp
```

The seven-mobile contact sheet uses four images on the first row and three centered on the second, each 349 px wide with the original aspect ratio. The finance desktop/mobile pair uses both captures at their native dimensions. The source template captures are in `docs/designs/2026-10-30-visitor-concepts/implemented/`.

## 简体中文

- `goodchat-icon.png` 直接复用应用现有 Ant Design `MessageFilled` 矢量和当前品牌色 `--brand: #1455ff`，输出透明的 96 × 96 PNG，没有另行设计新标识。源码位于上述图标依赖，应用品牌组件是 `frontend/src/components/brand.tsx`。
- `screenshots/` 存放真实应用截图及仅由实拍截图组成的拼图；`templates/` 存放七行业电脑、手机共十四张截图。图片使用中文界面和模拟咨询，不能用生成的设计概念图冒充功能实拍。
- 功能图包含十九张保持来源尺寸的常规截图（部分来源已裁为对话框），以及两张原生截图。两张原生来源均为 3024 × 1654，顶部精确裁去 112 像素浏览器调试栏，再将剩余 3024 × 1542 分别等比缩小为模板库 `template-library.webp` 的 1600 × 816，以及工作台 `workspace.webp` 的 1512 × 771，没有放大。工作台使用 `workspace-native.jpg` 保留实际显示的完整客户侧栏，不使用丢失图层的 `workspace.jpg`。AI 助手使用 `ai-assistant.jpg`，误抓其他标签的原生截图不使用。
- 图中数据来自本机验收或演示。`ai-assistant.webp` 是新对话空状态，不表示生成成功；`template-editor.webp` 展示实际金融模板预览及已选中文字。监控中手工录入的基线 **100** 及其推算人数仅作演示，不能当作压测或性能验收。成员及回复统计也属于验收样本。
- 使用已授权的演示租户截图。公开前逐张检查个人信息、访客标识、入口密钥、预览令牌、API 密钥、邮箱、电话、私密对话和浏览器调试栏。优先用干净的演示数据重拍；残留隐私先裁切或脱敏，再压缩。项目 README 中明确公开的作者联系方式与截图业务数据分开处理。
- 中英文共用同一组素材。保持文字清楚、原始比例，不放大截图；模板 WebP 使用质量 84，保留原尺寸，十四张独立图片均小于 140 KiB。
- 更新时，在仓库根目录使用上面的既有 Sharp 命令。按原尺寸目视检查输出，同步更新拼图及清单中的来源、原始/输出尺寸、字节数、SHA-256 和压缩比。私密验收 JSON、登录文件和带签名的预览链接不进入提交。

七手机拼图为首行四张、次行三张居中，每张宽 349 像素、等比缩小；金融电脑与手机对照图保留原始尺寸。来源目录为 `docs/designs/2026-10-30-visitor-concepts/implemented/`。

## Existing icon attribution · 既有图标许可

`MessageFilled` is part of Ant Design Icons, already used by GoodChat. The icon's MIT notice is reproduced below; this notice applies to that icon and does not set the license for the entire GoodChat project.

```text
MIT LICENSE

Copyright (c) 2018-present Ant UED, https://xtech.antfin.com/

Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
"Software"), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
```
