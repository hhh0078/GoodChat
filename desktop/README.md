<!-- 文件作用说明：说明同一份GoodChat桌面代码的服务器配置、双平台打包、原生消息职责和验证边界。 -->
# GoodChat 桌面端

同一份 Go + React + Ant Design 代码支持 macOS 与 Windows。Wails 固定为 `v3.1.0`；Mac 使用系统 WebKit，Windows 使用共享 WebView2，不随包携带 Chromium 或 Node.js 服务。

## 打包

需要 Go 1.26.5、Node.js 22.19 或兼容版本、Python 3.12+。Mac 构建还需 Xcode Command Line Tools。依赖安装前检查国内镜像：

```bash
npm --prefix frontend config get registry
go env GOPROXY
npm --prefix frontend ci --registry=https://registry.npmmirror.com
```

在仓库根目录执行，`--version` 与 `--server` 会同时注入程序：

```bash
# Mac通用包，包含Apple silicon和Intel架构，必须在Mac构建。
python3 desktop/scripts/build.py --target mac-universal --version 1.0.0 --server https://goodchat.58it.cn

# Windows x64免安装包，可在Mac交叉构建，也可在Windows构建。
python3 desktop/scripts/build.py --target win-amd64 --version 1.0.0 --server https://goodchat.58it.cn
```

还支持 `mac-arm64`、`mac-amd64`、`win-arm64`。前端未变化时可加 `--skip-frontend`。Windows终端可将 `python3` 换成 `python`。版本使用 `主版本.次版本.修订版本` 三段数字，分别写入 Mac `Info.plist`、Windows版本资源和启动界面。默认地址必须是HTTPS根地址，仅本机调试允许HTTP。

产物放在 `desktop/dist/`：Mac 为 `.dmg`，Windows 为解压运行的 `.zip`，每个产物附带版本、默认地址、大小、SHA256和签名状态的 `.json`。

仓库包含GitHub Actions的 **GoodChat desktop packages** 手动构建流程，填写同样的版本与服务器参数，会分别在Mac、Windows运行器构建，产物只上传为任务附件，不发布线上服务。GitHub要求手动流程先存在于默认分支；本次按要求合入 `dev`，未额外修改默认 `main`，因此在合入默认分支前请使用本机脚本。

## 启动和服务器选择

- 每次启动显示服务器选择；首次默认使用打包注入的地址，此后优先使用本机记住的地址。
- 地址可以编辑，也可以点击“恢复安装包默认值”；工具栏和托盘均可重新进入服务器选择。
- 服务器地址只包含协议、域名和可选端口，不支持把 `/login`、`/api` 等页面路径作为服务器地址。
- 配置以临时文件加原子替换保存。普通配置不包含密码或认证Cookie；当前窗口未发送草稿按服务器归档和恢复。
- 每个服务器使用自己的系统凭证记录；发送队列进一步按服务器、账号和租户分区。
- 切换服务器停止旧连接，旧待发消息保留在原分区，不能迁移到新服务器。
- 关闭窗口隐藏到托盘或菜单栏；菜单“退出 GoodChat”才会结束程序。可从托盘重新打开工作台。

## 消息职责

桌面资源随包携带，直接复用网页客服工作台。Vite仅作为桌面构建工具，现有Next.js官网与网页端配置保持不变。

本机资源服务通过WebView的内部资源通道接管 `/api/v1/`，不监听固定TCP端口。Go向用户选定的服务器发出HTTPS请求；认证Cookie不交给页面，也不跟随重定向发送。业务幂等头、附件上传下载和现有服务端鉴权继续有效。

原生Go任务申请一次性WebSocket票据、处理协议Ping/Pong、检测45秒没有应用帧、退避重连，并独立补拉持久事件。收到事件只唤醒对应咨询；每30秒扫描全部可见的排队、待接和接待中咨询作为恢复兜底。服务端签名游标与本机事件在同一SQLite事务提交，游标失效会重新获取授权快照。界面自身完成恢复前不显示连接成功。

普通消息先持久化到SQLite，再发送HTTP请求。只有收到匹配原 `client_message_id` 的服务端保存确认才删除队列；网络错误、408、429和5xx保留原消息待后续重试，确定性的业务拒绝转入人工处理。原生队列不依赖网页 `sessionStorage`，重启后在相同服务器、账号和租户重新认证才会继续处理。

前台复用已有提示音逻辑；窗口失去焦点后，原生模块根据服务端偏好领取提醒。系统通知只在用户授权后发送，不显示聊天正文；提示音使用随包固定WAV并按成员音量缩放，即使关闭系统通知也可播放提示音。通知和音频错误不阻塞消息连接。

## 凭证与数据

沿用现有服务端设备会话、过期和撤销规则，未增加永久令牌或万能码。会话Cookie存入macOS钥匙串或Windows凭据管理器，系统凭证库不可用时明确报错，不降级为明文文件。

本机目录为 `os.UserConfigDir()/GoodChat`，通常在Mac的 `~/Library/Application Support/GoodChat`、Windows的 `%AppData%/GoodChat`。SQLite保存待发消息、最近事件和游标，不保存认证凭证；它不是加密数据库。Mac配置目录按0700、文件按0600创建，Windows还受用户目录ACL保护。完整历史、附件读取和业务权限始终由服务器核验；本地缓存没有离线越权读取入口。

## 检查

```bash
npm --prefix frontend run desktop:typecheck
npm --prefix frontend run typecheck
npm --prefix frontend test
cd desktop
go test -race ./internal/...
go vet ./...
```

`go vet ./...` 和直接 `go build` 之前需先构建桌面前端，使嵌入资源存在。

自动检查包含：配置和地址校验、响应丢失后的原编号重试、重启恢复、服务器与租户分区、Cookie和重定向边界、幂等头透传、跨来源调用拒绝、事件及游标事务回滚、过期游标重建、没有网页时原生WebSocket接收、权限撤销停止重连、PCM音量缩放。测试HTTP服务仅用于验证客户端，不能称为真实业务验收。

## 发布和验证边界

- Mac最低版本按12.0构建，仍需在目标系统与Intel电脑验证。正式公开分发应使用Developer ID签名及公证：通过 `--signing-identity` 和 `--notary-profile` 指定已配置的系统签名身份与钥匙串公证配置。未指定时生成临时签名开发测试包。
- Windows提供x64和ARM64编译入口，至少面向Windows 10/11与WebView2。缺少WebView2的电脑需从微软官方安装；本版本不捆绑运行时、不自动下载第三方运行时。公开分发前还需Windows代码签名。
- 尚未加入自动更新、开机自启或系统服务。这些不影响原生消息模块随程序运行，后续按实际发布需求增加。
- 系统睡眠、断网、程序被结束时不能持续实时收发；恢复后依靠幂等与持久补拉核对。服务器接待意图及在线租约继续遵守现有协议。
- 构建成功和本地协议测试不代表真实双平台业务验收。发布前需要真实账号和访客双向消息、附件、前后台提示、窗口关闭、睡眠唤醒、网络切换、权限撤销、服务器重启及长时间运行验证。

实现与当前验证记录见 [桌面端开发记录](../docs/桌面端开发与验收.md)。
