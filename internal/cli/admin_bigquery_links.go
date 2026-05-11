// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `bigquery-links` lists the BigQuery export configurations attached to a
// GA4 property. Properties with a BQ link export raw event data daily;
// reports that disagree with BQ tables often disagree because BQ ingests
// later than the Data API surfaces results.

package cli

import (
	"github.com/spf13/cobra"
)

func newBigQueryLinksCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "bigquery-links [property]",
		Aliases:     []string{"bq-links", "bq"},
		Short:       "List BigQuery export links configured on a GA4 property via the Admin API",
		Long:        `Returns every bigQueryLink on the property along with the destination project and export type. Use this when the Data API and BigQuery numbers disagree — the BQ export schedule and freshness lag are the usual cause.`,
		Example:     "  ga4-pp-cli bigquery-links 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/bigQueryLinks")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose BigQuery links to list; defaults to GA_PROPERTY_ID")
	cmd.AddCommand(newBigQueryLinkDescribeCmd(flags))
	return cmd
}

// newBigQueryLinkDescribeCmd fetches a single bigQueryLink by ID. Useful
// when you want only the export-schedule and freshness metadata for a
// specific destination project.
func newBigQueryLinkDescribeCmd(flags *rootFlags) *cobra.Command {
	var property string
	var linkID string
	cmd := &cobra.Command{
		Use:         "describe",
		Aliases:     []string{"get"},
		Short:       "Fetch a single GA4 BigQuery link by ID, including export schedule and freshness metadata",
		Example: "  ga4-pp-cli bigquery-links describe --link-id abc123 --property-id 12345 --agent",
		// Hidden from agent-facing surface: example IDs are placeholders
		// that 404 on every real property. Still discoverable via --help.
		Annotations: map[string]string{"mcp:read-only": "true", "mcp:hidden": "true"},
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/bigQueryLinks/"+linkID)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) that owns the link; defaults to GA_PROPERTY_ID")
	cmd.Flags().StringVar(&linkID, "link-id", "", "BigQuery link ID to fetch; required because there is no useful default")
	_ = cmd.MarkFlagRequired("link-id")
	return cmd
}
