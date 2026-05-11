// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `trends pages` is a sibling of `drift pages` aimed at the "what's
// trending now?" question rather than "what changed between two
// adjacent windows?". It computes a per-page rolling delta over the
// requested lookback window and ranks pages by the strength of the
// trend rather than the size of a single jump. Reads the local store
// (run 'sync pages' first); --data-source live falls back to a single
// runReport across the lookback window.

package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"ga4-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

// trendRow is one ranked page in the trends output.
type trendRow struct {
	PagePath      string  `json:"page_path"`
	PageTitle     string  `json:"page_title,omitempty"`
	FirstSessions float64 `json:"first_sessions"`
	LastSessions  float64 `json:"last_sessions"`
	Slope         float64 `json:"slope"` // sessions per day
	PctChange     float64 `json:"pct_change"`
	Days          int     `json:"days_in_window"`
	Direction     string  `json:"direction"`
}

func newTrendsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trends",
		Short: "Rank pages by trend strength (rolling slope of sessions) over a lookback window",
		Long: `Where 'drift' answers "what changed between two adjacent windows", 'trends'
answers "what's been steadily moving over the whole window?" by fitting a
simple slope over the per-day sessions series for each page.`,
	}
	cmd.AddCommand(newTrendsPagesCmd(flags))
	return cmd
}

func newTrendsPagesCmd(flags *rootFlags) *cobra.Command {
	var property string
	var days int
	var top int
	var direction string
	var minSessions float64
	cmd := &cobra.Command{
		Use:         "pages",
		Short:       "Top trending pages by rolling slope of sessions across the lookback window",
		Example:     "  ga4-pp-cli trends pages --property 12345 --days 30 --top 20 --direction up --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if days <= 0 {
				days = 30
			}
			prop, err := resolveProperty(nil, flags)
			if err != nil && property == "" {
				return err
			}
			if property != "" {
				prop = property
			}

			rows, err := pagesDailyForCompound(cmd, flags, prop, days)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				return printOutputWithFlags(cmd.OutOrStdout(), []byte(`{"count":0,"trends":[]}`), flags)
			}
			trends := computeTrends(rows, minSessions)
			switch direction {
			case "up":
				trends = filterTrends(trends, func(t trendRow) bool { return t.Slope > 0 })
			case "down":
				trends = filterTrends(trends, func(t trendRow) bool { return t.Slope < 0 })
			case "", "any", "all":
				// no filter
			default:
				return usageErr(fmt.Errorf("invalid --direction %q (use up|down|any)", direction))
			}
			sort.Slice(trends, func(i, j int) bool {
				return math.Abs(trends[i].Slope) > math.Abs(trends[j].Slope)
			})
			if top > 0 && len(trends) > top {
				trends = trends[:top]
			}
			out := map[string]any{
				"property":  prop,
				"days":      days,
				"direction": direction,
				"count":     len(trends),
				"trends":    trends,
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID (numeric, defaults to GA_PROPERTY_ID env var)")
	cmd.Flags().IntVar(&days, "days", 30, "Lookback window in days over which the per-page session slope is fitted")
	cmd.Flags().IntVar(&top, "top", 20, "Maximum trending pages to return after ranking by absolute slope strength")
	cmd.Flags().StringVar(&direction, "direction", "any", "Filter trends by sign of slope: up | down | any (default any)")
	cmd.Flags().Float64Var(&minSessions, "min-sessions", 25, "Drop pages whose total window sessions fall below this floor (signal threshold)")
	return cmd
}

func computeTrends(rows []store.PageDaily, minSessions float64) []trendRow {
	type series struct {
		title string
		dates []string
		vals  []float64
	}
	byPath := map[string]*series{}
	for _, r := range rows {
		s := byPath[r.PagePath]
		if s == nil {
			s = &series{title: r.PageTitle}
			byPath[r.PagePath] = s
		}
		if r.PageTitle != "" {
			s.title = r.PageTitle
		}
		s.dates = append(s.dates, r.Date)
		s.vals = append(s.vals, r.Sessions)
	}

	out := []trendRow{}
	for path, s := range byPath {
		if len(s.vals) < 3 {
			continue
		}
		total := 0.0
		for _, v := range s.vals {
			total += v
		}
		if total < minSessions {
			continue
		}
		// Sort by date so slope reflects time, not insertion order. Both
		// dates and vals must stay aligned through the sort.
		idx := make([]int, len(s.dates))
		for i := range idx {
			idx[i] = i
		}
		sort.Slice(idx, func(a, b int) bool { return s.dates[idx[a]] < s.dates[idx[b]] })
		ordered := make([]float64, len(s.vals))
		for i, j := range idx {
			ordered[i] = s.vals[j]
		}
		slope := simpleSlope(ordered)
		first := ordered[0]
		last := ordered[len(ordered)-1]
		pct := 0.0
		if first > 0 {
			pct = (last - first) / first
		} else if last > 0 {
			pct = 1.0
		}
		dir := "flat"
		if slope > 0 {
			dir = "up"
		} else if slope < 0 {
			dir = "down"
		}
		out = append(out, trendRow{
			PagePath:      path,
			PageTitle:     s.title,
			FirstSessions: first,
			LastSessions:  last,
			Slope:         roundN(slope, 3),
			PctChange:     roundN(pct, 3),
			Days:          len(ordered),
			Direction:     dir,
		})
	}
	return out
}

// simpleSlope returns the least-squares slope of vs against the index
// 0..n-1. Used as the "trend strength" for a single page series.
func simpleSlope(vs []float64) float64 {
	n := float64(len(vs))
	if n < 2 {
		return 0
	}
	sumX := (n - 1) * n / 2 // 0+1+..+(n-1)
	sumX2 := (n - 1) * n * (2*n - 1) / 6
	sumY := 0.0
	sumXY := 0.0
	for i, y := range vs {
		x := float64(i)
		sumY += y
		sumXY += x * y
	}
	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denom
}

func filterTrends(rows []trendRow, keep func(trendRow) bool) []trendRow {
	out := rows[:0]
	for _, r := range rows {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}
