// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `bot-traffic` is the heuristic scan over pages_daily for suspicious
// traffic patterns (near-zero engagement, drive-by hits, one user with
// many sessions). It's a compound command in the Steinberger sense: one
// CLI call answers "which pages have suspicious patterns?" without the
// agent having to compose and reduce raw runReport responses.

package cli

import (
	"encoding/json"
	"sort"

	"ga4-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

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

func newBotTrafficCmd(flags *rootFlags) *cobra.Command {
	var property string
	var days int
	var minSessions float64
	var limit int
	cmd := &cobra.Command{
		Use:   "bot-traffic",
		Short: "Flag pages whose heuristics suggest bot or scraper traffic from pages_daily",
		Long: `Heuristic scan over pages_daily for suspicious traffic patterns:

  • engagement_rate < 0.1 (near-zero engagement)
  • average_session_duration < 5 seconds (drive-by hits)
  • sessions / total_users > 5 (one user, many sessions)

Each match adds a flag. Pages with two or more flags are returned, ranked by
total sessions in the window. Reads from the local store (run 'sync pages'
first); --data-source live falls back to a single runReport.`,
		Example:     "  ga4-pp-cli bot-traffic --property 12345 --days 7 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if days <= 0 {
				days = 7
			}
			// --property is MarkFlagRequired below; --days is required so
			// the user makes an explicit window choice for the heuristic
			// scan (the right window depends on traffic volume).
			prop := property

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
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID (numeric) the heuristic scan should examine")
	// Next flag, --days, controls the lookback window.
	cmd.Flags().IntVar(&days, "days", 7, "Explicit lookback window in days for the bot-pattern heuristic scan")
	// Next flag, --min-sessions, is the heuristic signal floor.
	cmd.Flags().Float64Var(&minSessions, "min-sessions", 10, "Ignore pages with fewer total sessions in the window (signal floor)")
	// Next flag, --limit, caps the returned result count.
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum suspect pages to return, ranked by total sessions in window")
	// Property + days must be explicit on this command. Bot-traffic is a
	// long-running heuristic that shouldn't silently fall back to env
	// defaults or a 7d window if the operator hasn't thought about scope.
	_ = cmd.MarkFlagRequired("property")
	_ = cmd.MarkFlagRequired("days")
	return cmd
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
