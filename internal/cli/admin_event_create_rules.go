// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `event-create-rules` lists per-stream event-create rules configured
// on a GA4 data stream. These rules synthesize new events from
// parameter matchers — important when reports show unexpected event
// names that don't appear to be sent by any client SDK.

package cli

import (
	"github.com/spf13/cobra"
)

func newEventCreateRulesCmd(flags *rootFlags) *cobra.Command {
	var property string
	var streamID string
	cmd := &cobra.Command{
		Use:         "event-create-rules",
		Aliases:     []string{"event-rules"},
		Short:       "List event-create rules configured on a GA4 data stream via the Admin API",
		Long:        `Returns every eventCreateRule on the data stream. These rules synthesize new events from parameter matchers; if a report shows event names you don't recognize, the rule that generated them lives here.`,
		Example:     "  ga4-pp-cli event-create-rules --property-id 12345 --stream-id 9876 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			prop, err := resolveProperty([]string{property}, flags)
			if err != nil {
				return err
			}
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/dataStreams/"+streamID+"/eventCreateRules")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) that owns the data stream; defaults to GA_PROPERTY_ID")
	cmd.Flags().StringVar(&streamID, "stream-id", "", "Numeric data stream ID whose event-create rules to list; required because rules are per-stream")
	_ = cmd.MarkFlagRequired("stream-id")
	return cmd
}
