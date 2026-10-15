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
    fix_version TEXT NOT NULL DEFAULT '',
    issue_type  TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT '',
    link        TEXT NOT NULL DEFAULT ''
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
    completion_time TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_konflux_releases_application_created ON konflux_releases(application, created_at);

-- The ART shipment MR of a Quay version in GitLab, found by its ocp-build-data
-- pointer or by searching the shipment project. labels is a JSON array.
CREATE TABLE IF NOT EXISTS shipment_mrs (
    version               TEXT PRIMARY KEY,
    assembly              TEXT NOT NULL,
    iid                   INTEGER NOT NULL,
    web_url               TEXT NOT NULL,
    state                 TEXT NOT NULL,
    draft                 INTEGER NOT NULL DEFAULT 0,
    labels                TEXT NOT NULL DEFAULT '[]',
    head_pipeline_status  TEXT NOT NULL DEFAULT '',
    detailed_merge_status TEXT NOT NULL DEFAULT '',
    merged_at             TEXT NOT NULL DEFAULT '',
    updated_at            TEXT NOT NULL DEFAULT '',
    source                TEXT NOT NULL CHECK (source IN ('pointer', 'search')),
    checked_at            TEXT NOT NULL
);

-- The ART job and advisory of a Konflux Release, by konflux_releases.name.
CREATE TABLE IF NOT EXISTS konflux_release_art (
    name     TEXT PRIMARY KEY,
    job_url  TEXT NOT NULL DEFAULT '',
    advisory TEXT NOT NULL DEFAULT ''
);

-- ART build history record behind a component image, keyed by its
-- sha256:... digest. A resolved row is immutable; an unresolved one is retried.
CREATE TABLE IF NOT EXISTS art_builds (
    digest        TEXT PRIMARY KEY,
    state         TEXT NOT NULL CHECK (state IN ('resolved', 'unresolved')),
    nvr           TEXT NOT NULL DEFAULT '',
    record_id     TEXT NOT NULL DEFAULT '',
    checked_at    TEXT NOT NULL
);

