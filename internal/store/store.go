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
const StoreSchemaVersion = 2

// schemaDoc is the canonical inline description of the v1 schema. It
// duplicates the DDL in migrations/0001_init.sql so the source file is
// self-documenting: a reader doesn't have to open the embedded .sql to
// see what tables and columns exist. The runtime migrator still uses the
// embedded file; this constant is documentation/audit, never executed.
//
// Keep in sync with migrations/0001_init.sql on every schema change.
const schemaDoc = `
-- v1 schema reference (executable source of truth: migrations/0001_init.sql)

CREATE TABLE properties (
    account_id   TEXT,
    property_id  TEXT NOT NULL PRIMARY KEY,
    name         TEXT,
    time_zone    TEXT,
    currency     TEXT,
    updated_at   TEXT NOT NULL
);

CREATE TABLE dimensions (
    property_id  TEXT NOT NULL,
    api_name     TEXT NOT NULL,
    ui_name      TEXT,
    description  TEXT,
    category     TEXT,
    custom       INTEGER NOT NULL DEFAULT 0,
    updated_at   TEXT NOT NULL,
    PRIMARY KEY(property_id, api_name)
);

CREATE TABLE metrics (
    property_id  TEXT NOT NULL,
    api_name     TEXT NOT NULL,
    ui_name      TEXT,
    description  TEXT,
    type         TEXT,
    category     TEXT,
    custom       INTEGER NOT NULL DEFAULT 0,
    updated_at   TEXT NOT NULL,
    PRIMARY KEY(property_id, api_name)
);

CREATE TABLE pages_daily (
    property_id              TEXT NOT NULL,
    date                     TEXT NOT NULL,
    page_path                TEXT NOT NULL,
    page_title               TEXT,
    sessions                 REAL NOT NULL DEFAULT 0,
    screen_page_views        REAL NOT NULL DEFAULT 0,
    engaged_sessions         REAL NOT NULL DEFAULT 0,
    total_users              REAL NOT NULL DEFAULT 0,
    engagement_rate          REAL NOT NULL DEFAULT 0,
    average_session_duration REAL NOT NULL DEFAULT 0,
    conversions              REAL NOT NULL DEFAULT 0,
    updated_at               TEXT NOT NULL,
    PRIMARY KEY(property_id, date, page_path)
);

CREATE TABLE sync_state (
    property_id  TEXT NOT NULL,
    scope        TEXT NOT NULL,
    last_date    TEXT,
    last_run_at  TEXT NOT NULL,
    row_count    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(property_id, scope)
);
`

// Ensure schemaDoc isn't dead-code-eliminated by the linker so the
// documentation is always carried with the binary.
var _ = schemaDoc

//go:embed migrations/0001_init.sql
var migration0001 string

