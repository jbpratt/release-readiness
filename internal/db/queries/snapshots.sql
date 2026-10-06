-- name: CreateSnapshot :execlastid
INSERT INTO snapshots (application, name, tests_passed, created_at)
VALUES (?, ?, ?, ?);

-- name: SnapshotExistsByName :one
SELECT COUNT(*) FROM snapshots WHERE name = ?;

-- name: GetSnapshotRow :one
SELECT id, application, name, tests_passed, created_at
FROM snapshots WHERE name = ?;

-- name: CreateSnapshotComponent :exec
INSERT INTO snapshot_components (snapshot_id, component, git_sha, image_url, git_url)
VALUES (?, ?, ?, ?, ?);

-- name: ListSnapshotComponents :many
SELECT id, snapshot_id, component, git_sha, image_url, git_url
FROM snapshot_components
WHERE snapshot_id = ?
ORDER BY component;

-- name: ListAllSnapshots :many
SELECT id, application, name, tests_passed, created_at
FROM snapshots
ORDER BY id DESC LIMIT ? OFFSET ?;

-- name: ListSnapshotsByApplications :many
SELECT id, application, name, tests_passed, created_at
FROM snapshots
WHERE application IN (sqlc.slice('applications'))
ORDER BY id DESC LIMIT ? OFFSET ?;

-- name: LatestSnapshotPerApplication :many
SELECT s.id, s.application, s.name, s.tests_passed, s.created_at, CAST(counts.cnt AS INTEGER) AS cnt,
       (SELECT COUNT(*) FROM test_suites WHERE snapshot_id = s.id) AS test_count
FROM snapshots s
JOIN (
    SELECT application, MAX(id) AS max_id, COUNT(*) AS cnt
    FROM snapshots
    GROUP BY application
) counts ON s.id = counts.max_id
ORDER BY s.application;

-- name: GetSnapshotByID :one
SELECT id, application, name, tests_passed, created_at
FROM snapshots WHERE id = ?;

-- name: GetTestSuiteByID :one
SELECT id, snapshot_id, name FROM test_suites WHERE id = ?;

-- name: CreateTestSuite :execlastid
INSERT INTO test_suites (snapshot_id, name, status, pipeline_run, tool_name, tool_version, tests, passed, failed, skipped, pending, other, flaky, start_time, stop_time, duration_ms)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: CreateTestCase :exec
INSERT INTO test_cases (test_suite_id, name, status, duration_ms, message, trace, file_path, suite, retries, flaky)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListTestSuitesBySnapshot :many
SELECT id, snapshot_id, name, status, pipeline_run, tool_name, tool_version, tests, passed, failed, skipped, pending, other, flaky, start_time, stop_time, duration_ms, created_at
FROM test_suites
WHERE snapshot_id = ?
ORDER BY name;

-- name: ListTestCasesBySuite :many
SELECT id, test_suite_id, name, status, duration_ms, message, trace, file_path, suite, retries, flaky
FROM test_cases
WHERE test_suite_id = ?
ORDER BY name;

-- name: CreateVulnerabilityReport :execlastid
INSERT INTO vulnerability_reports (snapshot_id, component, arch, total, critical, high, medium, low, unknown, fixable)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: CreateVulnerability :exec
INSERT INTO vulnerabilities (report_id, name, severity, package_name, package_version, fixed_in_version, description, link)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListVulnerabilityReportsBySnapshot :many
SELECT id, snapshot_id, component, arch, total, critical, high, medium, low, unknown, fixable, created_at
FROM vulnerability_reports
WHERE snapshot_id = ?
ORDER BY component, arch;

-- name: ListVulnerabilitiesByReport :many
SELECT id, report_id, name, severity, package_name, package_version, fixed_in_version, description, link
FROM vulnerabilities
WHERE report_id = ?
ORDER BY
    CASE severity
        WHEN 'Critical' THEN 0
        WHEN 'High' THEN 1
        WHEN 'Medium' THEN 2
        WHEN 'Low' THEN 3
        ELSE 4
    END,
    name;
