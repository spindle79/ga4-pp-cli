// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `drift pages` runs the same pageviews report for two adjacent date windows
// in a single batchRunReports call, joins the rows on pagePath, and sorts by
// the absolute % delta. Period-over-period analysis is the #1 GA4 web UI pain
// point flagged in research; this collapses it into one command.

package cli

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

func newDriftCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "drift",
		Short:   "Compare two adjacent date ranges and surface biggest movers",
		Long:    `One batchRunReports call hits both periods; the join happens locally.`,
	}
	cmd.AddCommand(newDriftPagesCmd(flags))
	return cmd
}

func newDriftPagesCmd(flags *rootFlags) *cobra.Command {
	var window string
	var top int
	var property string
	var metric string
	cmd := &cobra.Command{
		Use:   "pages",
		Short: "Top page movers between two adjacent windows",
		Example: `  ga4-pp-cli drift pages --window 7d --vs prior --top 20 --agent
  ga4-pp-cli drift pages --window 30d --metric engagementRate --top 10 --agent`,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			prop := property
			if prop == "" {
				p, err := resolveProperty(nil, flags)
				if err != nil {
					return err
				}
				prop = p
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}

			cur, prev, err := windowRanges(window)
			if err != nil {
				return err
			}
			body := map[string]any{
				"requests": []map[string]any{
					{
						"dimensions": dimList("pagePath"),
						"metrics":    metricList(metric),
						"dateRanges": []map[string]any{cur},
						"limit":      "10000",
					},
					{
						"dimensions": dimList("pagePath"),
						"metrics":    metricList(metric),
						"dateRanges": []map[string]any{prev},
						"limit":      "10000",
					},
				},
			}
			report, err := batchRunReports(newClientAdapter(c), prop, body)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			reports, _ := report["reports"].([]any)
			if len(reports) != 2 {
				return fmt.Errorf("expected 2 reports in batch response, got %d", len(reports))
			}
			curRows := pagePathRows(reports[0])
			prevRows := pagePathRows(reports[1])

			type driftRow struct {
				PagePath  string  `json:"pagePath"`
				Current   int     `json:"current"`
				Prior     int     `json:"prior"`
				Delta     int     `json:"delta"`
				DeltaPct  float64 `json:"delta_pct"`
				Direction string  `json:"direction"`
			}
			rows := []driftRow{}
			seen := map[string]bool{}
			for path, cur := range curRows {
				prev := prevRows[path]
				delta := cur - prev
				var pct float64
				if prev > 0 {
					pct = float64(delta) / float64(prev)
				} else if cur > 0 {
					pct = 1.0 // new entrant — 100% increase
				}
				dir := "flat"
				if delta > 0 {
					dir = "up"
				} else if delta < 0 {
					dir = "down"
				}
				rows = append(rows, driftRow{path, cur, prev, delta, pct, dir})
				seen[path] = true
			}
			for path, prev := range prevRows {
				if seen[path] {
					continue
				}
				rows = append(rows, driftRow{path, 0, prev, -prev, -1.0, "exit"})
			}
			sort.Slice(rows, func(i, j int) bool {
				return abs(rows[i].DeltaPct) > abs(rows[j].DeltaPct)
			})
			if top > 0 && len(rows) > top {
				rows = rows[:top]
			}

			payload := map[string]any{
				"property":   prop,
				"metric":     metric,
				"window":     window,
				"current":    cur,
				"prior":      prev,
				"row_count":  len(rows),
				"rows":       rows,
				"_warnings":  append(extractGA4Warnings(asMap(reports[0])), extractGA4Warnings(asMap(reports[1]))...),
			}
			b, _ := json.MarshalIndent(payload, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	cmd.Flags().StringVar(&window, "window", "7d", "Window size: 1d|7d|14d|28d|30d|90d (vs prior period of same length)")
	cmd.Flags().IntVar(&top, "top", 20, "Limit to N biggest movers (0 = all)")
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID (defaults to GA_PROPERTY_ID)")
	cmd.Flags().StringVar(&metric, "metric", "screenPageViews", "Metric to compare (e.g. screenPageViews, totalUsers, engagedSessions)")
	return cmd
}

// windowRanges turns a window like "7d" into ("7daysAgo,today",
// "14daysAgo,8daysAgo"). The GA4 Data API accepts these relative date
// expressions; we produce them as map[string]any to drop straight into a
// runReport body.
func windowRanges(window string) (cur, prev map[string]any, err error) {
	switch window {
	case "1d", "today":
		return map[string]any{"startDate": "today", "endDate": "today"},
			map[string]any{"startDate": "yesterday", "endDate": "yesterday"}, nil
	case "7d":
		return map[string]any{"startDate": "7daysAgo", "endDate": "today"},
			map[string]any{"startDate": "14daysAgo", "endDate": "8daysAgo"}, nil
	case "14d":
		return map[string]any{"startDate": "14daysAgo", "endDate": "today"},
			map[string]any{"startDate": "28daysAgo", "endDate": "15daysAgo"}, nil
	case "28d":
		return map[string]any{"startDate": "28daysAgo", "endDate": "today"},
			map[string]any{"startDate": "56daysAgo", "endDate": "29daysAgo"}, nil
	case "30d":
		return map[string]any{"startDate": "30daysAgo", "endDate": "today"},
			map[string]any{"startDate": "60daysAgo", "endDate": "31daysAgo"}, nil
	case "90d":
		return map[string]any{"startDate": "90daysAgo", "endDate": "today"},
			map[string]any{"startDate": "180daysAgo", "endDate": "91daysAgo"}, nil
	}
	return nil, nil, fmt.Errorf("unsupported window %q (use 1d/7d/14d/28d/30d/90d)", window)
}

// pagePathRows extracts {pagePath: metricValue} from a runReport-shaped report
// where the first dimension is pagePath and the first metric is the one being
// compared.
func pagePathRows(reportAny any) map[string]int {
	out := map[string]int{}
	rep, _ := reportAny.(map[string]any)
	rows, _ := rep["rows"].([]any)
	for _, r := range rows {
		rm, _ := r.(map[string]any)
		dv, _ := rm["dimensionValues"].([]any)
		mv, _ := rm["metricValues"].([]any)
		if len(dv) == 0 || len(mv) == 0 {
			continue
		}
		path, _ := dv[0].(map[string]any)["value"].(string)
		val, _ := mv[0].(map[string]any)["value"].(string)
		out[path] = atoi(val)
	}
	return out
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
