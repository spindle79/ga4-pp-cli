// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `attribution-settings` returns the property's conversion-attribution
// configuration: the current attribution model and the reporting lookback
// windows for acquisition and engagement. Required context for any
// conversion- or acquisition-shaped report.

package cli

import (
	"github.com/spf13/cobra"
)

func newAttributionSettingsCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "attribution-settings [property]",
		Aliases:     []string{"attribution", "attr"},
		Short:       "Show the attribution model and lookback windows for a GA4 property via the Admin API",
		Long:        `Returns the property's reporting attribution model (data-driven, last-click, etc.) and the acquisition / engagement lookback windows. Two GA4 properties with identical events can return wildly different conversion counts if their attribution settings differ — pull this before comparing properties.`,
		Example:     "  ga4-pp-cli attribution-settings 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/attributionSettings")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) to inspect; defaults to GA_PROPERTY_ID")
	return cmd
}
