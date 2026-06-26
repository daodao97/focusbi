# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

FocusBI is a SQL-report management system: users write report templates (SQL + annotations + filters), the engine executes them against configured data sources and returns structured results that a Vue3 frontend renders as tables/charts. It is a Go rewrite of a PHP system called **dataddy** (located at `../../dataddy`) — many features are ports, and dataddy is the reference when porting more. The authoritative template-syntax spec is `docs/SYNTAX.md`; the architecture/ops overview is `docs/REPORT.md`.

## Commands

```bash
make run        # build frontend + run backend (dev) on :8099  (= make web && go run ./cmd --app-env dev --bind :8099)
make web        # build frontend only -> web/dist (embedded into the Go binary)
make web_dev    # frontend dev server with HMR, proxies /api to :8099
make build      # production single binary -> build/server (embeds web/dist)

go test ./...                                   # all Go tests
go test ./internal/engine/                      # the report engine (most logic lives here)
go test ./internal/engine/ -run TestRunJoin -v  # a single test
gofmt -w <file>                                 # format before committing; CI-style check: gofmt -l <dir>
```

- Cron/subscription scheduler only runs when `ENABLE_CRON=true` (see `cmd/main.go`).
- Frontend uses **pnpm** (not npm). Building requires `pnpm install` (handled by `make web`).
- The `default` database (`conf.database[default]`) must be **MySQL** — migrations auto-run via goose on startup and only support MySQL (`dao/dao.go:initSchema`). Report *data sources* (the `dsn` table) additionally support PostgreSQL and SQLite.
- Demo data: `mysql ... < docs/schema.sql` (sales tables + a sample report).

## Architecture

**Request flow**: `cmd/main.go` boots via `xgo/xapp` (startup hooks: `conf.Init` → redis → `dao.Init`), then serves Gin routes from `api.Setup`. `dao.Init` runs goose migrations (`db/migrations/*.sql`, embedded) and instantiates `xdb.Model` handles for each table.

**The report engine (`internal/engine/`) is the core.** A report `content` string is parsed and executed by `Runner.Run(content, params)` → `*Result` (filters + blocks). Key sub-flow:

- `parser.go` splits content into blocks. Block kinds: `sql`, `markdown`, `raw`, and `#!SCRIPT` (JavaScript via goja, see `script.go`). `#!SCRIPT...#!END` is extracted *before* semicolon-splitting because JS contains semicolons.
- Filters `${name|label|default|type}` (`filter.go`) become frontend input controls; their values feed **macros** `{name}` (`macro.go`) substituted into SQL before execution. `enum_sql` filters run their own query to populate dropdowns.
- For SQL blocks, `runSQLData` runs a fixed **data pipeline** (order is independent of annotation order): query → `@filter` → `@date_line` → `@sort` → `@series` (pivot) → `@flip` (transpose). Then a **display phase** (`mergeGroup.finalize`) applies column config, `@chart`, `@sum`/`@avg`, `@data_fluctuations`, etc.
- **Block merging** (`@join`/`@union`, `jointable.go`): SQL blocks are pushed with a one-block delay (`pending *mergeGroup`). A block annotated `@join`/`@union` merges its rows into the previous block's `arrayTable` instead of producing its own block; an unannotated block flushes the group. So editing the loop in `engine.go:Run` requires preserving this deferred-flush ordering.
- Annotations (`-- @key=value`) and column config (`AS alias -- @{...json}`) are parsed per-block into `rb.annotations` / `rb.colConfigs`. `types.go` defines the output shape (`Block`, `Column`, `Result`).

**Data sources (`internal/datasource/`)**: `Query(dsnName, sql)` resolves a named DSN (`"default"` → main DB config; others → `dsn` table), pools `*sql.DB` by a config fingerprint, and supports **SSH tunneling for MySQL** (`ssh.go`). `engine/cache.go` wraps queries with a TTL cache keyed on dsn+sql (`@sql_cache`).

**AI (`internal/ai/`)**: conversational template editing via **Eino** (`cloudwego/eino`) unified ChatModel — providers `claude` (default) and `openai`. Config is read **only from `conf.ai`** (no env-var override). The model first streams a one-line explanation, then calls a `propose_template_patch` tool returning SEARCH/REPLACE blocks (`diff.go`), with full-template rewrite as fallback. `docs/SYNTAX.md` is embedded as the system prompt. Note: the shared HTTP client rewrites `User-Agent` to `focusbi/1.0` because the anthropic-sdk-go default UA gets 403'd by some Cloudflare-fronted gateways.

**Subscriptions (`internal/subscription/` + `job/`)**: `job.NewCronServer` registers a per-minute xcron tick (distributed-locked). `Tick` scans enabled subscriptions, atomically claims each due one (`ClaimSubscriptionRun`, prevents multi-instance dupes), runs the report, and pushes to lark/wework webhooks. Two modes: unconditional scheduled push, or threshold alarm (`SubCondition`). `@data_fluctuations` messages on the report surface in `Result.Messages` and are folded into the push body.

**MCP server (`internal/mcpserver/` + `api/mcp.go`)**: exposes report-development tools to AI clients (Codex/Claude Code) over `/mcp` (Streamable HTTP, official `modelcontextprotocol/go-sdk`). Auth is the security-critical part: the SDK's `RequireBearerToken` middleware verifies a Bearer token via `VerifyToken` (an `fbt_`-prefixed API token from the `api_token` table, or a login JWT), puts the user id in the request context, and `principalFromContext` loads the user + compiles `auth.NewPermission`. **Every tool gates on the caller's RBAC** (same `report.manage`/`dsn` resources as REST) — tools never bypass permissions. Report-readability logic is shared with `api/` via `auth.ReportReadable`/`LoadReportParents`. API tokens store only a SHA-256 hash; plaintext is shown once at creation.

**Frontend (`web/`)**: Vite MPA, two entries — `index.html` (`src/console/`, management console: report list/editor/datasources) and `view.html` (`src/viewer/`, standalone shareable report view). Built `web/dist` is `go:embed`ed and served by `api/ui.go`. `docs/SYNTAX.md` is also served (the in-app "docs" button fetches it), so it is simultaneously the human spec, the AI system prompt, and the in-app help — edit it in one place.

## Conventions

- Comments and commit messages in this repo are written in **Chinese**; match the surrounding style.
- Report-level page settings (auto-refresh, prepend HTML) live in `report.settings` JSON, parsed by `ReportRecord.ParseSettings()` and copied onto `Result` at the two `Run` call sites (`api/setup.go`, `api/public.go`) — keep both in sync.
- When porting a dataddy feature, read its PHP implementation under `../../dataddy/application/library/MY/Data/Template.php` first; the Go ports note their origin in comments.
- `conf.dev.yaml` is committed and contains real-looking dev secrets; it is the dev config loaded by `--app-env dev`.
