# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Release readiness dashboard for Quay container registry. Tracks Konflux build snapshots and releases, and JIRA issues, across release versions. Full-stack Go backend + React SPA frontend with SQLite storage.

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
# Start backend (the kubeconfig needs read access to Snapshots and Releases in -namespace)
KUBECONFIG=<path> go run ./cmd/release-readiness -addr :8088 -db /tmp/rr.db

# In separate terminal, start frontend dev server
cd web && npm run dev
```

The Vite dev server proxies `/api` requests to `localhost:8088` (the Go backend).

## Architecture

### Backend (`internal/`)
- **`cmd/release-readiness/main.go`** — CLI entry point. Runs background sync loops for Konflux and JIRA.
- **`internal/server/`** — HTTP server using Go stdlib `net/http`. Routes registered in `routes.go`, API handlers in `handlers_api.go`. The React SPA is served from embedded `web/dist/` via `go:embed` with SPA fallback routing.
- **`internal/db/`** — SQLite data layer (pure-Go driver `modernc.org/sqlite`, no CGO). Schema migrations in `migrations.go`. WAL mode enabled.
- **`internal/kube/`** — Kubernetes dynamic client. Syncs Konflux Snapshots and Releases from `-namespace` (kubeconfig or in-cluster service account); `created_at` comes from `creationTimestamp`.
- **`internal/jira/`** — JIRA REST API client. Discovers active releases, syncs issues by fixVersion.
- **`internal/prow/`** — Prow periodic CI runs from public GCS, keyed by Konflux application (`-prow-jobs job=quay-3-18`). A run tests a Snapshot component only on an exact (role, digest) match (`ComponentKey`).
- **`internal/model/`** — Shared data types used across packages.

### Frontend (`web/`)
- React 19 + TypeScript, built with Vite 6
- UI framework: **PatternFly 6** (Red Hat design system)
- API client in `web/src/api/client.ts`, types in `web/src/api/types.ts`
- Pages: `ReleasesOverview`, `ReleaseDetail`

### Data Flow
1. Konflux sync loop lists Snapshots and Releases in the namespace → ingests into SQLite (snapshot components, releases). Test results are not ingested.
2. JIRA sync loop discovers active releases → syncs issues per fixVersion into SQLite
3. React SPA fetches data via `/api/v1/` REST endpoints

### Deployment
- Kubernetes manifests in `deploy/` (Deployment, Service, Route, PVC, RBAC). `rbac.yaml` grants the `release-readiness` ServiceAccount read on Snapshots and Releases; its Role and RoleBinding must live in the Konflux namespace the app reads.
- SQLite DB persisted via PVC. Delete the old DB file when upgrading across the move to the Kubernetes source.
