// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// Package store provides local SQLite persistence for ga4-pp-cli.
//
// The store is GA4-shaped (properties, dimensions, metrics, pages_daily,
// sync_state) and pragmatic — only the columns the existing pages/funnel/
// drift/watch commands plus the new search/sql/traffic-anomalies/bot-traffic
// commands actually read are persisted.
//
// Uses modernc.org/sqlite (pure Go, no CGO) so the binary cross-compiles
// without a C toolchain — important for the worker's multi-arch Docker build.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// StoreSchemaVersion is the on-disk schema version this binary understands.
// Stamped into PRAGMA user_version on fresh databases and checked on every
// open. Bump when migrations change table shape.
const StoreSchemaVersion = 1

//go:embed migrations/0001_init.sql
var migration0001 string

// Store is the read+write handle for ga4-pp-cli's local SQLite DB.
type Store struct {
	db      *sql.DB
	path    string
	writeMu sync.Mutex
}

// DefaultPath returns the on-disk location used when callers don't pass an
// explicit path: $PRESS_DATA_DIR/ga4/data.db, with $XDG_DATA_HOME or
// ~/.local/share as the fallback root.
func DefaultPath() string {
	if root := os.Getenv("PRESS_DATA_DIR"); root != "" {
		return filepath.Join(root, "ga4", "data.db")
	}
	if root := os.Getenv("XDG_DATA_HOME"); root != "" {
		return filepath.Join(root, "ga4-pp-cli", "data.db")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "ga4-pp-cli", "data.db")
	}
	return filepath.Join(os.TempDir(), "ga4-pp-cli", "data.db")
}

// Open opens or creates the SQLite store at dbPath using the background ctx.
func Open(dbPath string) (*Store, error) {
	return OpenWithContext(context.Background(), dbPath)
}

// OpenWithContext opens or creates the SQLite store at dbPath, applying
// migrations under ctx so a SIGINT mid-migration propagates.
func OpenWithContext(ctx context.Context, dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}
	db, err := sql.Open("sqlite",
		dbPath+"?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	// WAL + 2 connections: one read cursor open while a second query runs.
	db.SetMaxOpenConns(2)

	s := &Store{db: db, path: dbPath}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	return s, nil
}

// OpenReadOnly opens an existing store in read-only mode. Refuses writes at
// the driver level — used by `sql` to reject mutating statements without
// having to parse them.
func OpenReadOnly(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite",
		"file:"+dbPath+"?mode=ro&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening database (read-only): %w", err)
	}
	db.SetMaxOpenConns(2)
	return &Store{db: db, path: dbPath}, nil
}

// Close closes the underlying *sql.DB.
func (s *Store) Close() error { return s.db.Close() }

// Path returns the on-disk path of the SQLite file.
func (s *Store) Path() string { return s.path }

// DB exposes the underlying *sql.DB for ad-hoc query callers (the `sql`
// command). The returned handle must not be closed by the caller.
func (s *Store) DB() *sql.DB { return s.db }

// SchemaVersion reads PRAGMA user_version.
func (s *Store) SchemaVersion() (int, error) {
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return 0, fmt.Errorf("read user_version: %w", err)
	}
	return v, nil
}

