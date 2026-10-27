<!-- 文件作用说明：说明GoodChat网页SaaS的运行入口、已有数据库复用、私有配置、验证方式和实际交付边界。 -->
# GoodChat

Go + PostgreSQL + Redis + Next.js + React + TypeScript + Ant Design 的在线客服 SaaS。默认免费版 ¥0、长期有效、2 个客服坐席；平台可维护套餐价格、有效期、席位和功能开关，官网从发布配置读取定价。

已实现邮箱身份、租户成员邀请、链接接入、客户资料与标签、会话分配、实时聊天、进入回调、记录统计、私有装修模板、隔离预览发布、AI 任务和人工确认开通流程。需求中的完整目标范围与实际验证状态分别记录在 `docs/` 的三份主文档和开发验收记录中。

高级路由包含持久加权轮询、必备/可选技能、历史客服优先及当前接待资格重判。接待运营支持分层营业场景欢迎、图片/选项、评价期限和改评历史，以及留言附件和默认关闭的可靠邮件提醒。完整代码装修支持已构建静态ZIP、包内资源、源码另存新版本、受控Logo与独立来源发布；不执行租户构建脚本。AI当前生成结构化页面或内联HTML/CSS/JS，对资源包直接AI改稿会明确拒绝，避免丢失包内依赖。

## 本机运行

本项目只新增 `goodchat` 一个应用容器，复用现有 `local_postgres` 与 `jijie-redis`。PostgreSQL 使用独立的 `goodchat_saas` 数据库，应用使用非 superuser、非 BYPASSRLS 的 `goodchat_saas_app` 角色。现有其他项目数据库和 Redis 键保持独立，GoodChat 键统一以 `goodchat:` 开头。

```bash
python3 scripts/prepare-docker.py
docker compose --env-file .local/docker.env build goodchat
docker compose --env-file .local/docker.env up -d --no-build goodchat
curl --fail http://127.0.0.1:13000/healthz
```

官网与工作端入口为 `http://127.0.0.1:13000`，装修在独立版本来源 `http://<release-id>.localhost:18081` 运行。浏览器必须使用与 `SUPPORT_PUBLIC_URL` 完全相同的来源。Docker Desktop 使用稳定的 `host.docker.internal:5432/56379` 连接已有 PostgreSQL/Redis，不依赖容器重启后变化的 IP。

首次邮箱注册会创建自己的租户和所有者管理员。已有本机验收平台管理员凭证只保存在 `.local/bootstrap-account.json`（私有文件，不提交 Git），不设公共默认口令。正式环境第一次初始化平台管理员可执行容器内 `/app/bootstrap --email 实际邮箱 --name 管理员`；该命令拒绝覆盖既有账号或重复初始化，随机口令只写 `/data/bootstrap-account.json`。

## 配置与迁移

配置示例在 `backend/.env.example`。本机私有文件格式如下，真实值不要粘贴到源码或提交：

- `.local/dev-database.json`：`database_url` 是应用 DSN，`migration_url` 是独立迁移 DSN。
- `.local/smtp.json`：`host`、`port`、`username`、`password`、`from`。当前按用户授权复用已有 SMTP 的运行配置。
- `.local/auth-secret`：至少 32 字节随机服务密钥，重启和备份恢复时保留。
- `.local/docker.env`：脚本生成的容器环境，权限 0600。

在全新环境先由数据库管理员建立独立库与应用角色，并为迁移 owner 创建的表和序列配置应用权限；迁移使用独立 owner 连接，应用不得持有 superuser/BYPASSRLS 权限。所有迁移须在 `public` schema 执行，带有版本校验，不要修改已应用的 SQL。

```bash
cd backend
go run ./cmd/migrate
```

或在已有应用容器执行 `docker exec goodchat /app/migrate`。新表的 `SELECT/INSERT/UPDATE/DELETE`、序列的 `USAGE/SELECT` 以及受限渲染函数的 `EXECUTE` 需授予应用角色；本机已配置。迁移账户为 PostgreSQL 对象 owner，受限 SECURITY DEFINER 函数的 owner 必须按技术文档管理，应用本身不能拥有这些表。

