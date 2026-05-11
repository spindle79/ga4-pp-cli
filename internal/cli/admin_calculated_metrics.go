// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `calculated-metrics` lists the calculated metrics defined on a GA4
// property. Calculated metrics are derived from other metrics via
// arithmetic; reports that compute their own derived metrics should
// cross-reference this list before reinventing one locally.

package cli

import (
	"github.com/spf13/cobra"
)

func newCalculatedMetricsCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "calculated-metrics [property]",
		Aliases:     []string{"calc-metrics", "calculated"},
		Short:       "List calculated metrics defined on a GA4 property via the Admin API",
		Long:        `Returns every calculatedMetric on the property along with its formula. Use this before defining a derived metric locally — the property may already carry the same calculation, and using the calculated metric directly produces consistent numbers across runReport, the GA4 UI, and Looker Studio.`,
		Example:     "  ga4-pp-cli calculated-metrics 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/calculatedMetrics")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose calculated metrics to list; defaults to GA_PROPERTY_ID")
	cmd.AddCommand(newCalculatedMetricDescribeCmd(flags))
	return cmd
}

// newCalculatedMetricDescribeCmd fetches a single calculatedMetric by
// ID. Useful when you need the formula or measurement unit of one
// specific metric without paging the full list.
func newCalculatedMetricDescribeCmd(flags *rootFlags) *cobra.Command {
	var property string
	var metricID string
	cmd := &cobra.Command{
		Use:         "describe",
		Aliases:     []string{"get"},
		Short:       "Fetch a single GA4 calculated metric by ID, including its formula and measurement unit",
		Example:     "  ga4-pp-cli calculated-metrics describe --metric-id revenue_per_user --property-id 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/calculatedMetrics/"+metricID)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) that owns the metric; defaults to GA_PROPERTY_ID")
	cmd.Flags().StringVar(&metricID, "metric-id", "", "Calculated metric ID (alphanumeric); required because there is no useful default")
	_ = cmd.MarkFlagRequired("metric-id")
	return cmd
}
