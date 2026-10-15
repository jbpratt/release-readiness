# Release Readiness Dashboard

Tracks Konflux build snapshots and releases, and JIRA issues, across Quay release versions.

## Architecture

```mermaid
flowchart LR
    Konflux[Konflux namespace] -->|Snapshots + Releases| App[Release Readiness]
    JIRA -->|JIRA sync loop| App
    App -->|SQLite| DB[(SQLite)]
    App -->|serves| SPA[React SPA]
```

The Go backend runs background sync loops that pull data into a local SQLite database. The React SPA is embedded into the binary and served directly by the backend.

## Syncing

### Konflux sync (default: every 30s)

Lists `Snapshot` and `Release` resources (`appstudio.redhat.com/v1alpha1`) in the `-namespace` Konflux namespace through the Kubernetes API, using `-kubeconfig`/`KUBECONFIG` or, when unset, the in-cluster service account. New snapshots are stored with their component images; releases are upserted. Each record's `created_at` is the resource's `creationTimestamp`.

### JIRA sync (default: every 5m)

Discovers active releases by querying for JIRA issues with the `-area/release` component that are not Closed/Done. Parses the version from the ticket summary (e.g. "Release Quay v3.16.2") and syncs all issues whose Target Version equals the release version (e.g. `quay-v3.16.2`).

### Prow sync (default: every 15m, only when jobs or a job pattern are configured)

Lists the runs of each `-prow-jobs` periodic, and of each job `-prow-job-pattern` matches among the job directories listed each pass, in the public `test-platform-results-public` GCS bucket and stores each run's state, Prow link and, once the run has finished, the images its `tested-images.json` names or, for a run without one (before 2026-10-09), the quay-operator, quay, clair, quay-builder and quay-builder-qemu images of the quay-operator CSV in its `olm-csvs.yaml`, the CSVs installed on the test cluster, each only when that CSV names a single digest of it. For a finished run that did not pass and whose `tested-images.json` does not list `subscription` as missing, it also reads the `finished.json` of the target's Quay install step (`quay-deploy-aws-s3`, `-odf`, `-azure-blob` or `-gcp-gcs`) and stores whether that step failed. A finished run is read once. A job with no stored runs is read newest first and stops at its first run that started before `-retention`, so a long-idle job does not backfill its history. A job maps to a Konflux application, so every z-stream of a minor (`quay-v3.18.1`, `quay-v3.18.2`) shows the same runs: a pattern-matched job's is `quay-<major>-<minor>` from the pattern's named groups (`redhat-3.18` -> `quay-3-18`), and a `-prow-jobs` entry for the same job overrides it. The deployment discovers the `periodic-ci-quay-quay-redhat-X.Y-...-nightly` install periodics by pattern.

A run's `tested-images.json` also records the FBC catalog it installed from (`catalog.resolved_ref`); the release candidate counts a run for the FBC of that digest. An `olm-csvs.yaml` run records no catalog and counts for no FBC.

### ART build history sync (every 10m, with `-art-build-history-url`)

For each Quay stream with a stored Snapshot (ART group `quay-X.Y`), lists ART's successful FBC builds day by day: the newest 3 days every pass and up to 7 older unscanned days, back 120 days. An FBC is stored once the bundle it holds and the operator and operand images that bundle deploys are stored. A day listing at the 1000-row search cap is split by build name, the operators' FBC names and any the listing shows; a name that alone reaches the cap, or a search warning, is a sync error and the day is scanned again.

### FBC render (every 10m, with `-art-build-history-url`)

Reads up to 20 FBC images a pass anonymously from their registry (Registry v2 API): the catalogs of staged FBC Snapshots, then the newest ART FBC build of each operator, version and OCP version. It keeps, by digest, the image's `com.redhat.art.nvr` label and the head bundle of its `stable-X.Y` channel; a failed read is retried after an hour. Each head bundle is joined to its ART bundle build, read from ART build history when not yet stored.

### FBC JIRA reconciler (every 10m, with `-art-build-history-url`)

