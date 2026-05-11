// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"ga4-pp-cli/internal/config"
	"ga4-pp-cli/internal/googleauth"
	"github.com/spf13/cobra"
)

func newAuthCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage Google Analytics Data API authentication",
		Long: `GA4 authenticates via Google Application Default Credentials (ADC).

The fastest path is a service-account JSON:

  1. Create a service account in the GCP project that owns the GA4 property
  2. Download its JSON key file
  3. Add the service account email as a Viewer on the GA4 property
     (Admin → Property Access Management → Add user)
  4. export GOOGLE_APPLICATION_CREDENTIALS=/absolute/path/to/sa.json
  5. export GA_PROPERTY_ID=<your numeric property id>

ADC also picks up gcloud user credentials and the GCE metadata server,
so any of those will work without further config.`,
	}

	cmd.AddCommand(newAuthLoginCmd(flags))
	cmd.AddCommand(newAuthStatusCmd(flags))
	cmd.AddCommand(newAuthLogoutCmd(flags))

	return cmd
}

func newAuthLoginCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Verify ADC credentials are reachable and mintable",
		Long: `Verifies the GOOGLE_APPLICATION_CREDENTIALS path (or any other ADC
source) by attempting to mint a real bearer token. No browser flow.

If verification fails, the command prints exactly which ADC step failed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(flags.configPath)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if !googleauth.Available() {
				if flags.asJSON {
					return printJSONFiltered(out, map[string]any{
						"ok":     false,
						"reason": "no Application Default Credentials found",
						"hint":   "set GOOGLE_APPLICATION_CREDENTIALS to a service-account JSON path, or run `gcloud auth application-default login`",
					}, flags)
				}
				fmt.Fprintf(out, "%s No Google Application Default Credentials found.\n\n", red("FAIL"))
				fmt.Fprintln(out, "Set one of:")
				fmt.Fprintln(out, "  export GOOGLE_APPLICATION_CREDENTIALS=/path/to/sa.json")
				fmt.Fprintln(out, "  gcloud auth application-default login")
				return fmt.Errorf("no credentials")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			tok, src, err := googleauth.Token(ctx)
			if err != nil {
				if flags.asJSON {
					return printJSONFiltered(out, map[string]any{
						"ok":     false,
						"reason": err.Error(),
						"source": src,
					}, flags)
				}
				return fmt.Errorf("%s minting token from %s: %w", red("FAIL"), src, err)
			}

			if flags.asJSON {
				return printJSONFiltered(out, map[string]any{
					"ok":          true,
					"source":      src,
					"expires_at":  tok.Expiry.Format(time.RFC3339),
					"property_id": cfg.PropertyID,
				}, flags)
			}
			fmt.Fprintf(out, "%s Token minted (expires %s)\n", green("OK"), tok.Expiry.Format(time.RFC3339))
			fmt.Fprintf(out, "  source: %s\n", src)
			if cfg.PropertyID != "" {
				fmt.Fprintf(out, "  property: %s\n", cfg.PropertyID)
			} else {
				fmt.Fprintf(out, "  property: %s GA_PROPERTY_ID is unset; pass --property on each call\n", yellow("WARN"))
			}
			return nil
		},
	}
	return cmd
}

func newAuthStatusCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current credentials and property",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(flags.configPath)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			adc := googleauth.Available()
			source := cfg.AuthSource
			if adc && source == "" {
				source = "google_adc"
			}
			if cfg.AuthHeaderVal != "" {
				source = "config:auth_header"
			}

			report := map[string]any{
				"authenticated":    adc || cfg.AccessToken != "" || cfg.AuthHeaderVal != "" || cfg.GoogleAnalyticsDataOauth2c != "",
				"source":           source,
				"google_adc_ready": adc,
				"credentials_path": cfg.CredentialsPath,
				"property_id":      cfg.PropertyID,
				"config_path":      cfg.Path,
			}

			if flags.asJSON {
				return printJSONFiltered(out, report, flags)
			}

			if !report["authenticated"].(bool) {
				fmt.Fprintf(out, "%s Not authenticated.\n", red("FAIL"))
				fmt.Fprintln(out, "  Run `ga4-pp-cli auth login` for setup instructions.")
				return nil
			}
			fmt.Fprintf(out, "%s Authenticated\n", green("OK"))
			fmt.Fprintf(out, "  source: %s\n", source)
			if cfg.CredentialsPath != "" {
				fmt.Fprintf(out, "  credentials: %s\n", cfg.CredentialsPath)
			}
			if cfg.PropertyID != "" {
				fmt.Fprintf(out, "  property: %s\n", cfg.PropertyID)
			} else {
				fmt.Fprintf(out, "  property: %s GA_PROPERTY_ID unset\n", yellow("WARN"))
			}
			return nil
		},
	}
}

func newAuthLogoutCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear cached tokens (does not unset env vars)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(flags.configPath)
			if err != nil {
				return err
			}
			if err := cfg.ClearTokens(); err != nil {
				return fmt.Errorf("clearing config tokens: %w", err)
			}
			googleauth.Reset()

			if flags.asJSON {
				return printJSONFiltered(cmd.OutOrStdout(), map[string]any{
					"cleared": true,
					"note":    "Env vars (GOOGLE_APPLICATION_CREDENTIALS, GA_PROPERTY_ID) are unchanged; unset them in your shell to fully sign out.",
				}, flags)
			}
			fmt.Fprintln(os.Stderr, "Cleared config tokens. Env vars unchanged.")
			return nil
		},
	}
}
