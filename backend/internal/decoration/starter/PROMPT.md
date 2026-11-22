请解压我上传的 GoodChat 在线聊天模板 ZIP，先完整阅读根目录 SKILL.md、README.md 和 template.json。这个项目可以直接导入 GoodChat，无需安装依赖或编译。

请在当前模板上实现我接下来描述的视觉效果与本地交互，兼顾手机、桌面和悬浮窗。保留所有官方聊天节点、消息类型和可访问性。发送消息、附件、引用、撤回、选项、表单、历史、已读、留言和评价必须沿用包内官方 SDK / chat-ui.js，调用对应已封装的方法，不得自己实现业务接口或模拟业务成功。禁止修改 static/goodchat/sdk.js、static/goodchat/chat-ui.js 和 goodchat-contract.json。

完成后检查引用、按钮、滚动和小屏输入区，递增 template.json 的 version（例如 1.0.0 → 1.0.1），将完整项目重新打包为 goodchat-template-<版本号>.zip 并提供下载。ZIP 根目录必须直接包含 index.html、template.json、SKILL.md 和 static；不要包含项目外层目录、依赖或系统隐藏文件。保留原版本，不要只输出代码片段或截图。每次修改都交付新的版本 ZIP。

我想要的效果是：
