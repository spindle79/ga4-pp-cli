// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `tail` is the simpler-API sibling of `watch realtime` — a single
// top-level command that streams runRealtimeReport polls as JSONL to
// stdout, one tick per line. Where `watch realtime` exposes the full
// dimension/metric/order configuration surface, `tail` picks sensible
// defaults (activeUsers by unifiedScreenName, top 10, every 30s) so
// the agent can `ga4-pp-cli tail --property 12345` and immediately see
// what's happening live.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func newTailCmd(flags *rootFlags) *cobra.Command {
	var interval time.Duration
	var top int
	var ticks int
	var property string
	cmd := &cobra.Command{
		Use:   "tail",
		Short: "Stream live activeUsers from runRealtimeReport as one JSON line per tick",
		Long: `Polls the GA4 Realtime Report on an interval and writes one JSON object
per tick to stdout. Each line is independently parseable so an agent can
pipe ` + "`ga4-pp-cli tail --agent`" + ` into a line-oriented consumer without
buffering issues.

Defaults are tuned for the "show me what's happening now" question:
  • dimension: unifiedScreenName
  • metric:    activeUsers (highest, descending)
  • limit:     top 10 rows
  • interval:  30s

For richer control (alternative dimensions, custom orderings, response
trimming), use ` + "`ga4-pp-cli watch realtime`" + ` — tail is the
zero-decision streaming front door.`,
		Example:     "  ga4-pp-cli tail --property 12345 --interval 15s --ticks 4 --agent",
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
			adapter := newClientAdapter(c)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			body := map[string]any{
				"dimensions": dimList("unifiedScreenName"),
				"metrics":    metricList("activeUsers"),
				"limit":      fmt.Sprintf("%d", top),
				"orderBys": []map[string]any{
					{"metric": map[string]any{"metricName": "activeUsers"}, "desc": true},
				},
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			tick := 0
			for {
				tick++
				report, rerr := runRealtimeReport(adapter, prop, body)
				payload := map[string]any{
					"tick":      tick,
					"timestamp": timeNowRFC3339(),
					"property":  prop,
				}
				if rerr != nil {
					payload["error"] = rerr.Error()
					_ = enc.Encode(payload)
					return classifyAPIError(rerr, flags)
				}
				rows := []map[string]any{}
				if rs, ok := report["rows"].([]any); ok {
					for i, r := range rs {
						rm, _ := r.(map[string]any)
						dv, _ := rm["dimensionValues"].([]any)
						mv, _ := rm["metricValues"].([]any)
						if len(dv) == 0 || len(mv) == 0 {
							continue
						}
						key, _ := dv[0].(map[string]any)["value"].(string)
						valStr, _ := mv[0].(map[string]any)["value"].(string)
						rows = append(rows, map[string]any{
							"rank":              i + 1,
							"unifiedScreenName": key,
							"activeUsers":       atoi(valStr),
						})
					}
				}
				payload["top"] = rows
				payload["_warnings"] = extractGA4Warnings(report)
				_ = enc.Encode(payload)
				if ticks > 0 && tick >= ticks {
					return nil
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(interval):
				}
			}
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", 30*time.Second, "Polling interval between realtime report ticks; tune for the cadence you want")
	cmd.Flags().IntVar(&top, "top", 10, "Number of top rows to emit per tick, ordered by activeUsers descending")
	cmd.Flags().IntVar(&ticks, "ticks", 0, "Stop after N ticks (0 means run until SIGINT cancels the stream)")
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID (defaults to GA_PROPERTY_ID environment variable)")
	return cmd
}