func (s *Store) migrate(ctx context.Context) error {
	var current int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&current); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	if current >= StoreSchemaVersion {
		return nil
	}
	if current == 0 {
		if _, err := s.db.ExecContext(ctx, migration0001); err != nil {
			return fmt.Errorf("apply migration 0001: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`PRAGMA user_version = %d`, StoreSchemaVersion)); err != nil {
		return fmt.Errorf("stamp user_version: %w", err)
	}
	return nil
}

// ----------------------------------------------------------------------------
// Properties
// ----------------------------------------------------------------------------

// Property is the persisted shape for `properties` rows.
type Property struct {
	AccountID  string `json:"account_id,omitempty"`
	PropertyID string `json:"property_id"`
	Name       string `json:"name,omitempty"`
	TimeZone   string `json:"time_zone,omitempty"`
	Currency   string `json:"currency,omitempty"`
	UpdatedAt  string `json:"updated_at"`
}

// UpsertProperty replaces the row with the given property_id.
func (s *Store) UpsertProperty(p Property) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if p.UpdatedAt == "" {
		p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.db.Exec(`
		INSERT INTO properties(account_id, property_id, name, time_zone, currency, updated_at)
		VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(property_id) DO UPDATE SET
			account_id = excluded.account_id,
			name       = excluded.name,
			time_zone  = excluded.time_zone,
			currency   = excluded.currency,
			updated_at = excluded.updated_at
	`, p.AccountID, p.PropertyID, p.Name, p.TimeZone, p.Currency, p.UpdatedAt)
	return err
}

// ----------------------------------------------------------------------------
// Dimensions / Metrics
// ----------------------------------------------------------------------------

// SchemaEntry is the shared shape for dimensions and metrics.
type SchemaEntry struct {
	PropertyID  string `json:"property_id"`
	APIName     string `json:"api_name"`
	UIName      string `json:"ui_name,omitempty"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type,omitempty"` // metrics only
	Category    string `json:"category,omitempty"`
	Custom      bool   `json:"custom,omitempty"`
	UpdatedAt   string `json:"updated_at"`
}

// UpsertDimensions replaces all dimensions for a property in a single tx.
// Older dimensions for the property that aren't in entries are removed so the
// store reflects a fresh metadata fetch.
func (s *Store) UpsertDimensions(propertyID string, entries []SchemaEntry) (int, error) {
	return s.upsertSchema(propertyID, entries, "dimensions")
}

// UpsertMetrics is the metric-side mirror of UpsertDimensions.
func (s *Store) UpsertMetrics(propertyID string, entries []SchemaEntry) (int, error) {
	return s.upsertSchema(propertyID, entries, "metrics")
}

func (s *Store) upsertSchema(propertyID string, entries []SchemaEntry, table string) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s WHERE property_id = ?`, table), propertyID); err != nil {
		return 0, fmt.Errorf("clear %s for property %s: %w", table, propertyID, err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	var stmt *sql.Stmt
	if table == "metrics" {
		stmt, err = tx.Prepare(`
			INSERT INTO metrics(property_id, api_name, ui_name, description, type, category, custom, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		`)
	} else {
		stmt, err = tx.Prepare(`
			INSERT INTO dimensions(property_id, api_name, ui_name, description, category, custom, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?)
		`)
	}
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	n := 0
	for _, e := range entries {
		custom := 0
		if e.Custom {
			custom = 1
		}
		if table == "metrics" {
			if _, err := stmt.Exec(propertyID, e.APIName, e.UIName, e.Description, e.Type, e.Category, custom, now); err != nil {
				return 0, fmt.Errorf("insert metric %s: %w", e.APIName, err)
			}
		} else {
			if _, err := stmt.Exec(propertyID, e.APIName, e.UIName, e.Description, e.Category, custom, now); err != nil {
				return 0, fmt.Errorf("insert dimension %s: %w", e.APIName, err)
			}
		}
		n++
	}

	if err := s.saveSyncStateTx(tx, propertyID, table, "", n); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// ----------------------------------------------------------------------------
// pages_daily
// ----------------------------------------------------------------------------

// PageDaily is one (date, page_path) row.
type PageDaily struct {
	PropertyID             string  `json:"property_id"`
	Date                   string  `json:"date"`
	PagePath               string  `json:"page_path"`
	PageTitle              string  `json:"page_title,omitempty"`
	Sessions               float64 `json:"sessions"`
	ScreenPageViews        float64 `json:"screen_page_views"`
	EngagedSessions        float64 `json:"engaged_sessions"`
	TotalUsers             float64 `json:"total_users"`
	EngagementRate         float64 `json:"engagement_rate"`
	AverageSessionDuration float64 `json:"average_session_duration"`
	Conversions            float64 `json:"conversions"`
}

// UpsertPagesDaily writes a batch of pages_daily rows in a single tx, replacing
// any rows that conflict on (property_id, date, page_path).
func (s *Store) UpsertPagesDaily(rows []PageDaily) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if len(rows) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO pages_daily(
			property_id, date, page_path, page_title,
			sessions, screen_page_views, engaged_sessions, total_users,
			engagement_rate, average_session_duration, conversions, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(property_id, date, page_path) DO UPDATE SET
			page_title               = excluded.page_title,
			sessions                 = excluded.sessions,
			screen_page_views        = excluded.screen_page_views,
			engaged_sessions         = excluded.engaged_sessions,
			total_users              = excluded.total_users,
			engagement_rate          = excluded.engagement_rate,
			average_session_duration = excluded.average_session_duration,
			conversions              = excluded.conversions,
			updated_at               = excluded.updated_at
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	maxDate := ""
	propertyID := ""
	for _, r := range rows {
		if _, err := stmt.Exec(
			r.PropertyID, r.Date, r.PagePath, r.PageTitle,
			r.Sessions, r.ScreenPageViews, r.EngagedSessions, r.TotalUsers,
			r.EngagementRate, r.AverageSessionDuration, r.Conversions, now,
		); err != nil {
			return 0, fmt.Errorf("insert pages_daily %s/%s/%s: %w", r.PropertyID, r.Date, r.PagePath, err)
		}
		if r.Date > maxDate {
			maxDate = r.Date
		}
		propertyID = r.PropertyID
	}

	if err := s.saveSyncStateTx(tx, propertyID, "pages", maxDate, len(rows)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// PagesDailyRange returns pages_daily rows for the given property within a
// closed-closed date range. Rows are ordered by date asc, then page_path asc.
func (s *Store) PagesDailyRange(propertyID, startDate, endDate string) ([]PageDaily, error) {
	rows, err := s.db.Query(`
		SELECT property_id, date, page_path, page_title,
		       sessions, screen_page_views, engaged_sessions, total_users,
		       engagement_rate, average_session_duration, conversions
		FROM pages_daily
		WHERE property_id = ? AND date >= ? AND date <= ?
		ORDER BY date ASC, page_path ASC
	`, propertyID, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PageDaily
	for rows.Next() {
		var p PageDaily
		if err := rows.Scan(&p.PropertyID, &p.Date, &p.PagePath, &p.PageTitle,
			&p.Sessions, &p.ScreenPageViews, &p.EngagedSessions, &p.TotalUsers,
			&p.EngagementRate, &p.AverageSessionDuration, &p.Conversions); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ----------------------------------------------------------------------------
// sync_state
// ----------------------------------------------------------------------------

// SyncState reports per-(property, scope) cursor state.
type SyncState struct {
	PropertyID string `json:"property_id"`
	Scope      string `json:"scope"`
	LastDate   string `json:"last_date,omitempty"`
	LastRunAt  string `json:"last_run_at"`
	RowCount   int    `json:"row_count"`
}

func (s *Store) saveSyncStateTx(tx *sql.Tx, propertyID, scope, lastDate string, rowCount int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := tx.Exec(`
		INSERT INTO sync_state(property_id, scope, last_date, last_run_at, row_count)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(property_id, scope) DO UPDATE SET
			last_date   = COALESCE(NULLIF(excluded.last_date, ''), sync_state.last_date),
			last_run_at = excluded.last_run_at,
			row_count   = excluded.row_count
	`, propertyID, scope, lastDate, now, rowCount)
	return err
}

// GetSyncState returns the cursor for (property, scope) or (nil, nil) if absent.
func (s *Store) GetSyncState(propertyID, scope string) (*SyncState, error) {
	var st SyncState
	var lastDate sql.NullString
	err := s.db.QueryRow(`
		SELECT property_id, scope, last_date, last_run_at, row_count
		FROM sync_state WHERE property_id = ? AND scope = ?
	`, propertyID, scope).Scan(&st.PropertyID, &st.Scope, &lastDate, &st.LastRunAt, &st.RowCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastDate.Valid {
		st.LastDate = lastDate.String
	}
	return &st, nil
}

// ----------------------------------------------------------------------------
// Search
// ----------------------------------------------------------------------------

// SearchHit is a single FTS result row, kind-tagged so callers can render a
// mixed result list cleanly.
type SearchHit struct {
	Kind        string `json:"kind"` // "dimension" | "metric" | "page"
	PropertyID  string `json:"property_id"`
	APIName     string `json:"api_name,omitempty"`
	UIName      string `json:"ui_name,omitempty"`
	Description string `json:"description,omitempty"`
	PagePath    string `json:"page_path,omitempty"`
	PageTitle   string `json:"page_title,omitempty"`
	Date        string `json:"date,omitempty"`
}

// Search runs the FTS5 query against dimensions_fts and metrics_fts and a LIKE
// fallback against pages_daily.page_path / page_title. The query string is
// passed straight to FTS5 (the caller is expected to format it; for raw user
// strings, the wrapper in cli/search.go quotes them).
func (s *Store) Search(propertyID, query string, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 50
	}
	var hits []SearchHit

	// Dimensions
	rows, err := s.db.Query(`
		SELECT d.property_id, d.api_name, d.ui_name, d.description
		FROM dimensions_fts f
		JOIN dimensions d ON d.rowid = f.rowid
		WHERE f.dimensions_fts MATCH ? AND d.property_id = ?
		ORDER BY rank
		LIMIT ?
	`, query, propertyID, limit)
	if err == nil {
		for rows.Next() {
			var h SearchHit
			h.Kind = "dimension"
			if err := rows.Scan(&h.PropertyID, &h.APIName, &h.UIName, &h.Description); err == nil {
				hits = append(hits, h)
			}
		}
		rows.Close()
	}

	// Metrics
	rows, err = s.db.Query(`
		SELECT m.property_id, m.api_name, m.ui_name, m.description
		FROM metrics_fts f
		JOIN metrics m ON m.rowid = f.rowid
		WHERE f.metrics_fts MATCH ? AND m.property_id = ?
		ORDER BY rank
		LIMIT ?
	`, query, propertyID, limit)
	if err == nil {
		for rows.Next() {
			var h SearchHit
			h.Kind = "metric"
			if err := rows.Scan(&h.PropertyID, &h.APIName, &h.UIName, &h.Description); err == nil {
				hits = append(hits, h)
			}
		}
		rows.Close()
	}

	// pages_daily — fall back to LIKE since FTS isn't worth maintaining on the
	// firehose. We also dedupe per page_path so callers get a clean list.
	likePattern := "%" + strings.ReplaceAll(query, "*", "%") + "%"
	rows, err = s.db.Query(`
		SELECT property_id, page_path, MAX(COALESCE(page_title, '')), MAX(date)
		FROM pages_daily
		WHERE property_id = ?
		  AND (page_path LIKE ? OR COALESCE(page_title, '') LIKE ?)
		GROUP BY property_id, page_path
		ORDER BY MAX(date) DESC
		LIMIT ?
	`, propertyID, likePattern, likePattern, limit)
	if err == nil {
		for rows.Next() {
			var h SearchHit
			h.Kind = "page"
			if err := rows.Scan(&h.PropertyID, &h.PagePath, &h.PageTitle, &h.Date); err == nil {
				hits = append(hits, h)
			}
		}
		rows.Close()
	}

	return hits, nil
}
