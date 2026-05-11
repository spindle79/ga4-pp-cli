// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

package cliutil

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newFreshnessDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "fresh.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`
		CREATE TABLE sync_state (
			property_id TEXT NOT NULL,
			scope       TEXT NOT NULL,
			last_date   TEXT,
			last_run_at TEXT NOT NULL,
			row_count   INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(property_id, scope)
		);
	`)
	if err != nil {
		t.Fatalf("create sync_state: %v", err)
	}
	return db
}

func insertSyncRow(t *testing.T, db *sql.DB, property, scope string, age time.Duration) {
	t.Helper()
	ts := time.Now().UTC().Add(-age).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO sync_state(property_id, scope, last_run_at) VALUES(?,?,?)`, property, scope, ts); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func TestEnsureFresh_MissingRowIsStale(t *testing.T) {
	db := newFreshnessDB(t)
	stale, _, err := EnsureFresh(context.Background(), db, "12345", "pages", 1*time.Hour)
	if err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if !stale {
		t.Fatalf("expected stale=true for missing sync_state row")
	}
}

func TestEnsureFresh_FreshRow(t *testing.T) {
	db := newFreshnessDB(t)
	insertSyncRow(t, db, "12345", "pages", 30*time.Minute)
	stale, last, err := EnsureFresh(context.Background(), db, "12345", "pages", 6*time.Hour)
	if err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if stale {
		t.Fatalf("expected stale=false for 30m-old row vs 6h maxAge")
	}
	if last.IsZero() {
		t.Fatalf("expected non-zero last time")
	}
}

func TestEnsureFresh_OldRow(t *testing.T) {
	db := newFreshnessDB(t)
	insertSyncRow(t, db, "12345", "pages", 48*time.Hour)
	stale, _, err := EnsureFresh(context.Background(), db, "12345", "pages", 24*time.Hour)
	if err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if !stale {
		t.Fatalf("expected stale=true for 48h-old row vs 24h maxAge")
	}
}

func TestEnsureFresh_DisabledMaxAge(t *testing.T) {
	db := newFreshnessDB(t)
	insertSyncRow(t, db, "12345", "pages", 48*time.Hour)
	stale, _, err := EnsureFresh(context.Background(), db, "12345", "pages", 0)
	if err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if stale {
		t.Fatalf("expected stale=false when maxAge=0 (check disabled)")
	}
}

func TestEnsureFresh_AnyProperty(t *testing.T) {
	db := newFreshnessDB(t)
	insertSyncRow(t, db, "99999", "properties", 10*time.Minute)
	// Asking with empty property looks up the most-recent row for the scope.
	stale, _, err := EnsureFresh(context.Background(), db, "", "properties", 1*time.Hour)
	if err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if stale {
		t.Fatalf("expected stale=false for recent row when property=\"\"")
	}
}

func TestEnsureFresh_NilDBErrors(t *testing.T) {
	if _, _, err := EnsureFresh(context.Background(), nil, "", "pages", time.Hour); err == nil {
		t.Fatalf("expected error for nil db")
	}
}

func TestEnsureFresh_EmptyScopeErrors(t *testing.T) {
	db := newFreshnessDB(t)
	if _, _, err := EnsureFresh(context.Background(), db, "", "", time.Hour); err == nil {
		t.Fatalf("expected error for empty scope")
	}
}
