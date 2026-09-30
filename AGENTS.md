# AGENTS.md

Guidance for agents (Claude Code, Codex, etc.) working in this repository.

## Project

Release readiness dashboard for the Quay container registry: a Go backend plus
an embedded React/PatternFly SPA, backed by SQLite. It ingests Konflux build
snapshots and CTRF test results from S3-compatible object storage, syncs JIRA
issues per release, and serves both a REST API and the SPA from one binary.

## Build, test, lint

Build the web bundle first — `web/embed.go` embeds `web/dist/`, so the Go
build fails without it.

```bash
cd web && npm ci && npm run build && npm run lint && cd ..
go build ./...
go test ./...
go vet ./...
```

- Go lint config: `.golangci.yml` (`golangci-lint run`).
- Web lint/format: Biome, config in `web/biome.json` (`npm run lint`).
- `npm run build` includes a `tsc -b` type-check.
- CI (`.github/workflows/ci.yaml`) runs on Node 24; Node 22 also works locally.
- Single package: `go test ./internal/jira/`.

## Run locally

Garage (S3-compatible) provides object storage; SQLite is a local file.

1. `dev/garage-setup.sh` expects `dev/garage.toml`, which is not tracked —
   create it (fixed RPC secret, sqlite metadata engine, ports below):

   ```toml
   metadata_dir = "/var/lib/garage/meta"
   data_dir = "/var/lib/garage/data"
   db_engine = "sqlite"
   replication_factor = 1
   rpc_bind_addr = "0.0.0.0:3901"
   rpc_secret = "<openssl rand -hex 32>"
   [s3_api]
   s3_region = "garage"
   api_bind_addr = "0.0.0.0:3900"
   root_domain = ".s3.garage.localhost"
   [admin]
   api_bind_addr = "0.0.0.0:3903"
   ```

2. `./dev/garage-setup.sh` — starts a fixed-name container `garage-dev` on
   ports 3900/3901/3903, wipes and recreates `dev/garage/` each run, and
   writes credentials to `dev/s3.env`.
3. Export S3 credentials and run the backend, pointed at the web dev
   server's proxy port and the Garage S3 endpoint:

   ```bash
   set -a; source dev/s3.env; set +a
   go run ./cmd/release-readiness -addr :8088 -db /tmp/rr.db \
     -s3-endpoint http://127.0.0.1:3900
   ```

   Use `http://127.0.0.1:3900` for the S3 endpoint — `localhost` resolves to
   `::1` on some hosts and the connection resets. `dev/s3.env` has plain
   `KEY=val` lines with no `export`, so `source` alone leaves the backend's
   env unset; `set -a` exports everything sourced afterward.
4. `cd web && npm run dev` — Vite dev server on 5173, proxies `/api` to
   `:8088`.

JIRA is optional: without `-jira-token`/`JIRA_TOKEN`, release overview pages
stay empty unless a `release_versions` row exists (JIRA sync is what
normally creates those rows).

## Layout

| Path | Purpose |
|---|---|
| `cmd/release-readiness/` | CLI entry point; flags/env, DB open, S3 + JIRA sync loops, HTTP serve |
| `internal/server/` | HTTP routes (`routes.go`), API handlers (`handlers_api.go`), embeds `web/dist/` via `web/embed.go` |
| `internal/db/` | SQLite layer: `schema.sql`, `migrations.go`, hand-written query wrappers |
| `internal/db/sqlc/` | Generated code from `sqlc generate` — commit alongside schema/query changes |
| `internal/db/queries/` | sqlc query inputs (`*.sql`) |
| `internal/s3/` | S3/Garage client and snapshot/CTRF sync |
| `internal/jira/` | JIRA REST client; discovers releases, syncs issues by fixVersion |
| `internal/konflux/` | Converts Konflux Snapshot spec JSON into `model.Snapshot` |
| `internal/ctrf/` | CTRF (Common Test Report Format) JSON types |
| `internal/clair/` | Clair vulnerability scan report types |
| `internal/model/` | Shared data types |
| `web/` | React 19 + TypeScript SPA, PatternFly 6, built with Vite 8 |
| `deploy/` | Kubernetes manifests (Deployment, Service, Route, PVC) + `Containerfile` |
| `dev/` | Local dev scripts (Garage setup, sample upload scripts) |
| `.github/workflows/` | CI: Go/web build+lint+test, sqlc diff, container build |
| `.tekton/` | Konflux pipeline definitions (pull-request, push) |

## Data contract

`internal/s3/` discovers objects at:

- `{application}/snapshots/{name}/snapshot.json` — the Konflux Snapshot's
  `.spec` only, not the full Custom Resource.
- `{application}/snapshots/{name}/{suite}/results/ctrf-report.json` — a CTRF
  JSON report, not JUnit XML.

`dev/upload-snapshots.sh` and `dev/its-upload.sh` currently upload the full
Snapshot CR and JUnit XML respectively, so objects they produce do **not**
ingest correctly (raw CR yields `application=""` with no components; JUnit
output produces no test suite). Treat them as references to fix, not as a
working ingestion path.

## Schema and sqlc

Edit `internal/db/schema.sql` and `internal/db/queries/*.sql`, then run
`sqlc generate` (config: `sqlc.yaml`) and commit the regenerated
`internal/db/sqlc/` files in the same change.

- `internal/db/migrations.go` only runs `CREATE TABLE IF NOT EXISTS` — there
  is no `ALTER` migration path for changing existing tables.
- Generated file headers pin the sqlc version that produced them (currently
  v1.30.0) while CI (`.github/workflows/sqlc.yaml`) installs `sqlc@latest`,
  so `sqlc diff` can fail on the header line alone even when the SQL is
  unchanged. Regenerate with a matching sqlc version before relying on
  `sqlc diff`.

## Gotchas

- `README.md` describes JUnit-based ingestion; the code requires CTRF.
- `dev/garage-setup.sh` deletes and recreates `dev/garage/` on every run and
  uses a fixed container name and host ports (3900/3901/3903) — only one
  instance can run at a time per host.

## Conventions

- Conventional commit messages.
- Go: stdlib `net/http`, no CGO (`modernc.org/sqlite`).
