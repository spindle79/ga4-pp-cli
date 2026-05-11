// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// Compound commands that join multiple GA4 signals into one agent-friendly
// answer. They prefer the local store (run 'sync pages' first); --data-source
// live falls back to a single runReport call when available.

package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"ga4-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

// ----------------------------------------------------------------------------
// traffic-anomalies
// ----------------------------------------------------------------------------

func newTrafficAnomaliesCmd(flags *rootFlags) *cobra.Command {
	var property string
	var days int
	var threshold float64
	var minSeries int
	var limit int
	cmd := &cobra.Command{
		Use:     "traffic-anomalies",
		Short:   "Flag pages whose latest day's sessions deviate from the rolling mean by N stdev (z-score)",
		Long: `Computes per-page z-scores over a rolling window of pages_daily.sessions and
returns pages whose most-recent observation exceeds --threshold standard
deviations from the window's mean.

Reads the local store by default; --data-source live runs one runReport call
and computes the z-scores in-process (slower, but works without a prior sync).`,
		Example: "  ga4-pp-cli traffic-anomalies --days 30 --threshold 2.0 --agent",
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
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID (defaults to GA_PROPERTY_ID)")
	cmd.Flags().IntVar(&days, "days", 30, "Lookback window in days")
	cmd.Flags().Float64Var(&threshold, "threshold", 2.0, "Z-score threshold; pages above this are flagged")
	cmd.Flags().IntVar(&minSeries, "min-days", 7, "Minimum days a page must appear in the window to be eligible")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max anomalies to return")
	return cmd
}

// Anomaly is one z-scored page result for the traffic-anomalies command.
type Anomaly struct {
	PagePath  string  `json:"page_path"`
	PageTitle string  `json:"page_title,omitempty"`
	Latest    float64 `json:"latest_sessions"`
	Mean      float64 `json:"mean_sessions"`
	Stdev     float64 `json:"stdev"`
	ZScore    float64 `json:"z_score"`
	LatestDate string `json:"latest_date"`
	Days       int    `json:"days_in_window"`
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

// ----------------------------------------------------------------------------
// bot-traffic
// ----------------------------------------------------------------------------

func newBotTrafficCmd(flags *rootFlags) *cobra.Command {
	var property string
	var days int
	var minSessions float64
	var limit int
	cmd := &cobra.Command{
		Use:     "bot-traffic",
		Short:   "Flag pages whose heuristics suggest bot or scraper traffic from pages_daily",
		Long: `Heuristic scan over pages_daily for suspicious traffic patterns:

  • engagement_rate < 0.1 (near-zero engagement)
  • average_session_duration < 5 seconds (drive-by hits)
  • sessions / total_users > 5 (one user, many sessions)

Each match adds a flag. Pages with two or more flags are returned, ranked by
total sessions in the window. Reads from the local store (run 'sync pages'
first); --data-source live falls back to a single runReport.`,
		Example: "  ga4-pp-cli bot-traffic --days 7 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if days <= 0 {
				days = 7
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

			suspects := computeBotSuspects(rows, minSessions)
			sort.Slice(suspects, func(i, j int) bool {
				return suspects[i].Sessions > suspects[j].Sessions
			})
			if limit > 0 && len(suspects) > limit {
				suspects = suspects[:limit]
			}

			out := map[string]any{
				"property": prop,
				"days":     days,
				"count":    len(suspects),
				"suspects": suspects,
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID (defaults to GA_PROPERTY_ID)")
	cmd.Flags().IntVar(&days, "days", 7, "Lookback window in days")
	cmd.Flags().Float64Var(&minSessions, "min-sessions", 10, "Ignore pages with fewer total sessions in the window")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max suspects to return")
	return cmd
}

// BotSuspect is one ranked entry for the bot-traffic command.
type BotSuspect struct {
	PagePath               string   `json:"page_path"`
	PageTitle              string   `json:"page_title,omitempty"`
	Sessions               float64  `json:"sessions"`
	TotalUsers             float64  `json:"total_users"`
	SessionsPerUser        float64  `json:"sessions_per_user"`
	EngagementRate         float64  `json:"engagement_rate"`
	AverageSessionDuration float64  `json:"average_session_duration"`
	Flags                  []string `json:"flags"`
	FlagCount              int      `json:"flag_count"`
}

func computeBotSuspects(rows []store.PageDaily, minSessions float64) []BotSuspect {
	type agg struct {
		title         string
		sessions      float64
		users         float64
		engaged       float64
		durationTotal float64
		days          int
	}
	byPath := map[string]*agg{}
	for _, r := range rows {
		a := byPath[r.PagePath]
		if a == nil {
			a = &agg{title: r.PageTitle}
			byPath[r.PagePath] = a
		}
		if r.PageTitle != "" {
			a.title = r.PageTitle
		}
		a.sessions += r.Sessions
		a.users += r.TotalUsers
		a.engaged += r.EngagedSessions
		// average_session_duration is already an avg per row; weight by sessions
		// so the aggregate is a sessions-weighted average over the window.
		a.durationTotal += r.AverageSessionDuration * r.Sessions
		a.days++
	}

	out := []BotSuspect{}
	for path, a := range byPath {
		if a.sessions < minSessions {
			continue
		}
		sessionsPerUser := 0.0
		if a.users > 0 {
			sessionsPerUser = a.sessions / a.users
		}
		engagementRate := 0.0
		if a.sessions > 0 {
			engagementRate = a.engaged / a.sessions
		}
		avgDuration := 0.0
		if a.sessions > 0 {
			avgDuration = a.durationTotal / a.sessions
		}

		flags := []string{}
		if engagementRate < 0.1 {
			flags = append(flags, "near_zero_engagement")
		}
		if avgDuration < 5 {
			flags = append(flags, "near_zero_session_duration")
		}
		if sessionsPerUser > 5 {
			flags = append(flags, "high_sessions_per_user")
		}
		if len(flags) < 2 {
			continue
		}
		out = append(out, BotSuspect{
			PagePath:               path,
			PageTitle:              a.title,
			Sessions:               roundN(a.sessions, 2),
			TotalUsers:             roundN(a.users, 2),
			SessionsPerUser:        roundN(sessionsPerUser, 3),
			EngagementRate:         roundN(engagementRate, 3),
			AverageSessionDuration: roundN(avgDuration, 2),
			Flags:                  flags,
			FlagCount:              len(flags),
		})
	}
	return out
}

// ----------------------------------------------------------------------------
// Shared: load pages_daily for the compound commands
// ----------------------------------------------------------------------------

// pagesDailyForCompound resolves data-source to either a local store read or
// a live runReport call. The 'auto' mode prefers local and falls back to live
// if the store is empty or hasn't been synced.
func pagesDailyForCompound(cmd *cobra.Command, flags *rootFlags, propertyID string, days int) ([]store.PageDaily, error) {
	end := time.Now().UTC()
	start := end.Add(-time.Duration(days) * 24 * time.Hour)
	startStr := start.Format("2006-01-02")
	endStr := end.Format("2006-01-02")

	switch flags.dataSource {
	case "local", "auto":
		s, err := store.OpenReadOnly(store.DefaultPath())
		if err == nil {
			rows, err := s.PagesDailyRange(propertyID, startStr, endStr)
			s.Close()
			if err != nil {
				if flags.dataSource == "local" {
					return nil, fmt.Errorf("read pages_daily: %w", err)
				}
			} else if len(rows) > 0 {
				return rows, nil
			}
		} else if flags.dataSource == "local" {
			return nil, fmt.Errorf("open store: %w (run 'ga4-pp-cli sync pages' first)", err)
		}
		// fall through to live for "auto"
		fallthrough
	case "live":
		return pagesDailyFromLive(cmd, flags, propertyID, days)
	}
	return nil, fmt.Errorf("invalid --data-source %q", flags.dataSource)
}

func pagesDailyFromLive(cmd *cobra.Command, flags *rootFlags, propertyID string, days int) ([]store.PageDaily, error) {
	c, err := flags.newClient()
	if err != nil {
		return nil, err
	}
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
			{"startDate": fmt.Sprintf("%ddaysAgo", days), "endDate": "today"},
		},
		"limit":               "100000",
		"returnPropertyQuota": true,
	}
	report, err := runReport(newClientAdapter(c), propertyID, body)
	if err != nil {
		return nil, classifyAPIError(err, flags)
	}
	return pagesDailyFromReport(propertyID, report)
}

// ----------------------------------------------------------------------------
// Math helpers
// ----------------------------------------------------------------------------

func meanFloat(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func stdevFloat(xs []float64, mean float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		d := x - mean
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(xs)-1))
}

func roundN(v float64, n int) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	p := math.Pow(10, float64(n))
	return math.Round(v*p) / p
}

