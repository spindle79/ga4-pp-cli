// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `watch realtime` polls runRealtimeReport on an interval, diffs against the
// previous tick, and streams JSON deltas. Designed for agent-friendly
// streaming during launches: each line is a complete JSON object the consumer
// can parse independently.

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

func newWatchCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Streaming polling commands (realtime)",
	}
	cmd.AddCommand(newWatchRealtimeCmd(flags))
	return cmd
}

func newWatchRealtimeCmd(flags *rootFlags) *cobra.Command {
	var interval time.Duration
	var top int
	var ticks int
	var metric string
	var dimension string
	var property string
	cmd := &cobra.Command{
		Use:   "realtime",
		Short: "Poll runRealtimeReport on an interval and stream diffs",
		Long: `Each tick emits a JSON object with: tick number, timestamp, top N rows by
metric, and a ` + "`changes`" + ` array of new entrants and rank shifts since the
previous tick. Press Ctrl-C (SIGINT) to stop, or pass --ticks N for a fixed
number of polls.`,
		Example:     "  ga4-pp-cli watch realtime --interval 30s --top 10 --agent",
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
				"dimensions": dimList(dimension),
				"metrics":    metricList(metric),
				"limit":      fmt.Sprintf("%d", top),
				"orderBys": []map[string]any{
					{"metric": map[string]any{"metricName": metric}, "desc": true},
				},
			}

			prev := map[string]int{}
			prevRank := map[string]int{}
			tick := 0
			for {
				tick++
				report, rerr := runRealtimeReport(adapter, prop, body)
				if rerr != nil {
					emitTick(cmd, map[string]any{
						"tick":      tick,
						"timestamp": timeNowRFC3339(),
						"error":     rerr.Error(),
					})
					return classifyAPIError(rerr, flags)
				}
				rows := []map[string]any{}
				cur := map[string]int{}
				curRank := map[string]int{}
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
						val := atoi(valStr)
						rows = append(rows, map[string]any{
							"rank":    i + 1,
							dimension: key,
							metric:    val,
						})
						cur[key] = val
						curRank[key] = i + 1
					}
				}
				changes := []map[string]any{}
				for k, v := range cur {
					p := prev[k]
					pr := prevRank[k]
					switch {
					case p == 0:
						changes = append(changes, map[string]any{"kind": "entered", dimension: k, "rank": curRank[k], metric: v})
					case curRank[k] != pr:
						changes = append(changes, map[string]any{"kind": "rank_change", dimension: k, "from_rank": pr, "to_rank": curRank[k], "delta": v - p})
					}
				}
				for k := range prev {
					if _, ok := cur[k]; !ok {
						changes = append(changes, map[string]any{"kind": "left", dimension: k})
					}
				}
				emitTick(cmd, map[string]any{
					"tick":      tick,
					"timestamp": timeNowRFC3339(),
					"property":  prop,
					"top":       rows,
					"changes":   changes,
					"_warnings": extractGA4Warnings(report),
				})
				prev = cur
				prevRank = curRank
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
	cmd.Flags().DurationVar(&interval, "interval", 30*time.Second, "Polling interval")
	cmd.Flags().IntVar(&top, "top", 10, "Top N rows to emit per tick")
	cmd.Flags().IntVar(&ticks, "ticks", 0, "Stop after N ticks (0 = run until SIGINT)")
	cmd.Flags().StringVar(&metric, "metric", "activeUsers", "Realtime metric (e.g. activeUsers, screenPageViews)")
	cmd.Flags().StringVar(&dimension, "dimension", "unifiedScreenName", "Realtime dimension (e.g. unifiedScreenName, country)")
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID (defaults to GA_PROPERTY_ID)")
	return cmd
}

func emitTick(cmd *cobra.Command, payload map[string]any) {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}
