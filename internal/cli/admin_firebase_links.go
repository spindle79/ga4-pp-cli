// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `firebase-links` lists the Firebase projects linked to a GA4 property.
// Mobile app properties typically have exactly one link; web-only
// properties have none. The link state controls whether Firebase user
// IDs map back to GA4 user IDs in reports.

package cli

import (
	"github.com/spf13/cobra"
)

func newFirebaseLinksCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "firebase-links [property]",
		Aliases:     []string{"fb-links", "firebase"},
		Short:       "List Firebase projects linked to a GA4 property via the Admin API",
		Long:        `Returns every firebaseLink on the property. Mobile app properties carry exactly one link; web-only properties return an empty list. Cross-reference this list when reconciling user-level metrics between Firebase Analytics and GA4 reports.`,
		Example:     "  ga4-pp-cli firebase-links 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/firebaseLinks")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose Firebase links to list; defaults to GA_PROPERTY_ID")
	return cmd
}
