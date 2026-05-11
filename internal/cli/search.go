// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `search` is the top-level FTS5 query over the local SQLite store. It searches
// dimensions + metrics + pages_daily (page_path/page_title) in one call, honoring
// --data-source local|live|auto. For "live" the command degrades gracefully:
// we don't have a server-side text search, so we surface a clear hint and exit
// with usage code 2 rather than masquerading as offline search.

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newSearchCmd(flags *rootFlags) *cobra.Command {
	var property string
	var limit int
	cmd := &cobra.Command{
		Use:     "search <query>",
		Short:   "Search dimensions, metrics, and synced page paths via the local FTS5 index",
		Long: `Runs an FTS5 query against the local SQLite store written by 'sync schema' /
'sync pages'. Searches across dimension/metric apiName/uiName/description and
LIKE-matches page_path/page_title in pages_daily.

Requires the store to be populated — run 'ga4-pp-cli sync schema' and (optionally)
'ga4-pp-cli sync pages' first.`,
		Example: "  ga4-pp-cli search engagement --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			if flags.dataSource == "live" {
				return usageErr(fmt.Errorf("--data-source=live is not supported for 'search' (no server-side text index); rerun with --data-source local or auto after 'ga4-pp-cli sync schema'"))
			}
			prop, err := resolveProperty(nil, flags)
			if err != nil && property == "" {
				return err
			}
			if property != "" {
				prop = property
			}
			query := buildFTSQuery(strings.Join(args, " "))

			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			hits, err := s.Search(prop, query, limit)
			if err != nil {
				return fmt.Errorf("search: %w", err)
			}
			out := map[string]any{
				"property": prop,
				"query":    strings.Join(args, " "),
				"count":    len(hits),
				"results":  hits,
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID (defaults to GA_PROPERTY_ID)")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max results per result class (dimensions, metrics, pages)")
	return cmd
}

// buildFTSQuery converts a free-form user string into an FTS5 MATCH expression.
// Each whitespace-separated token is wrapped in double quotes (escaping any
// embedded quotes) and joined with implicit AND. This avoids FTS5 syntax
// errors when users include hyphens, colons, or other characters that look
// like operators (e.g., "page:engagement").
func buildFTSQuery(s string) string {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	for i, p := range parts {
		p = strings.ReplaceAll(p, `"`, `""`)
		parts[i] = `"` + p + `"`
	}
	return strings.Join(parts, " ")
}
