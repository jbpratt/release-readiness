-- name: UpsertSyncStatus :exec
INSERT INTO sync_status (source, last_success)
VALUES (?, ?)
ON CONFLICT(source) DO UPDATE SET last_success = excluded.last_success;

-- name: ListSyncStatus :many
SELECT source, last_success FROM sync_status;
