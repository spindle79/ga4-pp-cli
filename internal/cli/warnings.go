// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"strings"
)

// extractGA4Warnings inspects a runReport / batchRunReports response and
// surfaces three GA4-specific data-quality signals as a `_warnings` slice the
// caller can attach to its agent-mode JSON envelope:
//
//   - sampling_active        — metadata.samplingMetadatas[*].samplesReadCount/samplingSpaceSize present
//   - other_bucket=<pct>%    — metadata.dataLossFromOtherRow == true
//   - quota_low=<remaining>  — propertyQuota.tokensPerHour.remaining < 25 OR concurrentRequests near limit
//
// Returns nil when no signal is present.
func extractGA4Warnings(report map[string]any) []string {
	if report == nil {
		return nil
	}
	var out []string

	// Sampling — present in metadata.samplingMetadatas
	if md, ok := report["metadata"].(map[string]any); ok {
		if sms, ok := md["samplingMetadatas"].([]any); ok && len(sms) > 0 {
			for _, sm := range sms {
				if m, ok := sm.(map[string]any); ok {
					if read, _ := m["samplesReadCount"].(string); read != "" {
						out = append(out, fmt.Sprintf("sampling_active=%s_of_%v", read, m["samplingSpaceSize"]))
						break
					}
				}
			}
		}
		// (other) bucket
		if v, ok := md["dataLossFromOtherRow"].(bool); ok && v {
			rows, _ := report["rowCount"].(float64)
			out = append(out, fmt.Sprintf("other_bucket_overflow rows=%d", int(rows)))
		}
		// time zone & currency hints are informational, not warnings.
	}

	// Quota
	if quota, ok := report["propertyQuota"].(map[string]any); ok {
		if t, ok := quota["tokensPerHour"].(map[string]any); ok {
			if r, ok := t["remaining"].(float64); ok && r > 0 && r < 25 {
				out = append(out, fmt.Sprintf("quota_tokens_low=%d_per_hour", int(r)))
			}
		}
		if c, ok := quota["concurrentRequests"].(map[string]any); ok {
			if r, ok := c["remaining"].(float64); ok && r > 0 && r < 5 {
				out = append(out, fmt.Sprintf("quota_concurrent_low=%d", int(r)))
			}
		}
	}

	return out
}

// attachWarnings folds extracted warnings into the JSON envelope returned to
// the caller, under `_warnings` so jq-style filtering stays out of the data.
func attachWarnings(report map[string]any) map[string]any {
	if report == nil {
		return nil
	}
	w := extractGA4Warnings(report)
	if len(w) == 0 {
		return report
	}
	report["_warnings"] = w
	return report
}

// formatWarnings stringifies a warning slice for human (non-JSON) output.
func formatWarnings(w []string) string {
	if len(w) == 0 {
		return ""
	}
	return "  warnings: " + strings.Join(w, ", ")
}
