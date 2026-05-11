// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `enhanced-measurement` returns the enhanced-measurement settings for a
// GA4 data stream — which automatic events (scrolls, outbound clicks,
// site search, etc.) are enabled. Reports that look like they're missing
// events often have the corresponding enhanced measurement toggle off.

package cli

import (
	"github.com/spf13/cobra"
)

func newEnhancedMeasurementCmd(flags *rootFlags) *cobra.Command {
	var property string
	var streamID string
	cmd := &cobra.Command{
		Use:         "enhanced-measurement",
		Aliases:     []string{"em", "auto-events"},
		Short:       "Show the enhanced measurement (auto-event) settings for a GA4 data stream via the Admin API",
		Long:        `Returns enhancedMeasurementSettings for the data stream — which automatic events GA4 collects (scrolls, outbound clicks, site search, video engagement, file downloads). When a report is missing one of these events, the toggle here is usually off.`,
		Example:     "  ga4-pp-cli enhanced-measurement --property-id 12345 --stream-id 9876 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			prop, err := resolveProperty([]string{property}, flags)
			if err != nil {
				return err
			}
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/dataStreams/"+streamID+"/enhancedMeasurementSettings")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) that owns the data stream; defaults to GA_PROPERTY_ID")
	cmd.Flags().StringVar(&streamID, "stream-id", "", "Numeric data stream ID whose enhanced-measurement settings to fetch; required because settings are per-stream")
	_ = cmd.MarkFlagRequired("stream-id")
	return cmd
}
