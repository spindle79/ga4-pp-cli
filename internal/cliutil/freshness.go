// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// Package cliutil — freshness.go is the auth-and-API-agnostic helper for
// answering "is the local cache stale for this scope?" against the SQLite
// sync_state table. The CLI layer wires this to its --data-source policy:
//
//   - data-source=local: stale is informational; the user has opted in to
//     possibly-stale local data.
//   - data-source=live: stale is irrelevant; we'll hit the API anyway.
//   - data-source=auto: stale triggers a best-effort background refresh
//     (auto_refresh.go) without blocking the read.
//
// The helper is deliberately small and untyped beyond the sync_state row
// shape so it can be used from any command that knows its scope name
// ("schema", "pages", "properties") without dragging in the full *store
// surface.

package cliutil

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// EnsureFresh reports whether the sync_state row for the given scope is
// stale relative to maxAge. A missing row is treated as stale (the cache
// hasn't been populated yet). A negative or zero maxAge disables the
// staleness check and always reports fresh.
//
// The function only reads sync_state; it never triggers a sync itself.
// Callers that want to act on a stale verdict should layer an
// auto-refresh policy on top (see internal/cli/auto_refresh.go).
//
// propertyID may be empty for scope rows that aren't property-scoped
// (e.g. global properties-list syncs); in that case the most recent row
// for the scope across any property is used.
func EnsureFresh(ctx context.Context, db *sql.DB, propertyID, scope string, maxAge time.Duration) (stale bool, lastRunAt time.Time, err error) {
	if db == nil {
		return false, time.Time{}, fmt.Errorf("cliutil.EnsureFresh: nil db")
	}
	if scope == "" {
		return false, time.Time{}, fmt.Errorf("cliutil.EnsureFresh: scope is required")
	}
	if maxAge <= 0 {
		// Disabled — caller didn't supply a staleness policy.
		return false, time.Time{}, nil
	}

	var lastStr string
	var qerr error
	if propertyID == "" {
		qerr = db.QueryRowContext(ctx, `
			SELECT last_run_at FROM sync_state
			WHERE scope = ?
			ORDER BY last_run_at DESC
			LIMIT 1
		`, scope).Scan(&lastStr)
	} else {
		qerr = db.QueryRowContext(ctx, `
			SELECT last_run_at FROM sync_state
			WHERE property_id = ? AND scope = ?
		`, propertyID, scope).Scan(&lastStr)
	}
	if qerr == sql.ErrNoRows {
		// No sync recorded for this scope — definitionally stale.
		return true, time.Time{}, nil
	}
	if qerr != nil {
		return false, time.Time{}, fmt.Errorf("read sync_state for scope %q: %w", scope, qerr)
	}

	parsed, perr := time.Parse(time.RFC3339, lastStr)
	if perr != nil {
		// Unparsable timestamps are treated as stale rather than fresh —
		// fail-safe in the user's favor (better to refresh than to serve
		// possibly-stale data when the cursor is corrupt).
		return true, time.Time{}, nil
	}
	age := time.Since(parsed)
	return age > maxAge, parsed, nil
}
