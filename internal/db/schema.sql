CREATE TABLE IF NOT EXISTS snapshots (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    application  TEXT NOT NULL,
    name         TEXT NOT NULL UNIQUE,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

CREATE INDEX IF NOT EXISTS idx_snapshots_application ON snapshots(application);
CREATE INDEX IF NOT EXISTS idx_snapshots_created ON snapshots(created_at DESC);

CREATE TABLE IF NOT EXISTS snapshot_components (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    component   TEXT NOT NULL,
    image_url   TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_snapshot_components_snapshot ON snapshot_components(snapshot_id);

CREATE TABLE IF NOT EXISTS jira_issues (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    key         TEXT NOT NULL,
    summary     TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT '',
    priority    TEXT NOT NULL DEFAULT '',
    labels      TEXT NOT NULL DEFAULT '',
    fix_version TEXT NOT NULL DEFAULT '',
    assignee    TEXT NOT NULL DEFAULT '',
    issue_type  TEXT NOT NULL DEFAULT '',
    link        TEXT NOT NULL DEFAULT '',
    qa_contact  TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_jira_issues_key_version ON jira_issues(key, fix_version);

CREATE INDEX IF NOT EXISTS idx_jira_issues_fix_version ON jira_issues(fix_version);

CREATE TABLE IF NOT EXISTS release_versions (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    name               TEXT NOT NULL UNIQUE,
    release_date       TEXT NOT NULL DEFAULT '',
    released           INTEGER NOT NULL DEFAULT 0,
    archived           INTEGER NOT NULL DEFAULT 0,
    release_ticket_key      TEXT NOT NULL DEFAULT '',
    release_ticket_assignee TEXT NOT NULL DEFAULT '',
    konflux_application     TEXT NOT NULL DEFAULT '',
    due_date                TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS konflux_releases (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL UNIQUE,
    application     TEXT NOT NULL DEFAULT '',
    snapshot        TEXT NOT NULL DEFAULT '',
    release_plan    TEXT NOT NULL DEFAULT '',
    released_status TEXT NOT NULL DEFAULT '',
    released_reason TEXT NOT NULL DEFAULT '',
    -- Task and step of the last managed pipeline attempt when Released reason=Failed.
    failed_task     TEXT NOT NULL DEFAULT '',
    failed_step     TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    start_time      TEXT NOT NULL DEFAULT '',
    completion_time TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_konflux_releases_application_created ON konflux_releases(application, created_at);

-- ART build history record behind a component image, keyed by its
-- sha256:... digest. A resolved row is immutable; an unresolved one is retried.
CREATE TABLE IF NOT EXISTS art_builds (
    digest        TEXT PRIMARY KEY,
    state         TEXT NOT NULL CHECK (state IN ('resolved', 'unresolved')),
    nvr           TEXT NOT NULL DEFAULT '',
    record_id     TEXT NOT NULL DEFAULT '',
    upstream_repo TEXT NOT NULL DEFAULT '',
    upstream_sha  TEXT NOT NULL DEFAULT '',
    checked_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS prow_runs (
    job_name        TEXT NOT NULL,
    build_id        TEXT NOT NULL,
    -- Unused; still set on insert: old databases have it NOT NULL, no default.
    kind            TEXT NOT NULL,
    application     TEXT NOT NULL,
    state           TEXT NOT NULL DEFAULT '',
    started_at      TEXT NOT NULL DEFAULT '',
    completed_at    TEXT NOT NULL DEFAULT '',
    prow_url        TEXT NOT NULL DEFAULT '',
    artifact_state  TEXT NOT NULL DEFAULT 'missing',
    catalog_ref     TEXT NOT NULL DEFAULT '',
    fetched_at      TEXT NOT NULL,
    UNIQUE(job_name, build_id)
);

CREATE INDEX IF NOT EXISTS idx_prow_runs_application_started ON prow_runs(application, started_at);

CREATE TABLE IF NOT EXISTS prow_run_images (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    job_name      TEXT NOT NULL,
    build_id      TEXT NOT NULL,
    role          TEXT NOT NULL,
    digest        TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (job_name, build_id) REFERENCES prow_runs(job_name, build_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_prow_run_images_run ON prow_run_images(job_name, build_id);

CREATE TABLE IF NOT EXISTS prow_syncs (
    job_name             TEXT PRIMARY KEY,
    application          TEXT NOT NULL,
    interval_seconds     INTEGER NOT NULL,
    last_successful_sync TEXT NOT NULL
);

-- Newest running ART image build per NVR name and version in a group, as of
-- the group's last successful search. Pending builds have no image digest.
-- component is the NVR name, e.g. quay-quay-container.
CREATE TABLE IF NOT EXISTS art_pending_builds (
    group_name      TEXT NOT NULL,
    release_version TEXT NOT NULL,
    component       TEXT NOT NULL,
    nvr             TEXT NOT NULL,
    record_id       TEXT NOT NULL,
    upstream_sha    TEXT NOT NULL DEFAULT '',
    started_at      TEXT NOT NULL,
    checked_at      TEXT NOT NULL,
    PRIMARY KEY (group_name, release_version, component)
);

-- Every ART image-build record a group's searches returned. ART records a
-- finished build as another record of its NVR and leaves the pending one.
CREATE TABLE IF NOT EXISTS art_build_attempts (
    group_name      TEXT NOT NULL,
    record_id       TEXT NOT NULL,
    release_version TEXT NOT NULL,
    component       TEXT NOT NULL,
    nvr             TEXT NOT NULL,
    outcome         TEXT NOT NULL,
    start_time      TEXT NOT NULL,
    first_seen      TEXT NOT NULL,
    last_seen       TEXT NOT NULL,
    PRIMARY KEY (group_name, record_id)
);

CREATE INDEX IF NOT EXISTS idx_art_build_attempts_version ON art_build_attempts(release_version);

-- Span of build start times a group's searches read without a gap.
CREATE TABLE IF NOT EXISTS art_build_coverage (
    group_name   TEXT PRIMARY KEY,
    covered_from TEXT NOT NULL,
    covered_to   TEXT NOT NULL
);

-- File-based catalog read from an FBC image, keyed by its sha256:... digest.
-- A parsed row is immutable, even with no bundles; a failed one is retried.
CREATE TABLE IF NOT EXISTS fbc_catalogs (
    digest     TEXT PRIMARY KEY,
    state      TEXT NOT NULL CHECK (state IN ('parsed', 'failed')),
    checked_at TEXT NOT NULL
);

-- Channel entries of a parsed catalog with the bundle image each names.
-- bundle_digest is empty unless bundle_ref is pinned by sha256 digest.
CREATE TABLE IF NOT EXISTS fbc_catalog_bundles (
    catalog_digest TEXT NOT NULL,
    package        TEXT NOT NULL,
    channel        TEXT NOT NULL,
    bundle_name    TEXT NOT NULL,
    bundle_ref     TEXT NOT NULL,
    bundle_digest  TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (catalog_digest, channel, bundle_name)
);

-- ART assembly annotations (art.redhat.com/assembly, kind, env) of a Konflux
-- Snapshot, for Snapshots that carry all three.
CREATE TABLE IF NOT EXISTS staged_snapshots (
    name       TEXT PRIMARY KEY,
    assembly   TEXT NOT NULL,
    kind       TEXT NOT NULL,
    env        TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_staged_snapshots_assembly ON staged_snapshots(assembly, env, kind, created_at);
