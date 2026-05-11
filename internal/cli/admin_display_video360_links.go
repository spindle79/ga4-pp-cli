// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `display-video360-links` lists the Display & Video 360 advertiser
// links configured on a GA4 property. Required for any DV360-attributed
// acquisition report; without a link the dv360CampaignName dimension
// returns "(not set)" rows.

package cli

import (
	"github.com/spf13/cobra"
)

func newDisplayVideo360LinksCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "display-video360-links [property]",
		Aliases:     []string{"dv360-links", "dv360"},
		Short:       "List Display & Video 360 advertiser links on a GA4 property via the Admin API",
		Long:        `Returns every displayVideo360AdvertiserLink on the property. DV360 dimensions in the Data API return "(not set)" rows when no link exists — pull this list first when DV360 attribution looks wrong.`,
		Example:     "  ga4-pp-cli display-video360-links 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/displayVideo360AdvertiserLinks")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose DV360 links to list; defaults to GA_PROPERTY_ID")
	return cmd
}
