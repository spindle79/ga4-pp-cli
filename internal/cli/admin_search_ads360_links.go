// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `search-ads360-links` lists the Search Ads 360 advertiser links on a
// property. Same diagnostic role as the DV360 listing — required to
// understand why a Search Ads 360 dimension returns "(not set)" rows.

package cli

import (
	"github.com/spf13/cobra"
)

func newSearchAds360LinksCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "search-ads360-links [property]",
		Aliases:     []string{"sa360-links", "sa360"},
		Short:       "List Search Ads 360 advertiser links on a GA4 property via the Admin API",
		Long:        `Returns every searchAds360Link on the property. Cross-reference this list when reconciling SA360 acquisition dimensions that return "(not set)" — usually a missing link, not a Data API bug.`,
		Example:     "  ga4-pp-cli search-ads360-links 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/searchAds360Links")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose Search Ads 360 links to list; defaults to GA_PROPERTY_ID")
	return cmd
}
