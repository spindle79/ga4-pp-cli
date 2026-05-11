// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `channel-groups` lists the channel groupings configured on a GA4
// property. The default grouping is hidden; only the custom and active
// override groupings are surfaced through the Admin API.

package cli

import (
	"github.com/spf13/cobra"
)

func newChannelGroupsCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "channel-groups [property]",
		Aliases:     []string{"channel-group", "channels"},
		Short:       "List custom channel groupings configured on a GA4 property via the Admin API",
		Long:        `Returns every customized channelGroup on the property. The default channel grouping is hidden — only overrides and explicitly-created groupings appear here. Pull this when reconciling acquisition reports that disagree with the GA4 web UI's Channel report.`,
		Example:     "  ga4-pp-cli channel-groups 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/channelGroups")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose channel groupings to inspect; defaults to GA_PROPERTY_ID")
	return cmd
}
