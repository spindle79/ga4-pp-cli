// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `reports` is the parent for hand-authored report compositions that don't
// map 1:1 to a single Data API endpoint (funnel today; future: cohort, etc).
// The raw 7-endpoint surface lives under `properties` (generator emitted).

package cli

import "github.com/spf13/cobra"

func newReportsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reports",
		Short: "Composed reports built on the GA4 Data API (funnel, more coming)",
	}
	cmd.AddCommand(newReportsFunnelCmd(flags))
	return cmd
}
