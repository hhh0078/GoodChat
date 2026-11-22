---
name: goodchat-template
description: 修改 GoodChat 静态聊天模板并交付带版本号的可导入 ZIP。
---
<!-- 文件作用说明：约束外部 AI 使用官方聊天能力并保留可回滚的版本包。 -->
# GoodChat 模板编辑规范

## 先阅读，再修改
解压整个 ZIP，阅读 template.json 与 README.md。在现有模板上修改 HTML、CSS 和本地界面逻辑。无需构建、无需依赖；文件引用必须相对且真实存在。不要添加远程资源、CDN、iframe、内联脚本、SVG、表单标签或网络请求。

## 不得重写的能力
以下文件必须逐字节保留，不得修改版本、摘要、方法或重新生成：
- static/goodchat/sdk.js
- static/goodchat/chat-ui.js
- goodchat-contract.json

保留唯一的官方节点：gc-status、gc-messages、gc-text（textarea）、gc-send（button）、gc-file（input type=file）、gc-add（button）、gc-attachment-menu、gc-typing、gc-error、gc-reply-bar、gc-reply-text、gc-reply-cancel（button）。保留全部 ID、无障碍标签、输入长度和脚本顺序。gc-file、附件菜单、引用栏初始 hidden。所有 button 必须 type="button" 并有且仅有一种声明动作，官方 ID 的按钮不要额外添加动作。不要隐藏聊天消息、输入框、发送或附件入口。

官方 chat-ui.js 自动处理普通文字、图片、文件、音视频、选项、表单（含必填验证和提交结果）、历史、已读、输入状态、引用和撤回。修改布局与样式即可。不能伪造消息、连接成功、提交成功或 AI 回复。

业务动作只允许官方 SDK；优先保持现有绑定，不能对同一按钮重复绑定：
- 发送文字：GoodChat.sendText(text, client_message_id?, reply_to_id?)
- 上传：GoodChat.upload(file, name?)；下载：GoodChat.download(attachment_id)
- 引用由发送的 reply_to_id 实现；撤回：GoodChat.withdrawMessage(message_id)
- 表单提交：GoodChat.submitForm(message_id, values, client_message_id?)
- 历史：GoodChat.loadOlder()；已读：GoodChat.read(last_read_seq)
- 状态：GoodChat.ready()、GoodChat.getState()、GoodChat.subscribe(handler)
- 输入中：GoodChat.typing(boolean)
- 留言：GoodChat.openLeaveMessage()；评价：GoodChat.openEvaluation()

请勿手写 API、WebSocket、身份、凭证、本地会话存储、AI 调用或发消息逻辑。额外 JS 只处理本地交互，放 static/custom.js 并按需引入。快捷入口发文字用 data-gc-send="文字"；留言/评价按钮用 data-gc-action="leave-message" / "evaluation"。本地交互按钮可用 data-gc-ui="唯一动作名" 配合外部自定义 JS；不要加入没响应的按钮。官方业务表单由聊天核心渲染，不能使用 HTML form 标签替代。

## 布局验收
消息区独立滚动，输入区始终可见；输入、附件、发送垂直居中。手机宽 320/390、桌面 1440、悬浮窗 360×600 均无横向溢出。不重复展示接待状态栏。保留 :focus-visible 与 reduced-motion 支持。只用单色或自然色系，不用蓝紫渐变。

## 每次必须交付 ZIP 与版本
每一轮修改都递增 template.json.version，使用三段数字版本（1.0.0 → 1.0.1）。不改变 schema_version、sdk_version 或固定合约。ZIP 文件名为 goodchat-template-1.0.1.zip，完整保留原始版本以便回退。
根目录直接放 index.html、template.json、SKILL.md、README.md、PROMPT.md、goodchat-contract.json 和 static/。不得套一层目录，不含 node_modules、构建缓存、.DS_Store 或其他隐藏文件。不能只交付截图、单个文件或代码片段。打包前核对固定核心文件 SHA256 与输入 ZIP 一致，说明已测试与尚未验证的范围。上传平台后仍须实际导入、预览并使用聊天功能；本地预览不代表真实业务验收。
