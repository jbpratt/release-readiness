-- name: UpsertShipmentMR :exec
INSERT INTO shipment_mrs (version, assembly, iid, web_url, state, draft, labels, head_pipeline_status, detailed_merge_status, merged_at, updated_at, source, checked_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(version) DO UPDATE SET
    assembly=excluded.assembly,
    iid=excluded.iid,
    web_url=excluded.web_url,
    state=excluded.state,
    draft=excluded.draft,
    labels=excluded.labels,
    head_pipeline_status=excluded.head_pipeline_status,
    detailed_merge_status=excluded.detailed_merge_status,
    merged_at=excluded.merged_at,
    updated_at=excluded.updated_at,
    source=excluded.source,
    checked_at=excluded.checked_at;

-- name: GetShipmentMR :one
SELECT * FROM shipment_mrs WHERE version = ?;

-- name: ListShipmentMRs :many
SELECT * FROM shipment_mrs;
