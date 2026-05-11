// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `analytics` is the umbrella parent that groups the compound analytics
// commands under one discoverable namespace. Top-level aliases for
// back-compatibility are kept (traffic-anomalies, bot-traffic, drift,
// trends) — agents can use either form. The umbrella mostly exists for
// --help discoverability: `ga4-pp-cli analytics --help` lists every
// compound surface in one place.

package cli

import "github.com/spf13/cobra"

func newAnalyticsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analytics",
		Short: "Compound analytics commands (z-score anomalies, bot heuristics, drift, trends)",
		Long: `Groups the compound analytics commands under one discoverable parent so
agents can list every "mine pages_daily for a signal" workflow with a single
--help. Each subcommand is also exposed at the top level for back-compat
with prior versions of this CLI; both forms invoke the same RunE.`,
		Aliases: []string{"analyze"},
		// Hidden from agent-context / MCP listing because every subcommand is
		// also registered at the top level with the same RunE. Showing both
		// forms doubles the tool catalog and forces dogfood to chase
		// `analytics drift pages` examples that — by design — reference the
		// top-level form (`ga4-pp-cli drift pages …`). The umbrella stays
		// discoverable via `--help`; only the agent-facing surface is hidden.
		Hidden: true,
	}
	// Each subcommand is constructed via the existing factory so the
	// alias preserves every flag, default, and MarkFlagRequired the
	// top-level version exposes. We don't try to share a single cobra.Command
	// pointer between two registration paths — cobra wants distinct
	// command trees per parent.
	cmd.AddCommand(
		newTrafficAnomaliesCmd(flags),
		newBotTrafficCmd(flags),
		newDriftCmd(flags),
		newTrendsCmd(flags),
		newHealthCmd(flags),
	)
	return cmd
}
