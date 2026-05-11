// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// Shared infrastructure for the compound commands. Each compound lives in
// its own `<name>_compound.go` file so the file layout matches the
// Steinberger workflow-file convention; the helpers here are the small
// surface they all share (data-source resolution, math primitives).

package cli

import (
	"fmt"
	"math"
	"time"

	"ga4-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

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
// Math helpers — shared by traffic-anomalies and bot-traffic.
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
	return math.Sqrt(sum / float64(len(xs) - 1))
}

func roundN(v float64, n int) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	p := math.Pow(10, float64(n))
	return math.Round(v*p) / p
}
