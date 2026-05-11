// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `traffic-anomalies` is the per-page z-score scan over pages_daily. It's a
// compound command in the Steinberger sense: a single CLI invocation answers
// the multi-step question "which pages moved unusually since their own
// baseline?" without the agent having to compose runReport calls and reduce
// the response itself. Prefer the local store (run 'sync pages' first);
// --data-source live falls back to a single runReport when available.

package cli

import (
	"encoding/json"
	"math"
	"sort"

	"ga4-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

// Anomaly is one z-scored page result for the traffic-anomalies command.
type Anomaly struct {
	PagePath   string  `json:"page_path"`
	PageTitle  string  `json:"page_title,omitempty"`
	Latest     float64 `json:"latest_sessions"`
	Mean       float64 `json:"mean_sessions"`
	Stdev      float64 `json:"stdev"`
	ZScore     float64 `json:"z_score"`
	LatestDate string  `json:"latest_date"`
	Days       int     `json:"days_in_window"`
}

func newTrafficAnomaliesCmd(flags *rootFlags) *cobra.Command {
	var property string
	var days int
	var threshold float64
	var minSeries int
	var limit int
	cmd := &cobra.Command{
		Use:   "traffic-anomalies",
		Short: "Flag pages whose latest day's sessions deviate from the rolling mean by N stdev (z-score)",
		Long: `Computes per-page z-scores over a rolling window of pages_daily.sessions and
returns pages whose most-recent observation exceeds --threshold standard
deviations from the window's mean.

Reads the local store by default; --data-source live runs one runReport call
and computes the z-scores in-process (slower, but works without a prior sync).`,
		Example:     "  ga4-pp-cli traffic-anomalies --days 30 --threshold 2.0 --agent",
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
				return printOutputWithFlags(cmd.OutOrStdout(), []byte(`{"count":0,"anomalies":[]}`), flags)
			}

			anomalies := computeAnomalies(rows, threshold, minSeries)
			sort.Slice(anomalies, func(i, j int) bool {
				return math.Abs(anomalies[i].ZScore) > math.Abs(anomalies[j].ZScore)
			})
			if limit > 0 && len(anomalies) > limit {
				anomalies = anomalies[:limit]
			}

			out := map[string]any{
				"property":  prop,
				"days":      days,
				"threshold": threshold,
				"count":     len(anomalies),
				"anomalies": anomalies,
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID, numeric (defaults to GA_PROPERTY_ID env var)")
	cmd.Flags().IntVar(&days, "days", 30, "Lookback window in days for the rolling baseline computation")
	cmd.Flags().Float64Var(&threshold, "threshold", 2.0, "Z-score threshold; pages above this magnitude are flagged as anomalies")
	cmd.Flags().IntVar(&minSeries, "min-days", 7, "Minimum days a page must appear in the window to be eligible for scoring")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum anomalies to return, ranked by absolute z-score magnitude")
	return cmd
}

// computeAnomalies groups rows by page_path and computes a z-score on the
// latest day's sessions against the rolling-window mean/stdev. Pages with
// fewer than minSeries observations are skipped: their stdev is too noisy
// to be useful.
func computeAnomalies(rows []store.PageDaily, threshold float64, minSeries int) []Anomaly {
	type series struct {
		title    string
		latest   float64
		latestDt string
		values   []float64
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
		s.values = append(s.values, r.Sessions)
		if r.Date > s.latestDt {
			s.latestDt = r.Date
			s.latest = r.Sessions
		}
	}

	out := []Anomaly{}
	for path, s := range byPath {
		if len(s.values) < minSeries {
			continue
		}
		mean := meanFloat(s.values)
		std := stdevFloat(s.values, mean)
		if std == 0 {
			continue // perfectly flat — no anomaly to flag
		}
		z := (s.latest - mean) / std
		if math.Abs(z) < threshold {
			continue
		}
		out = append(out, Anomaly{
			PagePath:   path,
			PageTitle:  s.title,
			Latest:     s.latest,
			Mean:       mean,
			Stdev:      std,
			ZScore:     roundN(z, 3),
			LatestDate: s.latestDt,
			Days:       len(s.values),
		})
	}
	return out
}
