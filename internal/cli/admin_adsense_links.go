// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `adsense-links` lists the AdSense for Content links configured on a
// GA4 property. Required to surface adsense revenue metrics in Data API
// reports — a property with no AdSense link returns zero ad revenue.

package cli

import (
	"github.com/spf13/cobra"
)

func newAdSenseLinksCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "adsense-links [property]",
		Aliases:     []string{"adsense"},
		Short:       "List AdSense for Content links configured on a GA4 property via the Admin API",
		Long:        `Returns every adSenseLink on the property. Without an AdSense link, the Data API surface metrics for AdSense revenue return zero — pull this list before investigating apparently-missing AdSense numbers.`,
		Example:     "  ga4-pp-cli adsense-links 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/adSenseLinks")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose AdSense links to list; defaults to GA_PROPERTY_ID")
	return cmd
}
