// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `reports funnel` composes a funnel-shaped runReport from a comma-separated
// list of event names — matching the shape that the official Google MCP's
// run_funnel_report exposes (which is itself a runReport composition, not a
// separate REST endpoint). The CLI reduces a multi-step conversion sequence
// to one command and a flag list.

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newReportsFunnelCmd(flags *rootFlags) *cobra.Command {
	var stepsCSV string
	var dateRangeSpec string
	var property string
	cmd := &cobra.Command{
		Use:   "funnel",
		Short: "Step-by-step conversion funnel built on runReport (matches official Google MCP run_funnel_report)",
		Long: `Composes a runReport call that filters on eventName IN (steps...) and
returns per-event counts and conversion rates between adjacent steps.`,
		Example:     "  ga4-pp-cli reports funnel --steps page_view,sign_up,purchase --date-range 28daysAgo,today --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if stepsCSV == "" {
				return fmt.Errorf("--steps is required: comma-separated event names in funnel order")
			}
			steps := []string{}
			for _, s := range strings.Split(stepsCSV, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					steps = append(steps, s)
				}
			}
			if len(steps) < 2 {
				return fmt.Errorf("funnel needs at least 2 steps")
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

			// IN-list filter: dimensionFilter -> filter -> inListFilter
			inList := []any{}
			for _, s := range steps {
				inList = append(inList, s)
			}
			body := map[string]any{
				"dimensions": dimList("eventName"),
				"metrics":    metricList("eventCount,totalUsers"),
				"dateRanges": dateRange(dateRangeSpec),
				"dimensionFilter": map[string]any{
					"filter": map[string]any{
						"fieldName": "eventName",
						"inListFilter": map[string]any{
							"values":        inList,
							"caseSensitive": true,
						},
					},
				},
				"returnPropertyQuota": true,
			}
			report, err := runReport(newClientAdapter(c), prop, body)
			if err != nil {
				return classifyAPIError(err, flags)
			}

			// Project into ordered funnel rows + drop-off rates
			counts := map[string]int{}
			users := map[string]int{}
			if rows, ok := report["rows"].([]any); ok {
				for _, r := range rows {
					rm, _ := r.(map[string]any)
					dv, _ := rm["dimensionValues"].([]any)
					mv, _ := rm["metricValues"].([]any)
					if len(dv) == 0 || len(mv) < 2 {
						continue
					}
					name, _ := dv[0].(map[string]any)["value"].(string)
					ec, _ := mv[0].(map[string]any)["value"].(string)
					tu, _ := mv[1].(map[string]any)["value"].(string)
					counts[name] = atoi(ec)
					users[name] = atoi(tu)
				}
			}
			out := []map[string]any{}
			var prev int
			for i, s := range steps {
				row := map[string]any{
					"step":      i + 1,
					"eventName": s,
					"events":    counts[s],
					"users":     users[s],
				}
				if i > 0 && prev > 0 {
					row["dropoff_rate"] = float64(prev-counts[s]) / float64(prev)
					row["dropoff_count"] = prev - counts[s]
				}
				prev = counts[s]
				out = append(out, row)
			}
			payload := map[string]any{
				"funnel":   out,
				"property": prop,
				"steps":    steps,
			}
			payload["_warnings"] = extractGA4Warnings(report)
			b, _ := json.MarshalIndent(payload, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	cmd.Flags().StringVar(&stepsCSV, "steps", "", "Comma-separated funnel steps (ordered event names defining the conversion path)")
	// Next flag, --date-range, picks the date window.
	cmd.Flags().StringVar(&dateRangeSpec, "date-range", "", "Date range spec, e.g. 7d / 28daysAgo,today / 2026-01-01,2026-01-31")
	// Final flag, --property, identifies the GA4 source.
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID, numeric (defaults to the GA_PROPERTY_ID environment variable)")
	// --steps has no sensible default and the funnel is meaningless without
	// it; surface the requirement at the cobra layer so --help renders it
	// and missing-flag errors arrive before RunE.
	_ = cmd.MarkFlagRequired("steps")
	return cmd
}

// atoi tolerantly converts the string-typed metric values GA4 returns to ints.
// Empty / unparsable values become 0 rather than failing the whole funnel.
func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}
