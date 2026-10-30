<!-- 文件作用说明：提供经过源码核对的中文安装、配置、迁移、测试与备份操作指南。 -->
# GoodChat 安装与运行指南

[中文 README](../../README.zh-CN.md) · [English](../en/getting-started.md) · [官方网站](https://goodchat.58it.cn)

GoodChat 是使用 Go、PostgreSQL、Redis、Next.js、React 和 TypeScript 构建的可自部署在线客服系统。本指南对应当前 `dev` 分支以及仓库中的 Docker Desktop 本机配置。Compose 只启动 **一个应用容器**，PostgreSQL 和 Redis 由部署者提前准备。

## 环境要求

| 组件 | 要求 |
| --- | --- |
| Docker | 所附 macOS 方案使用 Docker Desktop 和 Compose 插件 |
| PostgreSQL | 独立的 GoodChat 数据库、独立迁移账号与非特权应用账号 |
| Redis | 应用能够访问的既有 Redis 服务 |
| Python | 本机配置和备份脚本需要 Python 3.9 或以上 |
| Go | `backend/go.mod` 声明 Go 1.26.5；本机执行迁移和后端开发需要安装 |
| Node.js | 前端开发使用 Node.js 22 和 npm；Docker 构建自带 Node 运行环境 |
| SMTP | 邮箱验证等账号邮件流程需要自行配置可用 SMTP |

本机辅助脚本固定检查名为 `local_postgres`、`jijie-redis` 的既有容器，使用宿主端口 `5432`、`56379`，通过 `host.docker.internal` 连接。使用前核对自己的容器名称、端口和网络。Linux 或远程服务器需要按实际主机单独配置网络与反向代理。

## 1. 获取源码

```bash
git clone --branch dev https://github.com/hhh0078/GoodChat.git
cd GoodChat
mkdir -p .local
chmod 700 .local
```

下面的连接口令、邮箱和 SMTP 均为占位值，请在本机替换。密钥、`.local/`、数据库备份和上传文件不应提交 Git。

## 2. 准备独立数据库

全新安装前，由数据库管理员创建空的 `goodchat_saas` 数据库和两个账号。运行应用会拒绝超级用户、带 `BYPASSRLS` 的账号以及业务表所有者。

下面仅为使用 PostgreSQL 管理账号执行的 **全新独立实例初始化示例**，不要对已有 GoodChat 数据库重复执行，也不要使用其它项目的业务库。

```sql
CREATE ROLE goodchat_migrator LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS
  PASSWORD 'REPLACE_WITH_A_UNIQUE_MIGRATION_PASSWORD';
CREATE ROLE goodchat_saas_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS
  PASSWORD 'REPLACE_WITH_A_DIFFERENT_APPLICATION_PASSWORD';
CREATE DATABASE goodchat_saas OWNER goodchat_migrator;
GRANT CONNECT ON DATABASE goodchat_saas TO goodchat_saas_app;
```

在这个新建空库中，先用数据库管理账号设置 schema 所有者并取消所有用户默认的建对象权限：

```sql
ALTER SCHEMA public OWNER TO goodchat_migrator;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
```

再以迁移所有者连接 `goodchat_saas`，在创建业务表之前设置应用账号的默认权限：

```sql
GRANT USAGE ON SCHEMA public TO goodchat_saas_app;
ALTER DEFAULT PRIVILEGES FOR ROLE goodchat_migrator IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO goodchat_saas_app;
ALTER DEFAULT PRIVILEGES FOR ROLE goodchat_migrator IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO goodchat_saas_app;
```

迁移会单独授权受限渲染函数，并撤销不可变资源表的更新/删除权限。应保留这些约束，迁移后执行笼统的 `GRANT ALL` 可能破坏保护。直接使用所附迁移时，应用角色保留为 `goodchat_saas_app`；使用其它名称需要同步检查对应授权。

## 3. 准备私有本机配置

在 `.local/` 下创建以下文件。这些是本机辅助脚本读取的文件，不是公开配置接口。

| 文件 | 内容 |
| --- | --- |
| `dev-database.json` | 分开的应用 `database_url` 与迁移 `migration_url` |
| `smtp.json` | SMTP 主机、端口、账号、密码与发件人 |
| `auth-secret` | 至少 32 字节、持久保存的随机密钥 |
| `container-data/` | 应用持久文件目录，由准备脚本创建 |
| `docker.env` | 脚本生成的容器环境文件，权限为 `0600` |

`.local/dev-database.json` 示例：

```json
{
  "database_url": "postgres://goodchat_saas_app:URL_ENCODED_APP_PASSWORD@127.0.0.1:5432/goodchat_saas?sslmode=disable",
  "migration_url": "postgres://goodchat_migrator:URL_ENCODED_MIGRATION_PASSWORD@127.0.0.1:5432/goodchat_saas?sslmode=disable"
}
```

`.local/smtp.json` 示例：

```json
{
  "host": "smtp.example.com",
  "port": "587",
  "username": "your-smtp-account@example.com",
  "password": "YOUR_SMTP_PASSWORD",
  "from": "support@example.com"
}
```

示例中的 `sslmode=disable` 只适合受控的本机连接。远程数据库应使用适当的 TLS 证书校验参数。连接串口令中的特殊字符需要 URL 编码。

一次性生成末尾没有换行的 `.local/auth-secret`。下面的示例会拒绝覆盖已有密钥：

```bash
python3 - <<'PYTHON'
import os
import secrets
with open(".local/auth-secret", "x", encoding="utf-8") as output:
    output.write(secrets.token_urlsafe(48))
os.chmod(".local/auth-secret", 0o600)
PYTHON
chmod 600 .local/dev-database.json .local/smtp.json .local/auth-secret
```

重启和恢复时保留同一个密钥，并将其纳入受保护的备份。已保存 AI 凭证的加密密钥也从它派生，因此更换它不是普通的配置刷新。

`backend/.env.example` 列出环境变量，但程序不会自动加载任意 `.env` 文件。开发模式的 Go 配置加载器可以从 `SUPPORT_DATA_DIR` 读取上述数据库、SMTP 和密钥文件；从 `backend/` 执行命令时，默认目录为 `../.local`。

## 4. 执行迁移、构建并启动

先检查 Go 代理，再下载依赖。仓库 Dockerfile 使用 `goproxy.cn` 和 npm 镜像 `registry.npmmirror.com`。

```bash
cd backend
go env GOPROXY
GOPROXY=https://goproxy.cn,direct go mod download
go run ./cmd/migrate
cd ..
```

迁移命令需要 `MIGRATION_DATABASE_URL`，或上述私有文件中的 `migration_url`。迁移按顺序执行并核对已应用 SQL 的摘要，不要修改已经执行过的迁移文件。

随后生成容器配置并启动唯一应用服务：

```bash
python3 scripts/prepare-docker.py
docker compose --env-file .local/docker.env build goodchat
docker compose --env-file .local/docker.env up -d --no-build goodchat
curl --fail http://127.0.0.1:13000/healthz
```

准备脚本会检查两个既有依赖容器是否运行，但不会新建 PostgreSQL/Redis 容器、创建数据库或执行迁移。它生成开发环境配置，并把持久文件目录挂载到 `/data`。

| 本机地址 | 用途 |
| --- | --- |
| `http://127.0.0.1:13000` | 官网、账号、客服工作台与管理后台 |
| `http://127.0.0.1:13000/healthz` | 通过应用网关检查数据库和 Redis |
| `http://<release-id>.localhost:18081` | 按发布版本隔离的用户端模板渲染 |

浏览器访问地址必须与 `SUPPORT_PUBLIC_URL` 配置的来源一致。`localhost` 和 `127.0.0.1` 是不同的浏览器来源，不要直接把内部 Go API 端口当作官网入口。

升级已有环境时，先备份，再用迁移权限执行迁移。现有容器内的命令为：

```bash
docker exec goodchat /app/migrate
```

仓库命令不会自动把应用发布到 [goodchat.58it.cn](https://goodchat.58it.cn)。

## 5. 创建账号与配置工作空间

邮箱注册会验证邮箱并创建租户和所有者；成员邀请用于加入已有租户。项目没有公开默认管理员口令或万能验证码。

全新环境需要第一个平台管理员时，可显式执行：

```bash
docker exec goodchat /app/bootstrap --email admin@example.com --name "平台管理员"
```

请替换为自己的管理员邮箱，只在首次初始化时执行。命令会拒绝覆盖已有平台管理员。随机口令写入 `/data/bootstrap-account.json`，在所附挂载方案中对应 `.local/container-data/bootstrap-account.json`，请私下读取，不要提交、截图或粘贴到公开问题中。

平台管理员与租户所有者权限范围不同。可选的 `--test-tenant "本机演示"` 参数只用于开发环境显式创建验收租户，生产环境会拒绝该参数。

登录后可依次完成：

1. 邀请客服与管理员，设置可接待成员。
2. 配置分组、技能、分配池和接待状态。
3. 创建接入链接，用独立访客会话测试咨询。
4. 选择七套用户端模板之一，编辑私有副本、预览并发布页面。
5. 将链接绑定到已发布页面，或继续使用内置默认用户端。
6. 配置工作空间的欢迎语、快捷回复、留言与评价。

## AI 配置

管理员可在右上角 **AI 助手 → 配置** 中维护自己的 AI。租户配置和平台配置各自归属，普通客服角色没有该配置和生成功能入口。

配置文本/视觉模型的地址、模型和密钥；图片生成有独立的模型和地址设置，并且需要服务商实际开放相应模型权限。模型目录可见并不代表当前账号能调用。

助手支持上传参考图片、多轮对话和已有模板二创，接收受控文本或文件 JSON，并校验生成的静态包。平台聊天脚本始终受保护，预览使用内存模拟消息，不会发送真实客户会话。

模型密钥加密保存，设置接口不回传明文。新任务使用选定租户/平台自己的配置；旧的全局 `SUPPORT_AI_*` 仅为历史兼容保留。`SUPPORT_AI_DAILY_LIMIT` 默认按租户当地自然日限制 20 次提交，`0` 表示禁用提交；token、图片用量和实际账单应向服务商单独核对。

## 本机开发与检查

本机开发应避开 Docker 应用正在使用的端口。同一套私有配置和依赖准备好后，可从 `backend/` 执行 `go run ./cmd/server`，从 `frontend/` 执行 `npm run dev`。前端通过 `BACKEND_URL` 代理后端，默认是 `http://127.0.0.1:18080`。

安装前端依赖前先检查 npm 镜像：

```bash
cd frontend
npm config get registry
npm ci --registry=https://registry.npmmirror.com
npm run dev
```

停止开发服务后，先回到仓库根目录，再运行普通代码检查；下面的集成测试示例也从仓库根目录开始：

```bash
go -C backend test ./...
go -C backend vet ./...
npm --prefix frontend run typecheck
npm --prefix frontend test
npm --prefix frontend run format:check
npm --prefix frontend run build
```

依赖外部服务的集成测试需要显式开启，并准备隔离 PostgreSQL 实例及非特权应用角色。部分测试在缺少环境变量时会回退读取本机配置，**不要把这些测试指向运行服务使用的 `.local/dev-database.json` 或客户数据库**。

装修集成测试要求 `GOODCHAT_TEST_CONFIG` 指向仅含测试应用/迁移连接串的私有 JSON。当前 fixture 要求库名为 `goodchat_saas`，因此请使用独立测试实例，而不是运行服务的同名数据库：

```bash
GOODCHAT_TEST_CONFIG=/absolute/path/to/isolated-test-database.json \
  go -C backend test -race ./internal/decoration -count=1
```

身份/套餐集成测试支持 `SUPPORT_TEST_DATABASE_URL`、`SUPPORT_TEST_MIGRATION_URL`。开启对应 `SUPPORT_*_INTEGRATION` 标志前，应先检查目标包的 fixture 配置方式；浏览器、Redis 集成检查也需要显式准备测试服务。

`scripts/http-smoke.py` 是维护者针对 `127.0.0.1:13000` 的验收辅助脚本，要求 `.local/bootstrap-account.json` 中已有经过准备的私有验收账号和租户 ID。它执行登录后的读取/CSV 下载并保存本地证据，不负责全新安装初始化，也不发送业务邮件。

## 备份与恢复核验

仓库备份工具固定面向现有 `goodchat`、`local_postgres` 容器与 `goodchat_saas` 数据库。维护前先确认这些正是自己的目标资源。

```bash
python3 scripts/backup.py --quiesce
```

该参数明确允许短暂停止应用，随后把数据库、持久文件和身份密钥组成同一备份集。原先正在运行的应用会在清理路径重新启动；原先已停止的应用保持停止，不会停止 PostgreSQL 或 Redis。脚本核对归档可读性并记录摘要，归档可读本身不等于恢复演练。

核验某个备份目录，可在现有 PostgreSQL 容器内的临时库执行隔离恢复：

```bash
python3 scripts/verify-backup.py BACKUP_DIRECTORY_NAME --allow-temporary-database
```

工具只恢复到它自己创建的临时库，核对附件引用字节与密钥归档后删除该临时库。它不会替换运行库、启动恢复后的应用或执行生产切换。

请安全保存 `.local/backups/`，并把需要的备份集复制到本机以外的安全位置。真正恢复时应一起保留数据库记录、上传文件和对应密钥。

## 生产环境要求与交付范围

- 设置 `APP_ENV=production`、至少 32 字节的持久密钥，以及 HTTPS 的 `SUPPORT_PUBLIC_URL`。
- 使用实际 `NEXT_PUBLIC_SITE_URL` 重新构建前端；当前 Compose 会将公开地址传入该构建参数。
- 为模板渲染准备独立的可注册域名、通配 DNS/TLS 与指向渲染服务的反向代理，保留来源校验、sandbox 和 CSP。
- 核对数据库所有权、RLS 授权、迁移权限、Redis 隔离、可信网关头和持久目录权限。
- 反向代理同时处理 HTTP 和 WebSocket 升级；所附网关将 `/api/`、`/ws/`、`/healthz` 转发给 Go。
- 密钥不进入源码和日志；在自己的环境核验模型权限、实际收件、备份恢复与监控。
- 对实际浏览器、设备和业务负载单独验收。构建成功或模拟预览不能代替真机行为、服务商账单与生产容量证明。

目前交付的是网页应用与响应式用户端页面；原生桌面/移动端和支付渠道集成属于独立工作，本指南不将其称作已交付。

详细实现模型参阅[技术文档](../在线客服SaaS技术文档.md)及仓库中的验收记录。
