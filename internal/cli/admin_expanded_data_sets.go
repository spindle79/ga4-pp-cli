// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `expanded-data-sets` lists the expanded data sets (high-cardinality
// dimension expansions) configured on a GA4 property. These let the GA4
// UI surface high-cardinality breakdowns that would normally hit the
// (other) bucket — they don't unlock the same in the Data API, but they
// reveal which dimensions are flagged as high-cardinality on the property.

package cli

import (
	"github.com/spf13/cobra"
)

func newExpandedDataSetsCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "expanded-data-sets [property]",
		Aliases:     []string{"eds", "expanded-sets"},
		Short:       "List expanded data sets (high-cardinality dimension expansions) on a GA4 property",
		Long:        `Returns every expandedDataSet on the property. Pull this when you want to know which dimensions GA4 considers high-cardinality enough to need an expansion definition — useful context when picking a runReport dimension that's likely to hit the (other) bucket.`,
		Example:     "  ga4-pp-cli expanded-data-sets 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/expandedDataSets")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose expanded data sets to list; defaults to GA_PROPERTY_ID")
	return cmd
}