生产需设置 `APP_ENV=production`、HTTPS 官网、独立可注册装修域名及 DNS/TLS，并在部署时重建 `NEXT_PUBLIC_SITE_URL`。Next.js、Go API、网关和代码渲染服务都在同一镜像内。容器持久目录 `.local/container-data` 保存私有附件；数据库、附件与服务密钥需联合备份。

AI 使用 `SUPPORT_AI_ENDPOINT`、`SUPPORT_AI_KEY`、`SUPPORT_AI_MODEL`，只从服务端读取。本机可保存 `.local/ai.json` 的 `endpoint/key/model/daily_limit`，Docker私有环境生成器会加载并允许环境变量覆盖。`SUPPORT_AI_DAILY_LIMIT` 默认每租户每日20次提交，0禁用；按租户时区计数，失败或取消不返提交次数，供应商token用量与实际费用单独核对。供应商缺失时明确返回不可用，不生成假结果。配置环境后重新生成私有 Docker 环境并重启应用。2026年10月27日已用授权配置实际生成租户私有模板、隔离预览并发布到入口，供应商报告2966 tokens；该用量不等于已经核对费用账单。

## 验证

```bash
cd backend
go test ./...
go vet ./...
SUPPORT_AUTH_INTEGRATION=1 go test -race ./internal/auth -count=1
SUPPORT_BILLING_INTEGRATION=1 go test -race ./internal/billing -count=1
SUPPORT_CHAT_INTEGRATION=1 go test -race ./internal/chat -count=1
SUPPORT_CALLBACK_INTEGRATION=1 go test -race ./internal/callbacks -count=1
GOODCHAT_TEST_CONFIG=/绝对项目路径/.local/dev-database.json go test -race ./internal/decoration -count=1
SUPPORT_REDIS_INTEGRATION=1 go test -race ./cmd/server -count=1
cd ../frontend
npm run typecheck
npm test
npm run format:check
npm run build
```

PostgreSQL 集成测试使用隔离schema或随机租户，使用非特权应用角色验证 RLS；不会新建数据库或容器。装修AI测试另用随机schema中的物理隔离调度队列，正式worker看不到测试任务。邮件测试使用本地假 SMTP；AI 协议测试使用本地测试供应商。Redis 并发测试只写随机的短期 `goodchat:rate:integration:` 键。浏览器截图与私有验收数据放在 `.local/evidence/`。`python3 scripts/http-smoke.py` 使用本机私有验收账号进行26项实际HTTP检查，不发送业务邮件。

`python3 scripts/backup.py --quiesce` 会短暂停止唯一GoodChat应用，再通过现有PostgreSQL生成数据库、附件与身份密钥备份集并恢复应用。`python3 scripts/verify-backup.py 备份目录名 --allow-temporary-database` 在同一现有PG容器的临时库恢复，核对附件与密钥归档后删除本次临时库；不会覆盖运行库或创建容器。本轮已执行隔离恢复，恢复后的应用启动和生产切换仍未演练。备份保存在 `.local/backups/`，包含私有身份数据，不能提交Git。

即时CSV最多10000行/32MiB；批量导出任务最多100000行/64MiB、结果24小时有效，每租户两个待执行任务、每日20次提交、临时结果加待执行预留合计最多256MiB。申请者下载时重新检查角色与权益；有新的已提交消息撤回时，旧正文文件失效并要求重新生成，会话元数据不受影响。当前消息只软撤回；引入物理清理前须将撤回数量快照升级为单调版本。

## 文档

`docs/在线客服SaaS需求文档.md`、`docs/在线客服SaaS技术文档.md`、`docs/在线客服SaaS开发顺序与前端规范.md` 为主稿；可编辑 Word 在 `文档/`。`scripts/build-documents.py` 使用 python-docx 生成 Word，生成后须按文档工作流渲染并逐页检查，不能以文本提取代替排版验收。

原生 macOS、Windows、Android 客服端后续独立开发。当前为本地网页验证环境，真实收件、真实模型、支付渠道、生产 HTTPS/域名、真实设备和负载容量的证据不能由构建成功替代。