-- Successful ART build history records of FBC, bundle and image builds;
-- pending and failed records are never stored. commitish is the source
-- commit ART built from. start_time is as ART gives it.
CREATE TABLE IF NOT EXISTS art_history_records (
    record_id          TEXT PRIMARY KEY,
    group_name         TEXT NOT NULL,
    build_type         TEXT NOT NULL,
    nvr                TEXT NOT NULL,
    digest             TEXT NOT NULL,
    pullspec           TEXT NOT NULL,
    commitish          TEXT NOT NULL DEFAULT '',
    start_time         TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_art_history_records_nvr ON art_history_records(group_name, build_type, nvr);
CREATE INDEX IF NOT EXISTS idx_art_history_records_digest ON art_history_records(digest);

-- Name and public source repository of an ART history record, as its full
-- /build record gives them. source_repo '' marks a record checked that names
-- none, so it is not fetched again.
CREATE TABLE IF NOT EXISTS art_source_repos (
    record_id    TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    source_repo  TEXT NOT NULL
);

-- Days of ART build history whose successful builds are all stored. A day
-- whose listing was capped or failed is not.
CREATE TABLE IF NOT EXISTS art_history_record_days (
    group_name  TEXT NOT NULL,
    build_type  TEXT NOT NULL,
    day         TEXT NOT NULL,
    checked_at  TEXT NOT NULL,
    PRIMARY KEY (group_name, build_type, day)
);

CREATE TABLE IF NOT EXISTS prow_runs (
    job_name        TEXT NOT NULL,
    build_id        TEXT NOT NULL,
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

-- A run whose Quay install step failed after its quay-operator Subscription
-- was created.
CREATE TABLE IF NOT EXISTS prow_install_failures (
    job_name TEXT NOT NULL,
    build_id TEXT NOT NULL,
    PRIMARY KEY (job_name, build_id),
    FOREIGN KEY (job_name, build_id) REFERENCES prow_runs(job_name, build_id) ON DELETE CASCADE
);

-- ART assembly annotations (art.redhat.com/assembly, kind, env) of a Konflux
-- Snapshot, for Snapshots that carry all three.
CREATE TABLE IF NOT EXISTS staged_snapshots (
    name       TEXT PRIMARY KEY,
    assembly   TEXT NOT NULL,
    kind       TEXT NOT NULL,
    env        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_staged_snapshots_assembly ON staged_snapshots(assembly, env, kind);

-- The release channel head of an FBC image, and the entry below it by
-- version, read from its registry by digest. A rendered row is final; a
-- failed one, with its error, is retried after retry_after. label_nvr is the
-- image's com.redhat.art.nvr label, its identity when ART build history has
-- no record of it.
CREATE TABLE IF NOT EXISTS fbc_renders (
    digest                   TEXT PRIMARY KEY,
    state                    TEXT NOT NULL CHECK (state IN ('rendered', 'failed')),
    label_nvr                TEXT NOT NULL DEFAULT '',
    channel                  TEXT NOT NULL DEFAULT '',
    head_csv                 TEXT NOT NULL DEFAULT '',
    head_bundle_pullspec     TEXT NOT NULL DEFAULT '',
    previous_version         TEXT NOT NULL DEFAULT '',
    error                    TEXT NOT NULL DEFAULT '',
    rendered_at              TEXT NOT NULL,
    retry_after              TEXT NOT NULL DEFAULT ''
);

-- The relatedImages of a rendered FBC image's head bundle and of its
-- previous entry's bundle.
CREATE TABLE IF NOT EXISTS fbc_related_images (
    digest TEXT NOT NULL,
    role   TEXT NOT NULL CHECK (role IN ('head', 'previous')),
    name   TEXT NOT NULL,
    image  TEXT NOT NULL,
    PRIMARY KEY (digest, role, name, image)
);

-- CVE scan of a candidate image, keyed by its sha256:... digest, read from the
-- scan TaskRuns of the PipelineRun that built it. A pending row is retried;
-- the other states are final. counts_json and reports_json are NULL unless
-- scanned; reports_json maps each architecture manifest digest to its report.
CREATE TABLE IF NOT EXISTS image_scans (
    digest       TEXT PRIMARY KEY,
    state        TEXT NOT NULL CHECK (state IN ('pending', 'scanned', 'scan_failed', 'not_scanned')),
    pipeline_run TEXT NOT NULL DEFAULT '',
    detail_url   TEXT NOT NULL DEFAULT '',
    counts_json  TEXT,
    reports_json TEXT,
    checked_at   TEXT NOT NULL
);

-- Last successful pass of each background sync, so freshness survives a
-- restart.
CREATE TABLE IF NOT EXISTS sync_status (
    source       TEXT PRIMARY KEY,
    last_success TIMESTAMP NOT NULL
);

-- The first FBC each stream/operator/OCP slot was reconciled from. A row is
-- never overwritten.
CREATE TABLE IF NOT EXISTS reconcile_baselines (
    stream      TEXT NOT NULL,
    operator    TEXT NOT NULL,
    ocp         TEXT NOT NULL,
    fbc_digest  TEXT NOT NULL,
    PRIMARY KEY (stream, operator, ocp)
);

-- What the reconciler decided for each issue an FBC's STAGE Release fixes,
-- with its commit evidence. The reconciler never replaces a proposed row;
-- the JIRA writer moves it to applied, error or skipped, storing each step's
-- result so a retry redoes only the steps not done.
CREATE TABLE IF NOT EXISTS reconcile_actions (
    fbc_digest              TEXT NOT NULL,
    issue_key               TEXT NOT NULL,
    release_name            TEXT NOT NULL,
    assembly                TEXT NOT NULL,
    fbc_nvr                 TEXT NOT NULL DEFAULT '',
    evidence_json           TEXT NOT NULL DEFAULT '',
    desired_target_version  TEXT NOT NULL DEFAULT '',
    status                  TEXT NOT NULL,
    skip_reason             TEXT NOT NULL DEFAULT '',
    error                   TEXT NOT NULL DEFAULT '',
    version_step            TEXT NOT NULL DEFAULT '',
    transition_step         TEXT NOT NULL DEFAULT '',
    comment_step            TEXT NOT NULL DEFAULT '',
    attempts                INTEGER NOT NULL DEFAULT 0,
    updated_at              TEXT NOT NULL,
    PRIMARY KEY (fbc_digest, issue_key, release_name)
);

CREATE INDEX IF NOT EXISTS idx_reconcile_actions_issue ON reconcile_actions(issue_key);

-- Each STAGE FBC the reconciler is done with.
CREATE TABLE IF NOT EXISTS reconcile_runs (
    release_name TEXT NOT NULL,
    fbc_digest  TEXT NOT NULL,
    PRIMARY KEY (release_name, fbc_digest)
);
