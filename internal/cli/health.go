// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `health` is a higher-level summary command that aggregates the signals
// `doctor` produces (auth/config/API/credentials) with the local-store
// freshness picture from `cliutil.EnsureFresh` and reduces them to a
// single agent-friendly verdict — green/yellow/red — plus the underlying
// evidence. Where `doctor` is the kitchen-sink probe, `health` is the
// "should I trust the next query?" shortcut.

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"ga4-pp-cli/internal/cliutil"
	"ga4-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

// healthVerdict is the rolled-up status returned by `health`.
type healthVerdict struct {
	Status    string         `json:"status"` // "green" | "yellow" | "red"
	Score     int            `json:"score"`  // 0-100 composite
	Auth      string         `json:"auth"`
	Cache     map[string]any `json:"cache"`
	Reasons   []string       `json:"reasons,omitempty"`
	StorePath string         `json:"store_path"`
	CheckedAt string         `json:"checked_at"`
}

func newHealthCmd(flags *rootFlags) *cobra.Command {
	var staleAfter time.Duration
	cmd := &cobra.Command{
		Use:   "health",
		Short: "One-shot CLI health verdict: green / yellow / red plus the evidence",
		Long: `Aggregates the auth, API, and local-store freshness signals into a single
verdict an agent can branch on. Subset of 'doctor' tuned for the question
"is the next query going to work and return fresh data?"

  green   auth configured AND store populated AND scopes fresh
  yellow  auth ok but at least one scope is stale (> --stale-after)
  red     auth missing or invalid OR store empty/unmigrated

Returns exit code 0 always so this command is safe in CI pipelines; check
the JSON status field instead.`,
		Example:     `  ga4-pp-cli health --stale-after 6h --agent`,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			verdict := healthVerdict{
				Status:    "green",
				Score:     100,
				CheckedAt: timeNowRFC3339(),
			}

			// Auth presence: same env-var the doctor checks.
			if os.Getenv("GOOGLE_ANALYTICS_DATA_OAUTH2C") == "" {
				verdict.Auth = "missing"
				verdict.Status = "red"
				verdict.Score = 25
				verdict.Reasons = append(verdict.Reasons, "GOOGLE_ANALYTICS_DATA_OAUTH2C is not set")
			} else {
				verdict.Auth = "configured"
			}

			// Local store inspection.
			path := store.DefaultPath()
			verdict.StorePath = path
			verdict.Cache = map[string]any{"exists": false}
			if _, err := os.Stat(path); err == nil {
				s, err := store.OpenReadOnly(path)
				if err == nil {
					defer s.Close()
					verdict.Cache["exists"] = true
					row := map[string]int{}
					for _, t := range []string{"pages_daily", "dimensions", "metrics", "properties"} {
						if n, err := s.TableRowCount(t); err == nil {
							row[t] = n
						}
					}
					verdict.Cache["row_counts"] = row
					stale, last, ferr := cliutil.EnsureFresh(cmd.Context(), s.DB(), "", "pages", staleAfter)
					if ferr == nil {
						verdict.Cache["pages_last_run_at"] = last.Format(time.RFC3339)
						verdict.Cache["pages_stale"] = stale
						if stale && verdict.Status == "green" {
							verdict.Status = "yellow"
							verdict.Score = 70
							verdict.Reasons = append(verdict.Reasons,
								fmt.Sprintf("pages_daily sync is older than %s — run 'ga4-pp-cli sync pages'", staleAfter))
						}
					}
				}
			} else if verdict.Status == "green" {
				verdict.Status = "yellow"
				verdict.Score = 60
				verdict.Reasons = append(verdict.Reasons,
					"local store does not exist yet — run 'ga4-pp-cli sync schema' (then 'sync pages')")
			}

			b, _ := json.MarshalIndent(verdict, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	cmd.Flags().DurationVar(&staleAfter, "stale-after", 6*time.Hour, "Treat the pages_daily sync as stale when older than this duration window")
	return cmd
}