For each successful STAGE Release of an ART-staged FBC Snapshot, in a stream the allowlist in `internal/reconcile/config.go` lists, finds the PROJQUAY issues the FBC fixes and stores them as actions for the JIRA writer. FBCs are grouped by stream, operator and OCP version; the newest FBC of a group when first seen is its baseline and proposes nothing. Each later FBC is compared with the one before: for each member image whose digest changed and whose ART build is an allowlisted component from its listed repository, the commits between the two builds' commits on the stream's branch are read from the GitHub compare API, and each PROJQUAY key in their messages is `proposed` for `quay-vX.Y.Z`, or `skipped` when an earlier FBC of the stream already included it. Anything that keeps an FBC's fixes from being known (such as a component not allowlisted, a GitHub error other than a rate limit, 5xx or network error, a revert of a key, a missing ART source, an FBC rendered after a later one was reconciled) is logged as the FBC's uncertainty instead. An FBC with no group (not one ART name, no OCP version in its NVR, or a failed render) proposes nothing and is logged as uncertain, and so is the next FBC from a later Release in each group of its stream, whose diff would take its fixes. An FBC whose catalog is not rendered yet, or whose ART source records are not yet checked, or not yet stored within 6 hours of its Release completing, or whose GitHub read hit a rate limit, 5xx or network error, waits and is retried next pass, and so do the FBCs after it. `-github-token` reads GitHub authenticated; without it the 60-requests-an-hour anonymous limit applies. Actions are not pruned by `-retention`. Its sync status is `reconcile`.

### FBC JIRA writer (off until `-jira-write`)

After each reconcile pass, applies the proposed actions to their PROJQUAY issues, re-reading each issue before every step: replaces the stream's `quay-vX.Y.z` Target Version with the release's `quay-vX.Y.Z` (keeping every other Target Version, and skipping the issue when that version is missing, archived or released), moves a Verified issue to Release Pending (an issue already there, e.g. moved by release-tool, is never transitioned), then comments the FBC NVR, digest and commits once, found again by a marker. Each step's result is stored on the action, so a failed one is retried alone, up to 5 attempts. It stays off until `-jira-write` is true and its own credential (`-jira-write-email`, `-jira-write-token`), never the read one, is set. Setting `-jira-write` false is the clean stop; revoking that token also stops every write without touching the read sync, but each pass then spends an attempt of every pending action, which stays in error after 5 passes.

### Sync status

`/api/v1/sync-status` lists as `problems` each enabled sync (`konflux`, `jira`, `art-builds`, `art-history`, `fbc-render`, `reconcile`, `catalog`, `prow`, `image-scans`, `shipment-mrs`) whose last pass failed or that has had no success within 3 poll intervals (at least 10 minutes), and as `sources` every enabled sync with its `last_success`, null before its first success; the header shows a warning icon while any problems exist, except on the release list and a release page, whose freshness lines show them (image-scans only while CVEs are shown).

## Release view

Each JIRA release maps to a Konflux application by major.minor version: fixVersion `quay-v3.16.2` maps to `quay-3-16`, and `omr-v2.0.10` to `omr-2-0`.

