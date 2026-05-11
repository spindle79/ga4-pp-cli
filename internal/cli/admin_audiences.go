// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `audiences` lists the audiences defined on a GA4 property. The Admin API
// is the only way to inspect audience filter expressions — the Data API
// only lets you filter by audienceResourceName once you have the ID.

package cli

import (
	"github.com/spf13/cobra"
)

func newAudiencesCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "audiences [property]",
		Aliases:     []string{"audience"},
		Short:       "List GA4 audiences defined on a property via the Admin API",
		Long:        `Returns every audience configured on the property, with display name, description, and the full filter expression. Required reading before adding an audience to a runReport dimension filter — the Data API only accepts audience resource names, not display names.`,
		Example:     "  ga4-pp-cli audiences 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/audiences")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.AddCommand(newAudiencesDescribeCmd(flags))
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose audiences to list; defaults to GA_PROPERTY_ID")
	return cmd
}

// newAudiencesDescribeCmd fetches a single audience resource. The audience
// ID is required because there's no useful default — listing every
// audience is what the parent command does.
func newAudiencesDescribeCmd(flags *rootFlags) *cobra.Command {
	var property string
	var audienceID string
	cmd := &cobra.Command{
		Use:         "describe",
		Aliases:     []string{"get"},
		Short:       "Fetch a single GA4 audience by ID, including its filter expression and creation metadata",
		Example:     "  ga4-pp-cli audiences describe --audience-id 123456 --property-id 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/audiences/"+audienceID)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) that owns the audience; defaults to GA_PROPERTY_ID")
	cmd.Flags().StringVar(&audienceID, "audience-id", "", "Numeric audience ID to fetch; required because there is no useful default")
	_ = cmd.MarkFlagRequired("audience-id")
	return cmd
}
