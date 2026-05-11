// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `data-streams` lists the web, iOS, and Android data streams attached to a
// GA4 property. Useful for verifying which sources are actually sending
// hits before debugging a missing-data report.

package cli

import (
	"github.com/spf13/cobra"
)

func newDataStreamsCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "data-streams [property]",
		Aliases:     []string{"streams", "data-stream"},
		Short:       "List data streams (web, iOS, Android) attached to a GA4 property via the Admin API",
		Long:        `Returns every dataStream on the property along with stream type, name, and measurement ID. Run this first when diagnosing a missing-data report — a property with no active streams will simply return empty results from every Data API call.`,
		Example:     "  ga4-pp-cli data-streams 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/dataStreams")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) whose data streams to list; defaults to GA_PROPERTY_ID")
	cmd.AddCommand(newDataStreamDescribeCmd(flags))
	return cmd
}

// newDataStreamDescribeCmd fetches a single dataStream resource — useful
// when you already know the stream ID and only need its measurement
// secret or stream-specific settings.
func newDataStreamDescribeCmd(flags *rootFlags) *cobra.Command {
	var property string
	var streamID string
	cmd := &cobra.Command{
		Use:         "describe",
		Aliases:     []string{"get"},
		Short:       "Fetch a single GA4 data stream by ID, including its measurement ID and stream-specific settings",
		Example:     "  ga4-pp-cli data-streams describe --stream-id 9876 --property-id 12345 --agent",
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
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/dataStreams/"+streamID)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) that owns the stream; defaults to GA_PROPERTY_ID")
	cmd.Flags().StringVar(&streamID, "stream-id", "", "Numeric data stream ID to fetch; required because there is no useful default")
	_ = cmd.MarkFlagRequired("stream-id")
	return cmd
}
