# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Release readiness dashboard for Quay container registry. Tracks build snapshots, integration test results, and JIRA issues across release versions. Full-stack Go backend + React SPA frontend with SQLite storage.

## Build & Run Commands

### Backend (Go)
```bash
go build -o release-readiness ./cmd/release-readiness/  # Build binary
go test ./...                                    # Run all tests
go test ./internal/jira/                         # Run tests for a single package
GOTOOLCHAIN=go1.27.1 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...  # Lint as CI does (needs web/dist)
```

`go run pkg@version` ignores this module's `toolchain` line, so pin `GOTOOLCHAIN` to the `go.mod` toolchain; a golangci-lint built with an older Go fails with "can't load config".

### Frontend (web/)
```bash
cd web && npm install                            # Install dependencies
cd web && npm run dev                            # Vite dev server (proxies /api to :8088)
cd web && npm run build                          # Production build (output: web/dist/)
cd web && npm run lint                           # Biome lint (src/ only)
dev/e2e.sh                                       # Playwright e2e against fixtures (see below)
```

### Container
```bash
podman build -f deploy/Containerfile -t release-readiness .
```

### Running locally
```bash
# Start backend (source dev/s3.env for S3 creds)
./release-readiness -addr :8088 -db release-readiness.db \
  -s3-endpoint http://localhost:3900 -s3-region garage \
  -s3-bucket quay-release-readiness \
  -s3-access-key $AWS_ACCESS_KEY_ID -s3-secret-key $AWS_SECRET_ACCESS_KEY \
  -jira-token $JIRA_TOKEN

# In separate terminal, start frontend dev server
cd web && npm run dev
```

The Vite dev server proxies `/api` requests to `localhost:8088` (the Go backend).

### End-to-end (Playwright)

`dev/e2e.sh` builds the SPA and binary, starts a fixture stub (`dev/e2e/stub`, serving `dev/e2e/fixtures`) and the backend on free ports with a scratch SQLite DB under `/tmp`, waits for data, then runs `web/e2e/*.spec.ts` in Chromium. It tears everything down on exit.

Prerequisites: Go (version in `go.mod`), Node 22+ (CI uses 24), `jq`, `curl`. The script runs `npm ci` in `web/` when `node_modules` is missing or older than `package-lock.json`, and installs Playwright's Chromium itself (browser only, not its system libraries).

```bash
dev/e2e.sh                                       # fixture mode: full assertions on fixture data (CI runs this)
dev/e2e.sh --live                                # live Konflux: smoke checks only, local only
dev/e2e.sh --keep --no-tests                     # leave stub + backend running to poke at
dev/e2e.sh -g "snapshots"                        # extra args pass through to playwright test
```

- Live mode reads `KONFLUX_KUBECONFIG` (default `~/.kube/configs/art.yaml`), `KONFLUX_CONTEXT` (default: the kubeconfig's current-context) and `KONFLUX_NAMESPACE` (default `art-quay-tenant`). JIRA uses the fixture stub unless `JIRA_TOKEN` (and optionally `JIRA_URL`) is set.
- `dev/e2e/record-fixtures.sh` re-records `konflux-snapshots.json` from live Konflux (same env vars as live mode, read-only), trimmed to the newest 3 snapshots of quay-3-16/17/18 and their fbc- apps. JIRA fixtures are hand-written and re-recorded (scrubbed) only when `JIRA_TOKEN` and `JIRA_EMAIL` are set; the stub serves issues from `jira-issues-<fixVersion>.json`. Update the fixture specs in `web/e2e/` after re-recording.
- Artifacts land in `web/e2e-artifacts/<run timestamp>/` (gitignored): `test-results/` holds a full-page screenshot per page plus a screenshot and trace per test, and `report/` holds the HTML report (`npx playwright show-report web/e2e-artifacts/<run>/report`). CI uploads the directory as the `e2e-artifacts` artifact.
- QA convention: run `dev/e2e.sh` for any UI- or API-visible change, and cite on the bead the Playwright pass-count line (e.g. `3 passed`) and the screenshot paths.

## Architecture

### Backend (`internal/`)
- **`cmd/release-readiness/main.go`** — CLI entry point. Runs background sync loops for S3, JIRA, and Konflux.
- **`internal/server/`** — HTTP server using Go stdlib `net/http`. Routes registered in `routes.go`, API handlers in `handlers_api.go`. The React SPA is served from embedded `web/dist/` via `go:embed` with SPA fallback routing.
- **`internal/db/`** — SQLite data layer (pure-Go driver `modernc.org/sqlite`, no CGO). Schema migrations in `migrations.go`. WAL mode enabled.
- **`internal/s3/`** — AWS SDK v2 client for fetching snapshot data from S3/Garage object storage.
- **`internal/jira/`** — JIRA REST API client. Discovers active releases, syncs issues by fixVersion.
- **`internal/kube/`** — Minimal read-only Kubernetes REST client (stdlib `net/http` + `yaml.v3`, no `client-go`): `LoadKubeconfig` (bearer token or service-account token file only; `exec`/auth-provider/client-cert is a startup error) and a paginated `Client.List`.
- **`internal/konflux/`** — Konflux Snapshot ingestion. Lists `snapshots.appstudio.redhat.com` through `internal/kube`, read-only (list only). Flags: `-konflux-kubeconfig` (env `KONFLUX_KUBECONFIG`, empty = disabled), `-konflux-context` (env `KONFLUX_CONTEXT`, empty = current-context), `-konflux-namespace` (env `KONFLUX_NAMESPACE`, empty = context namespace), `-konflux-poll-interval` (default `5m`). Persists into the existing `snapshots`/`snapshot_components`/`components` tables via `db.CreateSnapshotWithComponents`, shared with the S3 sync; every snapshot is ingested; Konflux test status is not read (tests run in Prow).
- **`internal/model/`** — Shared data types used across packages.
- **`internal/ctrf/`** — CTRF (Common Test Report Format) JSON types.

### Frontend (`web/`)
- React 19 + TypeScript, built with Vite 6
- UI framework: **PatternFly 6** (Red Hat design system)
- API client in `web/src/api/client.ts`, types in `web/src/api/types.ts`
- Pages: `ReleasesOverview`, `ReleaseDetail`, `SnapshotsList`

### Data Flow
1. S3 sync loop polls for new snapshots → ingests into SQLite (components, test results)
2. JIRA sync loop discovers active releases → syncs issues per fixVersion into SQLite
3. React SPA fetches data via `/api/v1/` REST endpoints

### Deployment
- Kubernetes manifests in `deploy/` (Deployment, Service, Route, PVC)
- SQLite DB persisted via PVC
- Tekton CI pipeline in `its/pipeline.yaml`
