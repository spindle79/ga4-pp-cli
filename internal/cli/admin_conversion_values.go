// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `conversion-values` lists conversion default-value settings on a GA4
// property. Each key event can carry a default monetary value; reports
// that compute revenue per conversion should align with these defaults
// when the event itself doesn't carry a value parameter.

package cli

import (
	"github.com/spf13/cobra"
)

func newConversionValuesCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "conversion-values [property]",
		Aliases:     []string{"conv-values"},
		Short:       "List conversion default-value settings (key event defaults) on a GA4 property",
		Long:        `Returns conversion default values for the property's key events. When a key event lacks a value parameter, GA4 uses these defaults — pull this list to understand why a conversion has the revenue it does in a runReport response.`,
		Example:     "  ga4-pp-cli conversion-values 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/keyEvents")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose conversion defaults to inspect; defaults to GA_PROPERTY_ID")
	return cmd
}
