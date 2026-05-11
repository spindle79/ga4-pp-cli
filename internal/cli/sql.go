// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `sql` is a read-only raw-query escape hatch over the local SQLite store.
// Only SELECT/WITH/EXPLAIN/PRAGMA are accepted; the store is opened
// read-only so any mutating statement that slipped past the guard would still
// fail at the driver level.

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"ga4-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

func newSQLCmd(flags *rootFlags) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "sql <query>",
		Short: "Run a read-only SQL query against the local SQLite store",
		Long: `Executes a single SELECT/WITH/EXPLAIN/PRAGMA against the on-disk store at
$PRESS_DATA_DIR/ga4/data.db (created by 'sync'). The store is opened in
read-only mode and write statements are rejected.

  ga4-pp-cli sql "SELECT page_path, SUM(sessions) FROM pages_daily GROUP BY page_path ORDER BY 2 DESC LIMIT 10"

Tables: properties, dimensions, metrics, pages_daily, sync_state.
FTS5 mirrors: dimensions_fts, metrics_fts.`,
		Example:     `  ga4-pp-cli sql "SELECT api_name, ui_name FROM metrics LIMIT 5"`,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			query := strings.Join(args, " ")
			if err := guardReadOnlySQL(query); err != nil {
				return usageErr(err)
			}

			path := store.DefaultPath()
			if _, err := os.Stat(path); err != nil {
				return usageErr(fmt.Errorf("local store does not exist at %s — run 'ga4-pp-cli sync schema' (and/or 'sync pages') first", path))
			}
			s, err := store.OpenReadOnly(path)
			if err != nil {
				return fmt.Errorf("open store (read-only) at %s: %w", path, err)
			}
			defer s.Close()

			rows, err := s.DB().Query(query)
			if err != nil {
				return fmt.Errorf("query: %w", err)
			}
			defer rows.Close()

			cols, err := rows.Columns()
			if err != nil {
				return fmt.Errorf("columns: %w", err)
			}
			out := []map[string]any{}
			for rows.Next() {
				vals := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					return fmt.Errorf("scan: %w", err)
				}
				row := map[string]any{}
				for i, c := range cols {
					row[c] = normalizeSQLValue(vals[i])
				}
				out = append(out, row)
				if limit > 0 && len(out) >= limit {
					break
				}
			}
			if err := rows.Err(); err != nil {
				return fmt.Errorf("iterate: %w", err)
			}
			b, _ := json.MarshalIndent(map[string]any{
				"columns": cols,
				"count":   len(out),
				"rows":    out,
			}, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "Max rows to return (0 = no cap; the query's own LIMIT still applies)")
	return cmd
}

// guardReadOnlySQL rejects anything that isn't a SELECT/WITH/EXPLAIN/PRAGMA.
// Belt-and-braces alongside the read-only store handle: the database file is
// opened with ?mode=ro, so a write would fail anyway, but we'd rather refuse
// it with a clear message than surface modernc's driver error.
func guardReadOnlySQL(q string) error {
	trimmed := strings.TrimSpace(q)
	// strip leading comments
	for strings.HasPrefix(trimmed, "--") {
		if idx := strings.IndexByte(trimmed, '\n'); idx >= 0 {
			trimmed = strings.TrimSpace(trimmed[idx+1:])
		} else {
			trimmed = ""
		}
	}
	if trimmed == "" {
		return fmt.Errorf("empty query")
	}
	upper := strings.ToUpper(trimmed)
	for _, prefix := range []string{"SELECT", "WITH", "EXPLAIN", "PRAGMA"} {
		if strings.HasPrefix(upper, prefix) {
			return nil
		}
	}
	return fmt.Errorf("only SELECT/WITH/EXPLAIN/PRAGMA are allowed in 'sql'; got: %s", strings.SplitN(trimmed, " ", 2)[0])
}

// normalizeSQLValue converts []byte from the driver into strings so the JSON
// output is human-readable. modernc/sqlite returns TEXT columns as []byte
// by default; without this, JSON-marshal renders them as base64.
func normalizeSQLValue(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}
