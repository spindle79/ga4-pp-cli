// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

package store

import (
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "data.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenAppliesSchema(t *testing.T) {
	s := tempStore(t)
	v, err := s.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != StoreSchemaVersion {
		t.Fatalf("user_version = %d, want %d", v, StoreSchemaVersion)
	}

	// Spot-check tables exist by selecting from each.
	for _, table := range []string{"properties", "dimensions", "metrics", "pages_daily", "sync_state"} {
		var n int
		if err := s.DB().QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("count from %s: %v", table, err)
		}
	}
}

func TestUpsertDimensionsAndSearch(t *testing.T) {
	s := tempStore(t)
	property := "12345"

	entries := []SchemaEntry{
		{APIName: "pagePath", UIName: "Page path", Description: "The URL path of the page"},
		{APIName: "sessionSource", UIName: "Session source", Description: "Source of the session"},
		{APIName: "country", UIName: "Country", Description: "Country of the user"},
	}
	n, err := s.UpsertDimensions(property, entries)
	if err != nil {
		t.Fatalf("UpsertDimensions: %v", err)
	}
	if n != 3 {
		t.Fatalf("inserted %d, want 3", n)
	}

	// FTS search
	hits, err := s.Search(property, "session", 50)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected at least 1 hit for 'session'")
	}
	found := false
	for _, h := range hits {
		if h.Kind == "dimension" && h.APIName == "sessionSource" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected sessionSource in results, got %#v", hits)
	}

	// Re-upserting replaces — second batch with 2 entries should leave 2 rows.
	half := []SchemaEntry{{APIName: "pagePath", UIName: "x", Description: "x"}}
	if _, err := s.UpsertDimensions(property, half); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT count(*) FROM dimensions WHERE property_id = ?`, property).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("after re-upsert, want 1 row, got %d", count)
	}
}

func TestUpsertPagesDaily(t *testing.T) {
	s := tempStore(t)
	property := "12345"

	rows := []PageDaily{
		{PropertyID: property, Date: "2026-05-01", PagePath: "/", Sessions: 100, ScreenPageViews: 200, EngagedSessions: 60, TotalUsers: 80, EngagementRate: 0.6, AverageSessionDuration: 45.5, Conversions: 3},
		{PropertyID: property, Date: "2026-05-01", PagePath: "/about", Sessions: 50, ScreenPageViews: 80, EngagedSessions: 30, TotalUsers: 40, EngagementRate: 0.6, AverageSessionDuration: 50.0, Conversions: 1},
		{PropertyID: property, Date: "2026-05-02", PagePath: "/", Sessions: 110, ScreenPageViews: 210, EngagedSessions: 65, TotalUsers: 85, EngagementRate: 0.59, AverageSessionDuration: 46.0, Conversions: 4},
	}
	n, err := s.UpsertPagesDaily(rows)
	if err != nil {
		t.Fatalf("UpsertPagesDaily: %v", err)
	}
	if n != 3 {
		t.Fatalf("inserted %d, want 3", n)
	}

	got, err := s.PagesDailyRange(property, "2026-05-01", "2026-05-02")
	if err != nil {
		t.Fatalf("PagesDailyRange: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d rows, want 3", len(got))
	}

	// Conflict-update path: re-upserting a single row should change values but
	// keep the total count at 3.
	conflict := []PageDaily{{PropertyID: property, Date: "2026-05-01", PagePath: "/", Sessions: 999}}
	if _, err := s.UpsertPagesDaily(conflict); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT count(*) FROM pages_daily WHERE property_id = ?`, property).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Fatalf("after conflict update, want 3 rows, got %d", count)
	}
	var sessions float64
	if err := s.DB().QueryRow(`SELECT sessions FROM pages_daily WHERE property_id = ? AND date = '2026-05-01' AND page_path = '/'`, property).Scan(&sessions); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if sessions != 999 {
		t.Fatalf("sessions after conflict = %v, want 999", sessions)
	}

	// sync_state should reflect the latest pages run.
	state, err := s.GetSyncState(property, "pages")
	if err != nil {
		t.Fatalf("GetSyncState: %v", err)
	}
	if state == nil {
		t.Fatalf("expected sync_state for pages, got nil")
	}
}

func TestSQLReadOnly(t *testing.T) {
	s := tempStore(t)
	// Seed a row so SELECT has something to return.
	if _, err := s.UpsertDimensions("12345", []SchemaEntry{{APIName: "pagePath"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	path := s.Path()
	_ = s.Close()

	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer ro.Close()

	rows, err := ro.DB().Query(`SELECT api_name FROM dimensions`)
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("expected at least 1 row")
	}

	// Writes should fail.
	_, err = ro.DB().Exec(`INSERT INTO dimensions(property_id, api_name, updated_at) VALUES('1', 'x', 'now')`)
	if err == nil {
		t.Fatalf("expected write to fail on read-only store, got nil")
	}
}
