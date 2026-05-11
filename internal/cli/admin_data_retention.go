// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `data-retention` returns the user-data retention configuration for a GA4
// property. Properties retaining 2 months of user data versus 14 months
// return dramatically different cohort and retention metrics for
// older date ranges.

package cli

import (
	"github.com/spf13/cobra"
)

func newDataRetentionCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "data-retention [property]",
		Aliases:     []string{"retention"},
		Short:       "Show event-data retention configuration for a GA4 property via the Admin API",
		Long:        `Returns the property's user-data retention window (2 months vs 14 months) and the reset-on-new-activity flag. Cohort and retention reports for older date ranges silently truncate when this window expires — pull this when historical numbers don't match the GA4 UI.`,
		Example:     "  ga4-pp-cli data-retention 12345 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			argv := args
			if len(argv) == 0 && property != "" {
				argv = []string{property}
			}
			prop, err := resolveProperty(argv, flags)
			if err != nil {
				return err
			}
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/dataRetentionSettings")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose data retention to inspect; defaults to GA_PROPERTY_ID")
	return cmd
}
