// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `measurement-protocol-secrets` lists the Measurement Protocol API
// secrets configured for a GA4 data stream. These secrets authenticate
// server-side or test event sends; rotation needs the secret ID, which
// only the Admin API surfaces.

package cli

import (
	"github.com/spf13/cobra"
)

func newMeasurementProtocolSecretsCmd(flags *rootFlags) *cobra.Command {
	var property string
	var streamID string
	cmd := &cobra.Command{
		Use:         "measurement-protocol-secrets",
		Aliases:     []string{"mp-secrets"},
		Short:       "List Measurement Protocol API secrets on a GA4 data stream via the Admin API",
		Long:        `Returns every measurementProtocolSecret on the data stream. The secret_value is masked in the response; this listing is used to obtain secret IDs for rotation or revocation, not to retrieve secret material.`,
		Example:     "  ga4-pp-cli measurement-protocol-secrets --property-id 12345 --stream-id 9876 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			prop, err := resolveProperty([]string{property}, flags)
			if err != nil {
				return err
			}
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop+"/dataStreams/"+streamID+"/measurementProtocolSecrets")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property-id", "", "GA4 property ID (numeric, no 'properties/' prefix) that owns the data stream; defaults to GA_PROPERTY_ID")
	cmd.Flags().StringVar(&streamID, "stream-id", "", "Numeric data stream ID whose Measurement Protocol secrets to list; required because secrets are per-stream")
	_ = cmd.MarkFlagRequired("stream-id")
	return cmd
}
