// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `export` dumps the local SQLite store to a portable file format (CSV,
// JSON, or JSONL) so the synced data can be consumed by external
// pipelines without going through `sql`. This is the canonical
// non-interactive escape hatch: a downstream system points its loader at
// the file path and never has to know about the store schema.

package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"ga4-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

// supportedExportTables lists the domain tables `export` knows how to
// stream out. Hard-coded so we can pre-declare column order (matters for
// CSV consumers) without reflecting on the schema at runtime.
var supportedExportTables = map[string][]string{
	"pages_daily": {
		"property_id", "date", "page_path", "page_title",
		"sessions", "screen_page_views", "engaged_sessions", "total_users",
		"engagement_rate", "average_session_duration", "conversions", "updated_at",
	},
	"dimensions": {"property_id", "api_name", "ui_name", "description", "category", "custom", "updated_at"},
	"metrics":    {"property_id", "api_name", "ui_name", "description", "type", "category", "custom", "updated_at"},
	"properties": {"account_id", "property_id", "name", "time_zone", "currency", "updated_at"},
	"sync_state": {"property_id", "scope", "last_date", "last_run_at", "row_count"},
}

func newExportCmd(flags *rootFlags) *cobra.Command {
	var table string
	var format string
	var outPath string
	var property string
	var limit int
	cmd := &cobra.Command{
		Use:   "export <table>",
		Short: "Dump a local store table to CSV / JSON / JSONL for downstream pipelines",
		Long: `Streams rows from a domain table in $PRESS_DATA_DIR/ga4/data.db to stdout
or a file. Supported tables: ` + strings.Join(sortedTableNames(), ", ") + `.

Format auto-detection: --format defaults to jsonl when --out is set
(streaming-friendly) and json when output goes to stdout. CSV is available
for spreadsheet consumers.

Filtering is intentionally narrow: --property restricts to a single
property_id (where applicable); --limit caps row count. Use 'sql' for
richer projections — export is the bulk-dump primitive.`,
		Example: `  ga4-pp-cli export pages_daily --format csv --out pages.csv --property 12345
  ga4-pp-cli export dimensions --format jsonl --property 12345
  ga4-pp-cli export metrics --agent`,
		Annotations: map[string]string{"mcp:read-only": "true"},
		Args:        cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if len(args) == 1 {
				table = args[0]
			}
			cols, ok := supportedExportTables[table]
			if !ok {
				return usageErr(fmt.Errorf("unknown --table %q (supported: %s)", table, strings.Join(sortedTableNames(), ", ")))
			}
			if format == "" {
				if outPath != "" {
					format = "jsonl"
				} else {
					format = "json"
				}
			}
			format = strings.ToLower(format)
			switch format {
			case "csv", "json", "jsonl":
				// supported
			default:
				return usageErr(fmt.Errorf("unsupported --format %q (use csv|json|jsonl)", format))
			}

			path := store.DefaultPath()
			if _, err := os.Stat(path); err != nil {
				return usageErr(fmt.Errorf("local store does not exist at %s — run 'ga4-pp-cli sync %s' first", path, syncScopeForTable(table)))
			}
			s, err := store.OpenReadOnly(path)
			if err != nil {
				return fmt.Errorf("open store (read-only): %w", err)
			}
			defer s.Close()

			query, params := buildExportQuery(table, cols, property, limit)
			rows, err := s.DB().Query(query, params...)
			if err != nil {
				return fmt.Errorf("query %s: %w", table, err)
			}
			defer rows.Close()

			var w io.Writer = cmd.OutOrStdout()
			if outPath != "" {
				f, err := os.Create(outPath)
				if err != nil {
					return fmt.Errorf("create output file: %w", err)
				}
				defer f.Close()
				w = f
			}

			n, err := streamExport(w, rows, cols, format)
			if err != nil {
				return err
			}

			if outPath != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "exported %d rows from %s to %s (%s)\n", n, table, outPath, format)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&table, "table", "", "Domain table to export (pages_daily, dimensions, metrics, properties, sync_state)")
	cmd.Flags().StringVar(&format, "format", "", "Output format: csv | json | jsonl (default: jsonl for --out, json otherwise)")
	cmd.Flags().StringVar(&outPath, "out", "", "Write to this file path instead of stdout (recommended for csv and jsonl)")
	cmd.Flags().StringVar(&property, "property", "", "Restrict to rows for this GA4 property ID (numeric, applies where column exists)")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum rows to export (0 means no cap; useful for sampling large pages_daily)")
	return cmd
}

