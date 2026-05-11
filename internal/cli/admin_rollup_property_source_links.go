// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `rollup-property-source-links` lists the source-property links on a
// GA4 360 rollup property. Required when investigating a rollup whose
// numbers don't match the union of source-property reports.

package cli

import (
	"github.com/spf13/cobra"
)

func newRollupPropertySourceLinksCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "rollup-property-source-links [property]",
		Aliases:     []string{"rollup-sources"},
		Short:       "List source-property links on a GA4 360 rollup property via the Admin API",
		Long:        `Returns every rollupPropertySourceLink on the property. A rollup property whose totals don't match the union of its source properties almost always has either a missing source link here or a source-side filter that's silently dropping events.`,
		Example:     "  ga4-pp-cli rollup-property-source-links 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/rollupPropertySourceLinks")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose rollup source links to list; defaults to GA_PROPERTY_ID")
	return cmd
}