A Quay version's builds are its application's stream Snapshots (not ART's assemblies or `fbc-ri-*` re-releases) whose images, base image excluded, carry that version in the NVR ART build history resolved for them: `quay-quay-container-3.18.1-...` is 3.18.1. Unresolved images do not count, a build carrying two versions, as while ART switches the stream, is of none, and a build counts only if ART built no lower version after it.

`/api/v1/releases/{version}/candidate` returns the version's operator FBCs up for release in `fbc_candidate`, and as its stream built them in `fbc_stream`; both are null while the version has no FBC built or staged. `fbc_candidate` takes each operator and OCP version slot's FBC from the newest FBC Snapshot ART staged for stage (`source` `stage_snapshots`), else from the newest successful ART build (`stream`). An operator's slots are the OCP versions any of its FBCs of the version was built or staged for. `operators` always holds quay-operator, container-security-operator and quay-bridge-operator, each with the `head_bundle_nvr`, `channel` and `head_csv` of its newest FBC whose head bundle is joined, its slots' FBCs on that bundle in `members` and the others in `conflicts`, the slots without an FBC in `missing_ocp`, and `coverage`. Its `fbc_ri_stage` is `in` when a stage Release holding its head bundle succeeded, an `fbc-ri-*` Snapshot's or the version's image shipment's, else `failed` when one failed, else `none`, or `unknown` without a head bundle; `image_check` compares its head bundle with that of the version's STAGE build, else its newest build: `matches`, `differs`, or `absent` when either is unknown. `stage` counts its `members` whose newest stage Release succeeded, and, on `fbc_candidate`, each FBC's `staged` counts the staged FBC Snapshots of its slot whose newest Release succeeded. `images` lists its head bundle's relatedImages, by name, from its newest-OCP member FBC that was read, each with its ART build's `nvr`, `commit` and `build_url` (its ART build page), empty for an image ART did not build, and `stage` or `prod` set when a succeeded stage or prod Release holds its digest, and its CVE `scan`, null until the scan sync reads it. `compare` compares them with its FBC's previous channel entry (`previous`) and, on `fbc_candidate`, with the stream's head bundle when that is another (`newer`); each row is `same`, `rebuild` (same commit), `new source`, `new build` (a commit unknown), `added` or `removed`. `state` is `inconsistent` when an operator's FBCs head more than one bundle, `incomplete` when an operator or slot has no FBC, `unknown` while a head bundle or a staged catalog's identity is unresolved (staged catalogs no NVR names are in `unknown`), else `complete`; `errors` names each operator missing or heading more than one bundle. `pullspecs`, release-from-fbc's fbc-pullspecs by operator, then OCP version, is empty unless `complete`. `reading` counts the shown FBCs' reads as `read`, `failed` and `pending`. `verdict.state` is `shipped` for a shipped version, whose `fbc_candidate` is the record of what shipped, with no `missing_ocp` and no `pullspecs`; else `checking` while a read is pending, `not_ready` with a blocker, or `ready`. `verdict.items` lists blockers, then warnings, then info, each with its `operator` (empty for the release), `owner` (`ART`, `ART/Konflux`, `Quay (release-from-fbc)`, `Quay/ART` or empty), `code` and `text`. Blockers: `not_staged` on a stream candidate; `missing_operator`, `missing_ocp`, `read_failed`, `no_head_bundle`, `stage_release_failed` and `unknown_fbc` on a staged one; `csv_mismatch`, `mixed_builds`, `image_mismatch` and `stage_push_failed`, when an operator's `fbc_ri_stage` is `failed`, on either; `stream_ahead` is info when the stream's newer bundle only rebuilds images, else a warning. Each FBC's `ci` lists, by job, the periodic runs that installed from its digest (`exact`) and from another FBC of its operator, OCP version and head bundle (`equivalent`). `candidate` is the version's selected STAGE build with its `prod` Release, or null with the `reason`, e.g. `no release candidate yet`, or, for a version without a build while ART's newest build whose version resolves is of a lower one, `no 3.18.2 builds yet: ART still builds 3.18.1`. `catalogs` lists the version's FBC catalogs: each of its assembly's stage FBC Snapshots, `staged`, with its `snapshot`, `pullspec`, newest `release` and `release_plan`, then, for each operator and OCP version, the newest version-matched stream catalog ART did not stage, `built`. `unshipped_builds` lists, with a candidate, the newest image of the version of each component its stage Snapshot leaves out, by component, and is null without one. On `/api/v1/releases/overview`, a version's `latest_build` is when the newer of its newest build and its STAGE build was created, omitted when it has neither.

`/api/v1/releases/{version}/issues` lists the version's Target Version tickets, by key. Each has its Jira `status` as of the last JIRA sync; a `dashboard.db` from before that field must be deleted on upgrade.

`/api/v1/releases/{version}/ci` lists the version's ART FBC builds, newest first, each with its `state` (`shipped`, else `candidate`, else `staged`, else empty) and the periodic runs that installed from its catalog digest.

## Running the application

### Build

```bash
# Backend
go build -o release-readiness ./cmd/release-readiness/

# Frontend
cd web && npm install && npm run build
```

### CLI flags

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `-addr` | — | `:8080` | Listen address |
| `-db` | — | `dashboard.db` | SQLite database path |
| `-kubeconfig` | `KUBECONFIG` | — | Kubeconfig path (empty uses the in-cluster service account) |
| `-namespace` | `KONFLUX_NAMESPACE` | `art-quay-tenant` | Konflux namespace to read Snapshots and Releases from |
| `-konflux-poll-interval` | — | `30s` | Konflux sync poll interval |
| `-konflux-ui-url` | `KONFLUX_UI_URL` | `https://konflux-ui.apps.kflux-ocp-p01.7ayg.p1.openshiftapps.com` | Konflux UI URL for links to `-namespace`'s applications, Snapshots and Releases (empty disables Konflux links) |
| `-art-build-history-url` | — | `https://art-build-history-art-build-history.apps.artc2023.pc3z.p1.openshiftapps.com` | ART build history service; component images are looked up there in the background for build links, and FBC builds synced and rendered for the FBC candidate (empty disables all of them) |
| `-tekton-results-url` | — | `https://tekton-results-tekton-results.apps.kflux-ocp-p01.7ayg.p1.openshiftapps.com` | Tekton Results API; the CVE scans of candidate images are read there in the background with the Konflux credential (empty disables) |
| `-jira-url` | `JIRA_URL` | `https://redhat.atlassian.net` | JIRA Cloud URL |
| `-jira-api-url` | `JIRA_API_URL` | `-jira-url` | JIRA REST API base URL; set to `https://api.atlassian.com/ex/jira/<cloudId>` for scoped API tokens |
| `-jira-email` | `JIRA_EMAIL` | — | JIRA Cloud account email for API token auth |
| `-jira-token` | `JIRA_TOKEN` | — | JIRA Cloud API token (required to enable JIRA sync) |
| `-jira-project` | `JIRA_PROJECT` | `PROJQUAY` | JIRA project key |
| `-jira-poll-interval` | — | `5m` | JIRA sync poll interval |
| `-github-token` | `GITHUB_TOKEN` | — | Read-only GitHub token for the reconciler's commit range lookups (empty reads GitHub unauthenticated) |
| `-jira-write` | `JIRA_WRITE` | `false` | Write the reconciler's proposed actions to JIRA when `true`; needs `-jira-write-email` and `-jira-write-token`, else stays off |
| `-jira-write-email` | `JIRA_WRITE_EMAIL` | — | JIRA Cloud account email of the write credential |
| `-jira-write-token` | `JIRA_WRITE_TOKEN` | — | JIRA Cloud API token of the write credential, separate from `-jira-token` |
| `-stage-release-plan-pattern` | `STAGE_RELEASE_PLAN_PATTERN` | `^quay-advisory-stage-\d+-\d+$` | Regexp matching the Konflux ReleasePlan names whose successful Releases are image STAGE builds (empty selects no staged build) |
| `-prow-jobs` | `PROW_JOBS` | — | Periodic Prow jobs to ingest, as `job_name=konflux_application[,...]`, e.g. `periodic-ci-quay-quay-redhat-3.18-aws-ocp422-e2e-install-aws-s3-nightly=quay-3-18` |
| `-prow-job-pattern` | `PROW_JOB_PATTERN` | — | `^`-anchored regexp of periodic Prow job names to discover each pass, with named groups `major` and `minor` that give the application `quay-<major>-<minor>`, e.g. `^periodic-ci-quay-quay-redhat-(?P<major>\d+)\.(?P<minor>\d+)-.+-nightly(-.+)?$`; a `-prow-jobs` entry for the same job wins |
| `-prow-interval` | — | `15m` | Prow sync poll interval |
| `-retention` | — | `720h` | Age past which data no release needs is pruned daily and not ingested: Snapshots not ART staged, shipped or the newest of their application, Failed Releases not of an ART staged Snapshot, Prow runs and unreferenced image caches (0 disables) |
| `-catalog-url` | — | `https://catalog.redhat.com/api/containers/v1` | Red Hat container catalog API, checked hourly in the background to mark versions shipped (empty leaves JIRA's released flag as the only shipped signal) |
| `-gitlab-url` | — | `https://gitlab.cee.redhat.com` | GitLab holding ART's shipment MRs, polled for each unshipped version and each still-open MR (empty disables) |
| `-gitlab-token` | `GITLAB_TOKEN` | — | GitLab `read_api` token (empty reads anonymously, throttled) |
| `-shipment-project` | — | `116177` | GitLab project ID of ART's shipment MRs (ocp-shipment-data) |
| `-ocp-build-data-url` | — | `https://raw.githubusercontent.com/openshift-eng/ocp-build-data` | Raw ocp-build-data URL, whose `quay-X.Y` `releases.yml` names each assembly's shipment MR |
| `-gitlab-poll-interval` | — | `5m` | Shipment MR sync poll interval |

### Local development

```bash
# Start backend (the kubeconfig needs read access to Snapshots and Releases in the namespace,
# and to its Tekton Results records while -tekton-results-url is set)
KUBECONFIG=<path> go run ./cmd/release-readiness -addr :8088 -db /tmp/rr.db

# In a separate terminal, start the Vite dev server (proxies /api to localhost:8088)
cd web && npm run dev
```

### Deployment

`deploy/rbac.yaml` creates the `release-readiness` ServiceAccount and a Role granting read-only access to Snapshots and Releases, and get/list on `results.tekton.dev` results and records for the CVE scan counts. The Role and RoleBinding must live in the Konflux namespace the app reads.

### Upgrading

The schema changed when the Konflux source moved to Kubernetes. Delete the old SQLite database file before upgrading; it is recreated on startup.