// buildExportQuery assembles a SELECT for the named table with optional
// per-property filtering and limit clause. Returned params line up with
// the placeholders in the query string.
func buildExportQuery(table string, cols []string, property string, limit int) (string, []any) {
	var sb strings.Builder
	sb.WriteString("SELECT ")
	for i, c := range cols {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(c)
	}
	sb.WriteString(" FROM ")
	sb.WriteString(table)

	params := []any{}
	if property != "" && hasColumn(cols, "property_id") {
		sb.WriteString(" WHERE property_id = ?")
		params = append(params, property)
	}
	// Stable order for reproducible exports.
	switch table {
	case "pages_daily":
		sb.WriteString(" ORDER BY date ASC, page_path ASC")
	case "dimensions", "metrics":
		sb.WriteString(" ORDER BY api_name ASC")
	case "sync_state":
		sb.WriteString(" ORDER BY scope ASC, property_id ASC")
	}
	if limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", limit))
	}
	return sb.String(), params
}

// streamExport renders the *sql.Rows result set in the requested format
// directly to w. Returns the number of rows emitted.
func streamExport(w io.Writer, rows interface {
	Next() bool
	Scan(dest ...any) error
	Columns() ([]string, error)
	Err() error
}, cols []string, format string) (int, error) {
	colNames, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("columns: %w", err)
	}

	var cw *csv.Writer
	if format == "csv" {
		cw = csv.NewWriter(w)
		if err := cw.Write(colNames); err != nil {
			return 0, fmt.Errorf("write csv header: %w", err)
		}
	}

	// For "json" (array) we open a bracket, then comma-separate.
	if format == "json" {
		if _, err := io.WriteString(w, "[\n"); err != nil {
			return 0, err
		}
	}

	n := 0
	for rows.Next() {
		values := make([]any, len(colNames))
		ptrs := make([]any, len(colNames))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return n, fmt.Errorf("scan: %w", err)
		}
		switch format {
		case "csv":
			row := make([]string, len(values))
			for i, v := range values {
				row[i] = stringifyExportValue(v)
			}
			if err := cw.Write(row); err != nil {
				return n, fmt.Errorf("write csv row: %w", err)
			}
		case "jsonl":
			obj := map[string]any{}
			for i, c := range colNames {
				obj[c] = normalizeExportValue(values[i])
			}
			b, _ := json.Marshal(obj)
			if _, err := w.Write(append(b, '\n')); err != nil {
				return n, err
			}
		case "json":
			obj := map[string]any{}
			for i, c := range colNames {
				obj[c] = normalizeExportValue(values[i])
			}
			b, _ := json.MarshalIndent(obj, "  ", "  ")
			prefix := "  "
			if n > 0 {
				prefix = ",\n  "
			}
			if _, err := io.WriteString(w, prefix+string(b)); err != nil {
				return n, err
			}
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return n, fmt.Errorf("iterate: %w", err)
	}
	switch format {
	case "csv":
		cw.Flush()
		if err := cw.Error(); err != nil {
			return n, fmt.Errorf("flush csv: %w", err)
		}
	case "json":
		if _, err := io.WriteString(w, "\n]\n"); err != nil {
			return n, err
		}
	}
	return n, nil
}

func hasColumn(cols []string, target string) bool {
	for _, c := range cols {
		if c == target {
			return true
		}
	}
	return false
}

func stringifyExportValue(v any) string {
	if v == nil {
		return ""
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

func normalizeExportValue(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

func sortedTableNames() []string {
	names := make([]string, 0, len(supportedExportTables))
	for name := range supportedExportTables {
		names = append(names, name)
	}
	// Insertion order is non-deterministic; sort for stable --help text.
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j-1] > names[j]; j-- {
			names[j-1], names[j] = names[j], names[j-1]
		}
	}
	return names
}

func syncScopeForTable(table string) string {
	switch table {
	case "dimensions", "metrics":
		return "schema"
	case "properties":
		return "properties"
	default:
		return "pages"
	}
}
