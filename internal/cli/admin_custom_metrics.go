// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `custom-metrics` lists the custom metrics configured on a GA4 property.
// The Admin API exposes scope, measurement unit, and archive state — fields
// the Data API metadata endpoint omits.

package cli

import (
	"github.com/spf13/cobra"
)

func newCustomMetricsCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "custom-metrics [property]",
		Aliases:     []string{"custom-met", "cmets"},
		Short:       "List custom metrics configured on a GA4 property via the Admin API",
		Long:        `Returns every customMetric on the property, including measurement unit and archive state. Use this to audit metric configuration before authoring a runReport call — getMetadata only shows the active subset that's safe to query.`,
		Example:     "  ga4-pp-cli custom-metrics 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/customMetrics")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose custom metrics to list; defaults to GA_PROPERTY_ID")
	cmd.AddCommand(newCustomMetricDescribeCmd(flags))
	return cmd
}

// newCustomMetricDescribeCmd fetches a single customMetric by ID,
// including its measurement unit, scope, and restricted-metric flags.
func newCustomMetricDescribeCmd(flags *rootFlags) *cobra.Command {
	var property string
	var metricID string
	cmd := &cobra.Command{
		Use:         "describe",
		Aliases:     []string{"get"},
		Short:       "Fetch a single GA4 custom metric by ID, including its measurement unit and restricted-metric flags",
		Example:     "  ga4-pp-cli custom-metrics describe --metric-id 0001 --property-id 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/customMetrics/"+metricID)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) that owns the metric; defaults to GA_PROPERTY_ID")
	cmd.Flags().StringVar(&metricID, "metric-id", "", "Numeric custom metric ID to fetch; required because there is no useful default")
	_ = cmd.MarkFlagRequired("metric-id")
	return cmd
}
