// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `sync` populates the local SQLite store for offline querying:
//   • sync schema     dimensions + metrics via getMetadata
//   • sync pages      pages_daily via runReport (page-granularity)
//   • sync properties properties table via the Admin API
//
// The existing `schema fetch` JSON cache is kept for back-compat but the SQLite
// store is the canonical local data layer going forward.

package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"ga4-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

func newSyncCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Populate the local SQLite store (schema, pages, properties)",
		Long: `Top-level entrypoint for populating $PRESS_DATA_DIR/ga4/data.db.

  ga4-pp-cli sync schema      # dimensions + metrics via getMetadata
  ga4-pp-cli sync pages       # pages_daily rolling window via runReport
  ga4-pp-cli sync properties  # Admin API → properties table

After a sync, 'ga4-pp-cli search' and 'ga4-pp-cli sql' run entirely offline.`,
	}
	cmd.AddCommand(
		newSyncSchemaCmd(flags),
		newSyncPagesCmd(flags),
		newSyncPropertiesCmd(flags),
	)
	return cmd
}

// openStore opens the default store path or returns a usable error.
func openStore() (*store.Store, error) {
	path := store.DefaultPath()
	s, err := store.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open store at %s: %w", path, err)
	}
	return s, nil
}

func newSyncSchemaCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "schema [property]",
		Short:       "Sync dimensions and metrics from getMetadata into the local SQLite store",
		Example:     "  ga4-pp-cli sync schema 12345 --agent",
		Annotations: map[string]string{"mcp:read-only": "false"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			argv := args
			if len(argv) == 0 && property != "" {
				argv = []string{property}
			}
			prop, err := resolveProperty(argv, flags)
			if err != nil {
				return err
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			adapter := newClientAdapter(c)
			data, err := adapter.get("/v1beta/properties/"+prop+"/metadata", nil)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			var raw struct {
				Dimensions []map[string]any `json:"dimensions"`
				Metrics    []map[string]any `json:"metrics"`
			}
			if err := json.Unmarshal(data, &raw); err != nil {
				return fmt.Errorf("parsing metadata: %w", err)
			}

			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			dims := make([]store.SchemaEntry, 0, len(raw.Dimensions))
			for _, d := range raw.Dimensions {
				dims = append(dims, store.SchemaEntry{
					APIName:     str(d["apiName"]),
					UIName:      str(d["uiName"]),
					Description: str(d["description"]),
					Category:    str(d["category"]),
					Custom:      boolish(d["customDefinition"]),
				})
			}
			mets := make([]store.SchemaEntry, 0, len(raw.Metrics))
			for _, m := range raw.Metrics {
				mets = append(mets, store.SchemaEntry{
					APIName:     str(m["apiName"]),
					UIName:      str(m["uiName"]),
					Description: str(m["description"]),
					Type:        str(m["type"]),
					Category:    str(m["category"]),
					Custom:      boolish(m["customDefinition"]),
				})
			}

			dn, err := s.UpsertDimensions(prop, dims)
			if err != nil {
				return fmt.Errorf("upsert dimensions: %w", err)
			}
			mn, err := s.UpsertMetrics(prop, mets)
			if err != nil {
				return fmt.Errorf("upsert metrics: %w", err)
			}

			out := map[string]any{
				"property":         prop,
				"dimensions_count": dn,
				"metrics_count":    mn,
				"store_path":       s.Path(),
				"synced_at":        timeNowRFC3339(),
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID (defaults to GA_PROPERTY_ID)")
	return cmd
}

func newSyncPagesCmd(flags *rootFlags) *cobra.Command {
	var property string
	var days int
	var pageSize int
	cmd := &cobra.Command{
		Use:         "pages [property]",
		Short:       "Sync per-day, per-page session metrics into pages_daily via runReport",
		Example:     "  ga4-pp-cli sync pages 12345 --days 30 --agent",
		Annotations: map[string]string{"mcp:read-only": "false"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			argv := args
			if len(argv) == 0 && property != "" {
				argv = []string{property}
			}
			prop, err := resolveProperty(argv, flags)
			if err != nil {
				return err
			}
			if days <= 0 {
				days = 28
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			startDate := fmt.Sprintf("%ddaysAgo", days)
			body := map[string]any{
				"dimensions": []map[string]any{
					{"name": "date"},
					{"name": "pagePath"},
					{"name": "pageTitle"},
				},
				"metrics": []map[string]any{
					{"name": "sessions"},
					{"name": "screenPageViews"},
					{"name": "engagedSessions"},
					{"name": "totalUsers"},
					{"name": "engagementRate"},
					{"name": "averageSessionDuration"},
					{"name": "conversions"},
				},
				"dateRanges": []map[string]any{
					{"startDate": startDate, "endDate": "today"},
				},
				"limit":               strconv.Itoa(pageSize),
				"returnPropertyQuota": true,
			}
			report, err := runReport(newClientAdapter(c), prop, body)
			if err != nil {
				return classifyAPIError(err, flags)
			}

			rows, err := pagesDailyFromReport(prop, report)
			if err != nil {
				return err
			}

			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			n, err := s.UpsertPagesDaily(rows)
			if err != nil {
				return fmt.Errorf("upsert pages_daily: %w", err)
			}

			out := map[string]any{
				"property":   prop,
				"days":       days,
				"row_count":  n,
				"start_date": startDate,
				"end_date":   "today",
				"store_path": s.Path(),
				"synced_at":  timeNowRFC3339(),
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID (defaults to GA_PROPERTY_ID)")
	cmd.Flags().IntVar(&days, "days", 28, "How many days back to sync (rolling window)")
	cmd.Flags().IntVar(&pageSize, "page-size", 100000, "Max rows per runReport call (GA4 caps at 100k)")
	return cmd
}

func newSyncPropertiesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "properties",
		Short:       "Sync accessible GA4 properties from the Admin API into the local store",
		Example:     "  ga4-pp-cli sync properties --agent",
		Annotations: map[string]string{"mcp:read-only": "false"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			body, err := adminGet(cmd.Context(), "/v1beta/accountSummaries")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			var raw struct {
				AccountSummaries []struct {
					Account           string `json:"account"`
					DisplayName       string `json:"displayName"`
					PropertySummaries []struct {
						Property     string `json:"property"`
						DisplayName  string `json:"displayName"`
						PropertyType string `json:"propertyType"`
					} `json:"propertySummaries"`
				} `json:"accountSummaries"`
			}
			if err := json.Unmarshal(body, &raw); err != nil {
				return fmt.Errorf("parsing accountSummaries: %w", err)
			}

			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			n := 0
			for _, a := range raw.AccountSummaries {
				for _, p := range a.PropertySummaries {
					if p.Property == "" {
						continue
					}
					pid := p.Property
					// "properties/12345" → "12345"
					if len(pid) > len("properties/") && pid[:len("properties/")] == "properties/" {
						pid = pid[len("properties/"):]
					}
					if err := s.UpsertProperty(store.Property{
						AccountID:  a.Account,
						PropertyID: pid,
						Name:       p.DisplayName,
						UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
					}); err != nil {
						return fmt.Errorf("upsert property %s: %w", pid, err)
					}
					n++
				}
			}

			out := map[string]any{
				"count":      n,
				"store_path": s.Path(),
				"synced_at":  timeNowRFC3339(),
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	return cmd
}

// pagesDailyFromReport converts a runReport response into PageDaily rows.
// The report is expected to have dimensions [date, pagePath, pageTitle] and
// metrics matching the request in newSyncPagesCmd. Missing metric values are
// treated as 0; missing dimensions skip the row.
func pagesDailyFromReport(propertyID string, report map[string]any) ([]store.PageDaily, error) {
	rowsAny, _ := report["rows"].([]any)
	out := make([]store.PageDaily, 0, len(rowsAny))
	for _, ra := range rowsAny {
		row, _ := ra.(map[string]any)
		if row == nil {
			continue
		}
		dims, _ := row["dimensionValues"].([]any)
		mets, _ := row["metricValues"].([]any)
		if len(dims) < 2 {
			continue
		}
		dateRaw := dimensionAt(dims, 0)
		pathRaw := dimensionAt(dims, 1)
		titleRaw := dimensionAt(dims, 2)
		if pathRaw == "" || dateRaw == "" {
			continue
		}
		out = append(out, store.PageDaily{
			PropertyID:             propertyID,
			Date:                   normalizeReportDate(dateRaw),
			PagePath:               pathRaw,
			PageTitle:              titleRaw,
			Sessions:               metricAt(mets, 0),
			ScreenPageViews:        metricAt(mets, 1),
			EngagedSessions:        metricAt(mets, 2),
			TotalUsers:             metricAt(mets, 3),
			EngagementRate:         metricAt(mets, 4),
			AverageSessionDuration: metricAt(mets, 5),
			Conversions:            metricAt(mets, 6),
		})
	}
	return out, nil
}

func dimensionAt(dims []any, i int) string {
	if i >= len(dims) {
		return ""
	}
	d, _ := dims[i].(map[string]any)
	if d == nil {
		return ""
	}
	return str(d["value"])
}

func metricAt(mets []any, i int) float64 {
	if i >= len(mets) {
		return 0
	}
	m, _ := mets[i].(map[string]any)
	if m == nil {
		return 0
	}
	v, _ := strconv.ParseFloat(str(m["value"]), 64)
	return v
}

// normalizeReportDate converts GA4's "20260501" date dimension to "2026-05-01"
// so the persisted column is sortable lexicographically and matches what the
// rest of the codebase emits.
func normalizeReportDate(s string) string {
	if len(s) == 8 {
		return s[:4] + "-" + s[4:6] + "-" + s[6:8]
	}
	return s
}
