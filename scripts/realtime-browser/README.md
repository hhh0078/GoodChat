<!-- 文件作用说明：说明本机正常Chrome实时生命周期验收工具的明确作用、演示夹具边界、操作及证据限制。 -->

# 原生 Chrome 实时恢复验收

此工具使用 Node.js 内置 WebSocket 直接控制独立正常 Chrome。不会通过 Playwright 连接，也不会设置 focus/visibility 模拟或覆写浏览器 getter。需要 Node.js 22+ 和已安装的 Chrome；macOS 默认使用系统 Google Chrome，可用 `--chrome` 指定已安装浏览器的可执行文件。

先执行约三秒的空白页自检，不需要登录、不会访问应用：

```sh
node scripts/realtime-browser/lifecycle.mjs \
  --probe \
  --output .local/evidence/realtime-resilience/native-probe
```

通过条件为原生 `document.visibilityState === "hidden"`、`document.hidden === true`，冻结约 2.2 秒后 100ms 计时器停止，且实际收到 `freeze` / `resume`。若环境不能满足，工具明确失败，不把名义 CDP frozen 当作真实冻结。

正式验收必须显式提供私有 fixture 和输出目录：

```sh
node scripts/realtime-browser/lifecycle.mjs \
  --fixture .local/evidence/realtime-resilience/lifecycle/fixture.json \
  --output .local/evidence/realtime-resilience/native-browser
```

fixture 与 `scripts/realtime-load.sh --mode fixture` / `scripts/realtime-load` 的 ops 夹具格式一致：需要 `base_url`、`name`、`tenant_id`、`accounts` 中的 owner（`member_id`、Cookie）及 `links.load` 中的本租户组/池。生成生命周期夹具时使用独立演示租户、足够坐席容量、`--visitors 0`，让 owner 保持上班可接待。具体 fixture 创建参数见压测工具帮助。本工具不会改变成员接待意图；初始 owner 未在线或无容量时，正式领取会明确失败。

工具只允许 `http://127.0.0.1:<port>`，拒绝外部地址及重定向。创建任何入口前，会通过真实 `auth/me` 核对 owner、接待资格、租户编号与同名 `演示数据` 租户；不能只把普通租户的 fixture 名字改成演示名字绕过。不要把 Cookie 或 fixture 提交进 Git。示例 `.local/` 已由仓库忽略，输出目录及 Chrome 临时 profile 使用私有权限。

正式过程与已验证脚本一致：

1. 新建两个明确标记的演示入口与独立访客上下文，不使用已有访客身份或会话。
2. 原生最小化各自 Chrome 窗口，确认真正 hidden 后同时起表。
3. 第一页冻结至少 71 秒；第二页保持自然后台至少 305 秒，不冻结它。
4. 在冻结/后台期间通过正式 HTTP 各发送 55 条，合计 110 条 owner 消息；与其他测试共用 owner 时需协调每分钟限额。
5. 第一页原生计时器不得继续运行；恢复后两页分别按每个服务端消息 id、seq、client_message_id 和 DOM 行逐一对账，不能只对比总数。每个期望消息只能出现一行。
6. 写入 `native-lifecycle-result.json`、过程进度与两张恢复截图。只关闭工具自己创建的 Chrome，不操作其他 Chrome 或 Playwright 会话。

演示咨询和入口保留供审计，结束后使用 ops 夹具的 cleanup 模式统一回收。工具不会自动删除业务数据或清理其他测试入口。Chrome 启动日志包含本机调试能力地址，仅保存在指定私有目录，不能对外分享原文件。

后台测试保留真实计时，约五分钟；正式执行期间不要系统睡眠或重启服务。不要把短 probe、时钟前移或 getter 模拟当作长后台验收。默认 55 条在后台开始后发送；若要单独证明五分钟之后的新消息也能到达，应在达到五分钟后追加一条并单独对账，不要把“早先消息仍保留”描述成该项通过。

服务端已落库消息可恢复。彻底关闭标签后 `sessionStorage` 的未提交/未确认条目可能消失，本工具不把未提交内容描述为有服务端持久保证。
