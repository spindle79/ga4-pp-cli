// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `key-events` lists the key events (GA4's modern replacement for
// "conversions") configured on a property. Important for understanding
// which events should drive `conversions` metric in Data API reports.

package cli

import (
	"github.com/spf13/cobra"
)

func newKeyEventsCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "key-events [property]",
		Aliases:     []string{"key-event", "conversions-config"},
		Short:       "List key events (modern GA4 conversions) configured on a property via the Admin API",
		Long:        `Returns every keyEvent on the property. Key events are GA4's renamed replacement for "conversions" — only events flagged as key here populate the conversions metric in Data API reports. Use this command to verify the configuration matches your runReport assumptions.`,
		Example:     "  ga4-pp-cli key-events 12345 --agent",
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
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose key events to list; defaults to GA_PROPERTY_ID")
	cmd.AddCommand(newKeyEventDescribeCmd(flags))
	return cmd
}

// newKeyEventDescribeCmd fetches a single key event by ID. Useful when
// confirming the counting method (once vs every event) for a single
// conversion before running a conversion-shaped runReport.
func newKeyEventDescribeCmd(flags *rootFlags) *cobra.Command {
	var property string
	var eventID string
	cmd := &cobra.Command{
		Use:         "describe",
		Aliases:     []string{"get"},
		Short:       "Fetch a single GA4 key event by ID, including its counting method and event-create timestamp",
		Example:     "  ga4-pp-cli key-events describe --event-id 4321 --property-id 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/keyEvents/"+eventID)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) that owns the key event; defaults to GA_PROPERTY_ID")
	cmd.Flags().StringVar(&eventID, "event-id", "", "Numeric key event ID to fetch; required because there is no useful default")
	_ = cmd.MarkFlagRequired("event-id")
	return cmd
}
