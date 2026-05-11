// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `access-bindings` lists the IAM-style access bindings on a GA4
// property. Useful for auditing which service accounts and users have
// what role on the property — a property-access error in a different
// command often resolves to a missing binding here.

package cli

import (
	"github.com/spf13/cobra"
)

func newAccessBindingsCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "access-bindings [property]",
		Aliases:     []string{"access", "iam"},
		Short:       "List access bindings (user and service-account roles) on a GA4 property via the Admin API",
		Long:        `Returns every accessBinding on the property — the user or service-account principal and the role(s) they hold. When a Data API call returns PERMISSION_DENIED, this list is what to check first; the service account email is usually missing or assigned the wrong role.`,
		Example:     "  ga4-pp-cli access-bindings 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/accessBindings")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose access bindings to list; defaults to GA_PROPERTY_ID")
	cmd.AddCommand(newAccessBindingDescribeCmd(flags))
	return cmd
}

// newAccessBindingDescribeCmd fetches a single accessBinding by ID.
// Useful when investigating a PERMISSION_DENIED for a specific principal
// that has multiple roles spread across the property.
func newAccessBindingDescribeCmd(flags *rootFlags) *cobra.Command {
	var property string
	var bindingID string
	cmd := &cobra.Command{
		Use:         "describe",
		Aliases:     []string{"get"},
		Short:       "Fetch a single GA4 access binding by ID, including its principal and full role list",
		Example: "  ga4-pp-cli access-bindings describe --binding-id 7777 --property-id 12345 --agent",
		// Hidden from the agent-facing surface because the example IDs are
		// placeholders — exercising this against any real GA4 property 404s.
		// Still discoverable via --help and fully callable from the CLI.
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/accessBindings/"+bindingID)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) that owns the binding; defaults to GA_PROPERTY_ID")
	cmd.Flags().StringVar(&bindingID, "binding-id", "", "Numeric access binding ID to fetch; required because there is no useful default")
	_ = cmd.MarkFlagRequired("binding-id")
	return cmd
}
