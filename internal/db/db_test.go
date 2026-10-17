package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMigrateFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	openClose(t, path)
	n := len(migrations)
	if got := userVersion(t, path); got != n {
		t.Fatalf("user_version = %d, want %d", got, n)
	}

	setMigrations(t, applied(t, n)...)
	openClose(t, path)
	if got := userVersion(t, path); got != n {
		t.Fatalf("user_version after reopen = %d, want %d", got, n)
	}
}

func TestMigrateLegacy(t *testing.T) {
	path := legacyDB(t)
	for range 2 {
		d, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		checkSeeded(t, d)
		_ = d.Close()
		if got := userVersion(t, path); got != len(migrations) {
			t.Fatalf("user_version = %d, want %d", got, len(migrations))
		}
	}
}

func TestMigrateForward(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current.db")
	openClose(t, path)

	n := len(migrations)
	setMigrations(t, append(applied(t, n), func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE extra (x TEXT)`)
		return err
	})...)
	openClose(t, path)
	if got := userVersion(t, path); got != n+1 {
		t.Fatalf("user_version = %d, want %d", got, n+1)
	}
	if !tableExists(t, path, "extra") {
		t.Fatal("table extra missing after the appended migration")
	}
}

func TestMigrateRollback(t *testing.T) {
	path := legacyDB(t)
	openClose(t, path)

	orig := migrations
	n := len(orig)
	setMigrations(t, append(slices.Clone(orig), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`CREATE TABLE extra (x TEXT)`); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO no_such_table VALUES (1)`)
		return err
	})...)
	d, err := Open(path)
	if err == nil {
		_ = d.Close()
		t.Fatal("Open succeeded, want the appended migration to fail")
	}
	if want := fmt.Sprintf("migration %d:", n+1); !strings.Contains(err.Error(), want) {
		t.Fatalf("Open error = %v, want it to contain %q", err, want)
	}
	if got := userVersion(t, path); got != n {
		t.Fatalf("user_version = %d, want %d", got, n)
	}
	if tableExists(t, path, "extra") {
		t.Fatal("table extra present after rolled back migration")
	}

	migrations = orig
	d, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	checkSeeded(t, d)
}

func TestOpenNewerDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newer.db")
	openClose(t, path)
	execRaw(t, path, `PRAGMA user_version = 99`)

	d, err := Open(path)
	if err == nil {
		_ = d.Close()
		t.Fatal("Open succeeded on a DB newer than the binary")
	}
}

// setMigrations replaces the migration list for the rest of the test.
func setMigrations(t *testing.T, ms ...func(*sql.Tx) error) {
	t.Helper()
	orig := migrations
	migrations = ms
	t.Cleanup(func() { migrations = orig })
}

// applied stands in for n migrations a DB already has: each fails the test
// if it runs again.
func applied(t *testing.T, n int) []func(*sql.Tx) error {
	ms := make([]func(*sql.Tx) error, n)
	for i := range ms {
		ms[i] = func(*sql.Tx) error {
			t.Errorf("applied migration %d ran again", i+1)
			return nil
		}
	}
	return ms
}

func openClose(t *testing.T, path string) {
	t.Helper()
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
}

// legacyDB loads testdata/legacy.sql, a DB from before user_version was
// tracked, into a new file.
func legacyDB(t *testing.T) string {
	t.Helper()
	fixture, err := os.ReadFile("testdata/legacy.sql")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "legacy.db")
	execRaw(t, path, string(fixture))
	return path
}

// rawDB opens path without migrating it.
func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func execRaw(t *testing.T, path, query string) {
	t.Helper()
	conn := rawDB(t, path)
	if _, err := conn.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func userVersion(t *testing.T, path string) int {
	t.Helper()
	conn := rawDB(t, path)
	var v int
	if err := conn.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func tableExists(t *testing.T, path, name string) bool {
	t.Helper()
	conn := rawDB(t, path)
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// checkSeeded reads back every row testdata/legacy.sql seeds.
func checkSeeded(t *testing.T, d *DB) {
	t.Helper()
	ctx := t.Context()

	issues, err := d.ListJiraIssues(ctx, "quay-v3.18.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Key != "PROJQUAY-1" || issues[0].Status != "" {
		t.Fatalf("ListJiraIssues = %+v, want PROJQUAY-1 with empty status", issues)
	}

	rv, err := d.GetReleaseVersion(ctx, "quay-v3.18.1")
	if err != nil {
		t.Fatal(err)
	}
	if rv == nil || rv.KonfluxApplication != "quay-3-18" {
		t.Fatalf("GetReleaseVersion = %+v, want quay-3-18", rv)
	}

	digest, ok, err := d.GetBaseline(ctx, "3.18", "quay-operator", "4.20")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || digest != "sha256:base" {
		t.Fatalf("GetBaseline = %q, %v, want sha256:base", digest, ok)
	}

	actions, err := d.PendingActions(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Key.IssueKey != "PROJQUAY-1" || actions[0].Key.FBCDigest != "sha256:fbc" {
		t.Fatalf("PendingActions = %+v, want PROJQUAY-1 on sha256:fbc", actions)
	}

	done, err := d.RunDone(ctx, "stage-1", "sha256:fbc")
	if err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Fatal("RunDone = false, want the seeded run")
	}
}
