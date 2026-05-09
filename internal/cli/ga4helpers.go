// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// Helpers shared by hand-authored GA4 commands (pages, funnel, schema, templates,
// drift, watch). Centralized so each command's RunE stays focused on intent.

package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

// dimList parses a comma-separated dimension list ("pagePath,country") into the
// shape runReport expects: [{"name":"pagePath"},{"name":"country"}].
func dimList(csv string) []map[string]any {
	if csv == "" {
		return nil
	}
	out := []map[string]any{}
	for _, name := range strings.Split(csv, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = append(out, map[string]any{"name": name})
	}
	return out
}

// metricList mirrors dimList for metrics.
func metricList(csv string) []map[string]any {
	if csv == "" {
		return nil
	}
	out := []map[string]any{}
	for _, name := range strings.Split(csv, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = append(out, map[string]any{"name": name})
	}
	return out
}

// dateRange takes "7daysAgo,today" or "2026-01-01,2026-01-31" or a single
// preset ("yesterday") and returns the runReport `dateRanges` array.
func dateRange(spec string) []map[string]any {
	if spec == "" {
		return []map[string]any{{"startDate": "30daysAgo", "endDate": "today"}}
	}
	parts := strings.Split(spec, ",")
	if len(parts) == 1 {
		switch strings.ToLower(strings.TrimSpace(parts[0])) {
		case "today":
			return []map[string]any{{"startDate": "today", "endDate": "today"}}
		case "yesterday":
			return []map[string]any{{"startDate": "yesterday", "endDate": "yesterday"}}
		case "7d", "last-7-days", "this-week":
			return []map[string]any{{"startDate": "7daysAgo", "endDate": "today"}}
		case "28d", "last-28-days":
			return []map[string]any{{"startDate": "28daysAgo", "endDate": "today"}}
		case "30d", "last-30-days", "this-month":
			return []map[string]any{{"startDate": "30daysAgo", "endDate": "today"}}
		case "90d":
			return []map[string]any{{"startDate": "90daysAgo", "endDate": "today"}}
		default:
			s := strings.TrimSpace(parts[0])
			return []map[string]any{{"startDate": s, "endDate": s}}
		}
	}
	return []map[string]any{{
		"startDate": strings.TrimSpace(parts[0]),
		"endDate":   strings.TrimSpace(parts[1]),
	}}
}

// pagePathFilter builds a runReport dimensionFilter that matches `pagePath`
// using `EXACT`, `CONTAINS`, `BEGINS_WITH`, `ENDS_WITH`, or `FULL_REGEXP`.
// The MCP we're absorbing only supported EXACT.
func pagePathFilter(value, matchType string) map[string]any {
	matchType = strings.ToUpper(matchType)
	if matchType == "" {
		matchType = "EXACT"
	}
	caseSensitive := false
	return map[string]any{
		"filter": map[string]any{
			"fieldName": "pagePath",
			"stringFilter": map[string]any{
				"matchType":     matchType,
				"value":         value,
				"caseSensitive": caseSensitive,
			},
		},
	}
}

// runReport sends a runReport request and returns the parsed response. The
// caller owns the body shape (dimensions/metrics/dateRanges/etc).
func runReport(c clientForRunReport, property string, body map[string]any) (map[string]any, error) {
	path := "/v1beta/properties/" + property + ":runReport"
	raw, _, err := c.post(path, body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parsing runReport response: %w", err)
	}
	return out, nil
}

// runRealtimeReport mirrors runReport for realtime requests.
func runRealtimeReport(c clientForRunReport, property string, body map[string]any) (map[string]any, error) {
	path := "/v1beta/properties/" + property + ":runRealtimeReport"
	raw, _, err := c.post(path, body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parsing runRealtimeReport response: %w", err)
	}
	return out, nil
}

// batchRunReports posts a /batchRunReports request and returns the array of
// per-report responses.
func batchRunReports(c clientForRunReport, property string, body map[string]any) (map[string]any, error) {
	path := "/v1beta/properties/" + property + ":batchRunReports"
	raw, _, err := c.post(path, body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parsing batchRunReports response: %w", err)
	}
	return out, nil
}

// clientForRunReport is a tiny adapter so this file doesn't need to import
// internal/client and risk a cycle with helpers that import this file.
// The real implementation is in client_adapter.go.
type clientForRunReport interface {
	post(path string, body any) ([]byte, int, error)
	get(path string, params map[string]string) ([]byte, error)
}
