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
```

### Frontend (web/)
```bash
cd web && npm install                            # Install dependencies
cd web && npm run dev                            # Vite dev server (proxies /api to :8088)
cd web && npm run build                          # Production build (output: web/dist/)
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