//go:embed migrations/0002_top_tables.sql
var migration0002 string

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
	if current < 2 {
		// 0002 introduces acquisition_daily, events_daily, devices_geo_daily.
		// Statements are guarded by CREATE TABLE IF NOT EXISTS so re-running
		// against a database that already has them (e.g. a brand-new install
		// that runs both 0001 and 0002 in the same pass) is a no-op.
		if _, err := s.db.ExecContext(ctx, migration0002); err != nil {
			return fmt.Errorf("apply migration 0002: %w", err)
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
// acquisition_daily
// ----------------------------------------------------------------------------

// AcquisitionDaily is one (date, source, medium, campaign) row.
//
// Every dimension column is part of the PRIMARY KEY together with property_id
// and date — if a future caller adds a dimension to the runReport request
// without also adding it here, the rows will silently collapse on UPSERT.
// Don't add e.g. session_default_channel_group as an extra "descriptive"
// dimension; fetch that into a side-table.
type AcquisitionDaily struct {
	PropertyID      string  `json:"property_id"`
	Date            string  `json:"date"`
	SessionSource   string  `json:"session_source"`
	SessionMedium   string  `json:"session_medium"`
	SessionCampaign string  `json:"session_campaign"`
	Sessions        float64 `json:"sessions"`
	TotalUsers      float64 `json:"total_users"`
	NewUsers        float64 `json:"new_users"`
	EngagedSessions float64 `json:"engaged_sessions"`
	Conversions     float64 `json:"conversions"`
	TotalRevenue    float64 `json:"total_revenue"`
}

// UpsertAcquisitionDaily writes a batch in a single tx, replacing rows that
// conflict on (property_id, date, session_source, session_medium,
// session_campaign).
func (s *Store) UpsertAcquisitionDaily(rows []AcquisitionDaily) (int, error) {
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
		INSERT INTO acquisition_daily(
			property_id, date, session_source, session_medium, session_campaign,
			sessions, total_users, new_users, engaged_sessions, conversions, total_revenue, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(property_id, date, session_source, session_medium, session_campaign) DO UPDATE SET
			sessions         = excluded.sessions,
			total_users      = excluded.total_users,
			new_users        = excluded.new_users,
			engaged_sessions = excluded.engaged_sessions,
			conversions      = excluded.conversions,
			total_revenue    = excluded.total_revenue,
			updated_at       = excluded.updated_at
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
			r.PropertyID, r.Date, r.SessionSource, r.SessionMedium, r.SessionCampaign,
			r.Sessions, r.TotalUsers, r.NewUsers, r.EngagedSessions, r.Conversions, r.TotalRevenue, now,
		); err != nil {
			return 0, fmt.Errorf("insert acquisition_daily %s/%s: %w", r.PropertyID, r.Date, err)
		}
		if r.Date > maxDate {
			maxDate = r.Date
		}
		propertyID = r.PropertyID
	}

	if err := s.saveSyncStateTx(tx, propertyID, "acquisition", maxDate, len(rows)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// ----------------------------------------------------------------------------
// events_daily
// ----------------------------------------------------------------------------

// EventDaily is one (date, event_name, page_path) row.
type EventDaily struct {
	PropertyID        string  `json:"property_id"`
	Date              string  `json:"date"`
	EventName         string  `json:"event_name"`
	PagePath          string  `json:"page_path"`
	EventCount        float64 `json:"event_count"`
	EventCountPerUser float64 `json:"event_count_per_user"`
	EventValue        float64 `json:"event_value"`
	TotalUsers        float64 `json:"total_users"`
	Conversions       float64 `json:"conversions"`
}

// UpsertEventsDaily writes a batch in a single tx, replacing rows that
// conflict on (property_id, date, event_name, page_path).
func (s *Store) UpsertEventsDaily(rows []EventDaily) (int, error) {
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
		INSERT INTO events_daily(
			property_id, date, event_name, page_path,
			event_count, event_count_per_user, event_value, total_users, conversions, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(property_id, date, event_name, page_path) DO UPDATE SET
			event_count          = excluded.event_count,
			event_count_per_user = excluded.event_count_per_user,
			event_value          = excluded.event_value,
			total_users          = excluded.total_users,
			conversions          = excluded.conversions,
			updated_at           = excluded.updated_at
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
			r.PropertyID, r.Date, r.EventName, r.PagePath,
			r.EventCount, r.EventCountPerUser, r.EventValue, r.TotalUsers, r.Conversions, now,
		); err != nil {
			return 0, fmt.Errorf("insert events_daily %s/%s/%s: %w", r.PropertyID, r.Date, r.EventName, err)
		}
		if r.Date > maxDate {
			maxDate = r.Date
		}
		propertyID = r.PropertyID
	}

	if err := s.saveSyncStateTx(tx, propertyID, "events", maxDate, len(rows)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// ----------------------------------------------------------------------------
// devices_geo_daily
// ----------------------------------------------------------------------------

// DevicesGeoDaily is one (date, device_category, country) row.
type DevicesGeoDaily struct {
	PropertyID             string  `json:"property_id"`
	Date                   string  `json:"date"`
	DeviceCategory         string  `json:"device_category"`
	Country                string  `json:"country"`
	Sessions               float64 `json:"sessions"`
	TotalUsers             float64 `json:"total_users"`
	EngagedSessions        float64 `json:"engaged_sessions"`
	AverageSessionDuration float64 `json:"average_session_duration"`
	ScreenPageViews        float64 `json:"screen_page_views"`
}

// UpsertDevicesGeoDaily writes a batch in a single tx, replacing rows that
// conflict on (property_id, date, device_category, country).
func (s *Store) UpsertDevicesGeoDaily(rows []DevicesGeoDaily) (int, error) {
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
		INSERT INTO devices_geo_daily(
			property_id, date, device_category, country,
			sessions, total_users, engaged_sessions, average_session_duration, screen_page_views, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(property_id, date, device_category, country) DO UPDATE SET
			sessions                 = excluded.sessions,
			total_users              = excluded.total_users,
			engaged_sessions         = excluded.engaged_sessions,
			average_session_duration = excluded.average_session_duration,
			screen_page_views        = excluded.screen_page_views,
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
			r.PropertyID, r.Date, r.DeviceCategory, r.Country,
			r.Sessions, r.TotalUsers, r.EngagedSessions, r.AverageSessionDuration, r.ScreenPageViews, now,
		); err != nil {
			return 0, fmt.Errorf("insert devices_geo_daily %s/%s/%s: %w", r.PropertyID, r.Date, r.DeviceCategory, err)
		}
		if r.Date > maxDate {
			maxDate = r.Date
		}
		propertyID = r.PropertyID
	}

	if err := s.saveSyncStateTx(tx, propertyID, "devices_geo", maxDate, len(rows)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(rows), nil
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

// AllSyncStates returns every (property, scope) cursor recorded in the
// store, ordered by scope then property. Used by the doctor's cache report
// so agents can see at a glance which scopes are stale and for which
// property.
func (s *Store) AllSyncStates() ([]SyncState, error) {
	rows, err := s.db.Query(`
		SELECT property_id, scope, last_date, last_run_at, row_count
		FROM sync_state
		ORDER BY scope ASC, property_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list sync_state: %w", err)
	}
	defer rows.Close()
	var out []SyncState
	for rows.Next() {
		var st SyncState
		var lastDate sql.NullString
		if err := rows.Scan(&st.PropertyID, &st.Scope, &lastDate, &st.LastRunAt, &st.RowCount); err != nil {
			return nil, err
		}
		if lastDate.Valid {
			st.LastDate = lastDate.String
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// TableRowCount returns the total row count for a domain table. Caller is
// responsible for passing a known table name; an unknown table yields a
// driver-level error rather than an injection point because the value is
// interpolated directly into the SQL.
func (s *Store) TableRowCount(table string) (int, error) {
	allowed := map[string]bool{
		"properties":        true,
		"dimensions":        true,
		"metrics":           true,
		"pages_daily":       true,
		"acquisition_daily": true,
		"events_daily":      true,
		"devices_geo_daily": true,
		"sync_state":        true,
	}
	if !allowed[table] {
		return 0, fmt.Errorf("unknown table %q", table)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		return 0, fmt.Errorf("count %s: %w", table, err)
	}
	return n, nil
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
//
// Search is a convenience aggregator for callers that want all kinds at once;
// callers that need only one kind should prefer the domain-named methods
// SearchDimensions / SearchMetrics / SearchPages which return typed slices.
func (s *Store) Search(propertyID, query string, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 50
	}
	var hits []SearchHit

	dims, err := s.SearchDimensions(propertyID, query, limit)
	if err == nil {
		hits = append(hits, dims...)
	}
	mets, err := s.SearchMetrics(propertyID, query, limit)
	if err == nil {
		hits = append(hits, mets...)
	}
	pages, err := s.SearchPages(propertyID, query, limit)
	if err == nil {
		hits = append(hits, pages...)
	}
	return hits, nil
}

// SearchDimensions runs an FTS5 MATCH against dimensions_fts and returns
// dimension-kind hits ranked by relevance. The query is passed straight to
// FTS5; callers handling raw user input should wrap tokens via buildFTSQuery
// (or quote them) to avoid syntax errors on special characters.
func (s *Store) SearchDimensions(propertyID, query string, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT d.property_id, d.api_name, d.ui_name, d.description
		FROM dimensions_fts f
		JOIN dimensions d ON d.rowid = f.rowid
		WHERE f.dimensions_fts MATCH ? AND d.property_id = ?
		ORDER BY rank
		LIMIT ?
	`, query, propertyID, limit)
	if err != nil {
		return nil, fmt.Errorf("search dimensions: %w", err)
	}
	defer rows.Close()
	var hits []SearchHit
	for rows.Next() {
		h := SearchHit{Kind: "dimension"}
		if err := rows.Scan(&h.PropertyID, &h.APIName, &h.UIName, &h.Description); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// SearchMetrics runs an FTS5 MATCH against metrics_fts and returns
// metric-kind hits ranked by relevance. See SearchDimensions for query
// quoting guidance.
func (s *Store) SearchMetrics(propertyID, query string, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT m.property_id, m.api_name, m.ui_name, m.description
		FROM metrics_fts f
		JOIN metrics m ON m.rowid = f.rowid
		WHERE f.metrics_fts MATCH ? AND m.property_id = ?
		ORDER BY rank
		LIMIT ?
	`, query, propertyID, limit)
	if err != nil {
		return nil, fmt.Errorf("search metrics: %w", err)
	}
	defer rows.Close()
	var hits []SearchHit
	for rows.Next() {
		h := SearchHit{Kind: "metric"}
		if err := rows.Scan(&h.PropertyID, &h.APIName, &h.UIName, &h.Description); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// SearchPages runs a LIKE-based search over pages_daily.page_path and
// page_title and returns page-kind hits. Results are deduplicated by
// page_path and ordered by most-recent date so callers get a clean list.
// FTS5 isn't maintained on pages_daily because the table is the analytics
// firehose; a per-path LIKE is fast enough on the indexed page_path column.
func (s *Store) SearchPages(propertyID, query string, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 50
	}
	likePattern := "%" + strings.ReplaceAll(query, "*", "%") + "%"
	// Strip FTS quote chars so users can paste a SearchDimensions-style
	// query into a page search without coming up empty.
	likePattern = strings.ReplaceAll(likePattern, `"`, "")
	rows, err := s.db.Query(`
		SELECT property_id, page_path, MAX(COALESCE(page_title, '')), MAX(date)
		FROM pages_daily
		WHERE property_id = ?
		  AND (page_path LIKE ? OR COALESCE(page_title, '') LIKE ?)
		GROUP BY property_id, page_path
		ORDER BY MAX(date) DESC
		LIMIT ?
	`, propertyID, likePattern, likePattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search pages: %w", err)
	}
	defer rows.Close()
	var hits []SearchHit
	for rows.Next() {
		h := SearchHit{Kind: "page"}
		if err := rows.Scan(&h.PropertyID, &h.PagePath, &h.PageTitle, &h.Date); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}
