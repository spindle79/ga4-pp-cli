// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `accounts` exposes read-only GA4 Admin API endpoints needed for property
// discovery. Bypasses the generated client because the Admin API lives on a
// different host (analyticsadmin.googleapis.com vs analyticsdata) and the
// generated client is hard-coded to the Data API base URL.
//
// Auth reuses googleauth.Token, since the analytics.readonly scope already
// granted by Phase 0 covers both APIs. No new credentials needed.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ga4-pp-cli/internal/googleauth"
	"github.com/spf13/cobra"
)

const adminBase = "https://analyticsadmin.googleapis.com"

// adminGet hits the Admin API with the current bearer token and returns the
// raw JSON body. Errors are wrapped with HTTP status for typed exit-code
// classification by the existing classifyAPIError helper.
func adminGet(ctx context.Context, path string) ([]byte, error) {
	tok, _, err := googleauth.Token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adminBase+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("User-Agent", "ga4-pp-cli/v1beta")
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("admin GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading admin response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("admin GET %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}
	return body, nil
}

func newAccountsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "accounts",
		Aliases: []string{"account"},
		Short:   "GA4 Admin API: list accessible accounts and discover properties",
		Long: `Read-only access to the GA4 Admin API for property/account discovery.
Auth reuses the same service-account token used by the Data API; the
service account needs at minimum Viewer access on the account or
property to appear in the response.`,
	}
	cmd.AddCommand(
		newAccountsListCmd(flags),
		newAccountsSummariesCmd(flags),
	)
	return cmd
}

func newAccountsListCmd(flags *rootFlags) *cobra.Command {
	var pageSize int
	cmd := &cobra.Command{
		Use:         "list",
		Short:       "List GA4 accounts the service account can access",
		Example:     "  ga4-pp-cli accounts list --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			path := "/v1beta/accounts"
			if pageSize > 0 {
				path += fmt.Sprintf("?pageSize=%d", pageSize)
			}
			body, err := adminGet(cmd.Context(), path)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	cmd.Flags().IntVar(&pageSize, "page-size", 0, "Max accounts per page (0 = API default)")
	return cmd
}

func newAccountsSummariesCmd(flags *rootFlags) *cobra.Command {
	var pageSize int
	var includePropertyIds bool
	cmd := &cobra.Command{
		Use:     "summaries",
		Aliases: []string{"summary", "tree"},
		Short:   "List accounts AND their child GA4 properties (the property-discovery command)",
		Long: `Returns each accessible account along with the GA4 properties under it
(displayName + propertyId + parent). One call covers full property
discovery — agents should run this first, then pass the chosen
propertyId to any Data API command via --property.`,
		Example: `  ga4-pp-cli accounts summaries --agent
  ga4-pp-cli accounts summaries --ids-only --agent`,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			path := "/v1beta/accountSummaries"
			if pageSize > 0 {
				path += fmt.Sprintf("?pageSize=%d", pageSize)
			}
			body, err := adminGet(cmd.Context(), path)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			if !includePropertyIds {
				return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
			}
			// Project to a flat list of {propertyId, displayName, account, accountDisplayName}
			// — the tightest agent-friendly shape for property discovery.
			var raw struct {
				AccountSummaries []struct {
					Account           string `json:"account"`
					DisplayName       string `json:"displayName"`
					Name              string `json:"name"`
					PropertySummaries []struct {
						Property     string `json:"property"`
						DisplayName  string `json:"displayName"`
						PropertyType string `json:"propertyType"`
						Parent       string `json:"parent"`
					} `json:"propertySummaries"`
				} `json:"accountSummaries"`
			}
			if err := json.Unmarshal(body, &raw); err != nil {
				return fmt.Errorf("parsing accountSummaries: %w", err)
			}
			rows := []map[string]any{}
			for _, a := range raw.AccountSummaries {
				for _, p := range a.PropertySummaries {
					rows = append(rows, map[string]any{
						"propertyId":         strings.TrimPrefix(p.Property, "properties/"),
						"propertyName":       p.DisplayName,
						"propertyType":       p.PropertyType,
						"account":            a.Account,
						"accountDisplayName": a.DisplayName,
					})
				}
			}
			out, _ := json.MarshalIndent(map[string]any{
				"count":      len(rows),
				"properties": rows,
			}, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), out, flags)
		},
	}
	cmd.Flags().IntVar(&pageSize, "page-size", 0, "Max account summaries per page (0 = API default)")
	cmd.Flags().BoolVar(&includePropertyIds, "ids-only", false, "Flatten to {propertyId, propertyName, account} rows for direct agent consumption")
	return cmd
}

// newPropertiesDescribeCmd is wired alongside the spec-generated properties
// subcommands. Lives in admin.go because it talks to the Admin API host, not
// the Data API host the generator targeted.
func newPropertiesDescribeCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "describe [property]",
		Aliases:     []string{"info", "details"},
		Short:       "GA4 Admin API: full property metadata (display name, time zone, currency, account)",
		Example:     "  ga4-pp-cli properties describe 123456789 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			prop, err := resolveProperty(args, flags)
			if err != nil {
				return err
			}
			body, err := adminGet(cmd.Context(), "/v1beta/properties/"+prop)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), body, flags)
		},
	}
	return cmd
}
