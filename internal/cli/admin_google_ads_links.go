// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `google-ads-links` lists the Google Ads accounts linked to a GA4
// property. Required for any acquisition report that joins on ad cost or
// click data — an unlinked property cannot return campaign-level revenue
// metrics from the Data API.

package cli

import (
	"github.com/spf13/cobra"
)

func newGoogleAdsLinksCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "google-ads-links [property]",
		Aliases:     []string{"ads-links", "gads-links"},
		Short:       "List Google Ads accounts linked to a GA4 property via the Admin API",
		Long:        `Returns every googleAdsLink on the property along with the linked customer ID and link state. Use this before authoring acquisition reports — runReport will silently return zero ad-cost rows when no link exists, which is easy to mistake for a metric error.`,
		Example:     "  ga4-pp-cli google-ads-links 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/googleAdsLinks")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose Google Ads links to list; defaults to GA_PROPERTY_ID")
	return cmd
}
