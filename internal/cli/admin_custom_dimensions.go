// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `custom-dimensions` lists the custom dimensions configured on a GA4
// property. Distinct from `schema dimensions` (which uses the Data API's
// getMetadata) — the Admin API surface is the authoritative configuration
// store, including archived dimensions and parameter scope.

package cli

import (
	"github.com/spf13/cobra"
)

func newCustomDimensionsCmd(flags *rootFlags) *cobra.Command {
	var property string
	var showArchived bool
	cmd := &cobra.Command{
		Use:         "custom-dimensions [property]",
		Aliases:     []string{"custom-dim", "cdims"},
		Short:       "List custom dimensions configured on a GA4 property via the Admin API",
		Long:        `Returns every customDimension on the property, including archived ones. Use this when 'schema dimensions' is missing a custom name — the Data API's getMetadata only includes active, registered dimensions while the Admin API exposes the full configuration.`,
		Example:     "  ga4-pp-cli custom-dimensions 12345 --agent",
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
			path := "/v1beta/properties/" + prop + "/customDimensions"
			if showArchived {
				path += "?showDeleted=true"
			}
			body, err := adminGet(cmd.Context(), path)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose custom dimensions to list; defaults to GA_PROPERTY_ID")
	cmd.Flags().BoolVar(&showArchived, "show-archived", false, "Include archived (soft-deleted) custom dimensions in the response")
	cmd.AddCommand(newCustomDimensionDescribeCmd(flags))
	return cmd
}

// newCustomDimensionDescribeCmd fetches a single customDimension by ID.
// Useful when you want only one dimension's parameter scope and event
// name without paging through the full list.
func newCustomDimensionDescribeCmd(flags *rootFlags) *cobra.Command {
	var property string
	var dimensionID string
	cmd := &cobra.Command{
		Use:         "describe",
		Aliases:     []string{"get"},
		Short:       "Fetch a single GA4 custom dimension by ID, including its parameter scope and event name",
		Example:     "  ga4-pp-cli custom-dimensions describe --dimension-id 0001 --property-id 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/customDimensions/"+dimensionID)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) that owns the dimension; defaults to GA_PROPERTY_ID")
	cmd.Flags().StringVar(&dimensionID, "dimension-id", "", "Numeric custom dimension ID to fetch; required because there is no useful default")
	_ = cmd.MarkFlagRequired("dimension-id")
	return cmd
}
