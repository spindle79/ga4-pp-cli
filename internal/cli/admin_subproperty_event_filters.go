// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `subproperty-event-filters` lists the subproperty event filters
// attached to a GA4 subproperty (360-only). Useful when reconciling a
// subproperty's narrower event set against the parent property's full
// firehose.

package cli

import (
	"github.com/spf13/cobra"
)

func newSubpropertyEventFiltersCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "subproperty-event-filters [property]",
		Aliases:     []string{"subprop-filters"},
		Short:       "List subproperty event filters attached to a GA4 360 subproperty via the Admin API",
		Long:        `Returns every subpropertyEventFilter on the property. GA4 360 subproperties carry a narrower event set than the parent — this filter list explains which events make it through.`,
		Example:     "  ga4-pp-cli subproperty-event-filters 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/subpropertyEventFilters")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose subproperty filters to list; defaults to GA_PROPERTY_ID")
	return cmd
}
