package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/quay/release-readiness/internal/db/sqlc"
	_ "modernc.org/sqlite"
)

//go:generate sqlc generate -f ../../sqlc.yaml

type DB struct {
	conn *sql.DB
	dbtx dbsqlc.DBTX
}

func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode%%3DWAL&_pragma=foreign_keys%%3DON&_pragma=busy_timeout%%3D5000&_pragma=synchronous%%3DNORMAL", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db := &DB{conn: sqlDB, dbtx: sqlDB}
	if err := db.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) Ping() error {
	return d.conn.Ping()
}

// InTx runs fn inside a database transaction. The fn receives a tx-scoped *DB
// whose queries all run on the same transaction.
func (d *DB) InTx(ctx context.Context, fn func(*DB) error) error {
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	txDB := &DB{conn: d.conn, dbtx: tx}
	if err := fn(txDB); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) queries() *dbsqlc.Queries {
	return dbsqlc.New(d.dbtx)
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func parseOptionalTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
