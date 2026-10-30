<!-- 文件作用说明：提供经过源码核对的英文安装、配置、迁移、测试与备份操作指南。 -->
# Getting started with GoodChat

[English README](../../README.md) · [简体中文](../zh-CN/getting-started.md) · [Official website](https://goodchat.58it.cn)

GoodChat is a self-hosted live chat and customer support platform built with Go, PostgreSQL, Redis, Next.js, React and TypeScript. This guide describes the checked-in Docker Desktop setup and the current `dev` branch. The Compose file starts **one application container** and connects it to PostgreSQL and Redis that you provision separately.

## Prerequisites

| Component | Requirement |
| --- | --- |
| Docker | Docker Desktop with the Compose plugin for the supplied macOS setup |
| PostgreSQL | A dedicated GoodChat database and separate migration/application roles |
| Redis | An existing Redis service reachable by the application |
| Python | Python 3.9 or later for the local configuration and backup scripts |
| Go | Go 1.26.5 as declared in `backend/go.mod`, for host-side migrations and backend development |
| Node.js | Node.js 22 and npm for frontend development; Docker builds provide their own Node runtime |
| SMTP | Your own working SMTP configuration for email verification and account email flows |

The local helper expects existing containers named `local_postgres` and `jijie-redis`, with host ports `5432` and `56379`. It connects through `host.docker.internal`. Check your own container names, ports and network before using it. Linux and remote deployments require an explicit network and reverse-proxy configuration suited to that host.

## 1. Get the source

```bash
git clone --branch dev https://github.com/hhh0078/GoodChat.git
cd GoodChat
mkdir -p .local
chmod 700 .local
```

The examples below contain placeholders. Replace them locally; do not commit credentials, `.local/`, database backups or uploaded files.

## 2. Prepare a dedicated database

On a new installation, a database administrator must create an empty `goodchat_saas` database and the roles before migrations. The running application rejects superusers, `BYPASSRLS` roles and owners of business tables.

The following is a **fresh-instance provisioning example**, executed with a PostgreSQL administration account. Do not run it against an existing GoodChat database or reuse another project's database.

```sql
CREATE ROLE goodchat_migrator LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS
  PASSWORD 'REPLACE_WITH_A_UNIQUE_MIGRATION_PASSWORD';
CREATE ROLE goodchat_saas_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS
  PASSWORD 'REPLACE_WITH_A_DIFFERENT_APPLICATION_PASSWORD';
CREATE DATABASE goodchat_saas OWNER goodchat_migrator;
GRANT CONNECT ON DATABASE goodchat_saas TO goodchat_saas_app;
```

In that new empty database, use the administration account to assign the schema to the migration owner and remove public schema-creation permission:

```sql
ALTER SCHEMA public OWNER TO goodchat_migrator;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
```

Then connect to `goodchat_saas` as the migration owner and set the application grants **before** creating the tables:

```sql
GRANT USAGE ON SCHEMA public TO goodchat_saas_app;
ALTER DEFAULT PRIVILEGES FOR ROLE goodchat_migrator IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO goodchat_saas_app;
ALTER DEFAULT PRIVILEGES FOR ROLE goodchat_migrator IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO goodchat_saas_app;
```

The migrations grant access to specific rendering functions and revoke updates/deletes on immutable resource tables. Preserve those restrictions. A later blanket `GRANT ALL` can undo them. Keep the role name `goodchat_saas_app` when using the supplied migration grants; a different role needs an equivalent grant review.

## 3. Create private local configuration

Create the files below under `.local/`. These names are local helpers, not public configuration endpoints.

| File | Contents |
| --- | --- |
| `dev-database.json` | Separate `database_url` and `migration_url` connection strings |
| `smtp.json` | SMTP host, port, username, password and sender |
| `auth-secret` | A persistent random secret of at least 32 bytes |
| `container-data/` | Persistent application files; created by the preparation helper |
| `docker.env` | Generated container environment; created with mode `0600` |

Example `.local/dev-database.json`:

```json
{
  "database_url": "postgres://goodchat_saas_app:URL_ENCODED_APP_PASSWORD@127.0.0.1:5432/goodchat_saas?sslmode=disable",
  "migration_url": "postgres://goodchat_migrator:URL_ENCODED_MIGRATION_PASSWORD@127.0.0.1:5432/goodchat_saas?sslmode=disable"
}
```

Example `.local/smtp.json`:

```json
{
  "host": "smtp.example.com",
  "port": "587",
  "username": "your-smtp-account@example.com",
  "password": "YOUR_SMTP_PASSWORD",
  "from": "support@example.com"
}
```

`sslmode=disable` in the example is only for a controlled local connection. Use the appropriate verified TLS settings for remote database connections. URL-encode special characters in connection-string passwords.

Generate `.local/auth-secret` once without a trailing newline. This example refuses to overwrite an existing secret:

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

Keep that secret stable across restarts and include it in protected backups. It is also used to derive protection for stored AI credentials; replacing it is not a routine configuration refresh.

`backend/.env.example` lists the environment keys. It is an example file, not an automatic `.env` loader. In development, the Go configuration loader can read `dev-database.json`, `smtp.json` and `auth-secret` from `SUPPORT_DATA_DIR`, which defaults to `../.local` when commands run from `backend/`.

## 4. Migrate, build and start

First check the Go proxy and download the module dependencies. The checked-in Dockerfile uses `goproxy.cn` and the npm mirror `registry.npmmirror.com`.

```bash
cd backend
go env GOPROXY
GOPROXY=https://goproxy.cn,direct go mod download
go run ./cmd/migrate
cd ..
```

The migration command requires `MIGRATION_DATABASE_URL` or the private `migration_url` above. It runs ordered migrations with checksum verification. Do not edit SQL files that have already been applied.

Prepare the container configuration and start the single application service:

```bash
python3 scripts/prepare-docker.py
docker compose --env-file .local/docker.env build goodchat
docker compose --env-file .local/docker.env up -d --no-build goodchat
curl --fail http://127.0.0.1:13000/healthz
```

The helper checks that the two existing dependency containers are running. It does not create a PostgreSQL or Redis container, initialize the database or apply migrations. It writes a private development environment and maps the persistent directory to `/data`.

| Local endpoint | Purpose |
| --- | --- |
| `http://127.0.0.1:13000` | Website, account pages, agent workspace and administration |
| `http://127.0.0.1:13000/healthz` | Database/Redis health through the application gateway |
| `http://<release-id>.localhost:18081` | Isolated, version-specific visitor template rendering |

Use exactly the origin configured in `SUPPORT_PUBLIC_URL`. `localhost` and `127.0.0.1` are different browser origins. Do not open the internal Go API port as the main website.

For later updates to an existing installation, back up first and apply migrations with the migration credentials. The existing-container command is:

```bash
docker exec goodchat /app/migrate
```

The repository does not automatically deploy changes to [goodchat.58it.cn](https://goodchat.58it.cn).

## 5. Create accounts and configure the workspace

Email registration verifies the email address and creates a tenant with its owner. Invitations add members to an existing tenant. There is no public default administrator password or universal verification code.

For a new installation that needs its first platform administrator, the explicit bootstrap command is:

```bash
docker exec goodchat /app/bootstrap --email admin@example.com --name "Platform administrator"
```

Run it only once with your own administrator email. It refuses to overwrite an existing platform administrator. The generated password is written to `/data/bootstrap-account.json`, mapped to `.local/container-data/bootstrap-account.json` in the supplied setup. Read it privately and keep it out of issues, screenshots and Git.

A platform administrator and a tenant owner have different scopes. The optional `--test-tenant "Local demo"` flag creates an explicit local acceptance tenant only in development; production rejects that flag.

After signing in:

1. Invite agents and administrators; choose who can receive conversations.
2. Configure groups, skills, assignment pools and availability.
3. Create an access link and test it from a separate visitor session.
4. Choose one of the seven visitor templates, edit a private copy, preview it and publish a page.
5. Bind the access link to the published page, or keep the built-in visitor page.
6. Configure welcome rules, quick replies, leave messages and evaluations for the workspace.

## AI configuration

Use the administrator's upper-right **AI Assistant → Configuration** panel. Tenant AI settings and platform AI settings have separate ownership. Ordinary agent roles do not have the configuration or generation interface.

Configure the text/vision endpoint, model and key. Image generation has a separate endpoint/model configuration and needs provider permission for that model. An image model appearing in a provider catalog is not proof that the account can call it.

The assistant supports reference-image uploads, conversations and existing-template remixing. It accepts controlled text or file JSON, validates the resulting static package and keeps the platform chat scripts protected. Previews use in-memory sample messages; they do not send a real customer conversation.

Model keys are encrypted and are not returned as plaintext by the settings API. New assistant jobs use the selected tenant/platform configuration; old global `SUPPORT_AI_*` keys only remain for historical compatibility. `SUPPORT_AI_DAILY_LIMIT` defaults to 20 submissions per tenant-local day; `0` disables submissions. Provider token/image usage and billing must be checked with the provider.

## Development and verification

Choose a development setup that does not conflict with the Docker service's ports. With the same private development configuration and dependencies ready, the backend can run from `backend/` with `go run ./cmd/server`; the frontend can run from `frontend/` with `npm run dev`. The frontend proxies to `BACKEND_URL`, defaulting to `http://127.0.0.1:18080`.

Check your npm registry before installing frontend dependencies:

```bash
cd frontend
npm config get registry
npm ci --registry=https://registry.npmmirror.com
npm run dev
```

After stopping the development server, return to the repository root. Run the ordinary code checks there; the integration-test example below also starts from the repository root:

```bash
go -C backend test ./...
go -C backend vet ./...
npm --prefix frontend run typecheck
npm --prefix frontend test
npm --prefix frontend run format:check
npm --prefix frontend run build
```

External-service integration tests are opt-in. Provision an isolated PostgreSQL instance and non-privileged application role for them. Some fixture helpers fall back to local configuration when their environment variables are absent: **do not point these tests at the running `.local/dev-database.json` or a customer database**.

For decoration integration tests, `GOODCHAT_TEST_CONFIG` must point to a private JSON file with test-only application/migration DSNs. The fixture currently requires the database name `goodchat_saas`, so use a separate test instance rather than the running service's database:

```bash
GOODCHAT_TEST_CONFIG=/absolute/path/to/isolated-test-database.json \
  go -C backend test -race ./internal/decoration -count=1
```

Auth/billing integration fixtures support `SUPPORT_TEST_DATABASE_URL` and `SUPPORT_TEST_MIGRATION_URL`; review the target package's fixture before enabling its `SUPPORT_*_INTEGRATION` flag. Browser and Redis integration checks also require their explicitly configured test services.

`scripts/http-smoke.py` is a maintainer acceptance helper for `127.0.0.1:13000`. It expects a previously provisioned private acceptance account in `.local/bootstrap-account.json`, including its tenant ID. It performs authenticated reads/CSV downloads and writes local evidence; it does not provision a fresh installation or send business email.

## Backup and restore verification

The supplied backup tools are scoped to the existing `goodchat` and `local_postgres` containers and the `goodchat_saas` database. Confirm those are your intended resources before maintenance.

```bash
python3 scripts/backup.py --quiesce
```

This explicitly allows a brief application stop, then backs up the database, persistent files and identity secret as one set. If the application was running, the cleanup path starts it again; an already stopped application stays stopped. PostgreSQL and Redis containers are not stopped. The tool verifies archive readability and records hashes; readability alone is not a restore drill.

To verify a named backup in an isolated temporary database inside the existing PostgreSQL container:

```bash
python3 scripts/verify-backup.py BACKUP_DIRECTORY_NAME --allow-temporary-database
```

The verification restores only into its own temporary database, checks referenced attachment bytes and the secret archive, then removes that temporary database. It does not replace the running database, start a restored application or perform a production cutover.

Store `.local/backups/` securely and copy required backup sets off the host. Real restoration must preserve database records, uploaded files and the matching secret together.

## Production checklist and current scope

- Set `APP_ENV=production`, a persistent secret of at least 32 bytes and an HTTPS `SUPPORT_PUBLIC_URL`.
- Rebuild the frontend with the intended `NEXT_PUBLIC_SITE_URL`; Compose currently passes the public URL as that build argument.
- Use a separate registrable template-rendering domain with the required wildcard DNS/TLS and reverse proxy to the renderer. Preserve the origin checks, sandbox and Content Security Policy.
- Review PostgreSQL ownership, RLS grants, migration permissions, Redis isolation, trusted gateway headers and persistent-volume permissions.
- Configure the reverse proxy for ordinary HTTP and WebSocket upgrades. The supplied gateway forwards `/api/`, `/ws/` and `/healthz` to Go.
- Keep SMTP/AI credentials out of source control and logs. Validate provider permissions, email delivery, backup restoration and monitoring in your own environment.
- Test your actual browsers/devices and operational load. A successful build or sample preview does not prove real-device behavior, provider billing or production capacity.

GoodChat currently delivers the web application and responsive visitor pages. Native desktop/mobile clients and payment-channel integrations are separate work; this guide does not claim they are shipped.

For the detailed implementation model, see the [Chinese technical specification](../在线客服SaaS技术文档.md) and the repository's acceptance records.
