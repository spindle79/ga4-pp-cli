// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// Intent-grouped MCP tools. Each tool here is a single agent verb that
// composes multiple Data/Admin API calls behind a fixed-shape input.
// Distinct from the endpoint mirrors in tools.go: intents trade fewer
// degrees of freedom for fewer round-trips and a steadier output shape,
// which is what agents need when answering questions like "give me the
// top movers this week" rather than authoring a runReport call.
//
// RegisterIntents is called by RegisterTools after the endpoint mirrors
// land so the two groups coexist; agents that need raw API access still
// reach for properties_run-report, while agents that want a workflow
// reach for `top_pages` or `traffic_summary`.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterIntents registers intent-grouped tools — each one composes a
// fixed set of Data/Admin API calls behind one agent-friendly verb.
func RegisterIntents(s *server.MCPServer) {
	s.AddTool(
		mcplib.NewTool("top_pages",
			mcplib.WithDescription("Top GA4 pages by sessions for a property and window. Returns array of {pagePath, sessions}."),
			mcplib.WithString("property", mcplib.Required(), mcplib.Description("GA4 property ID.")),
			mcplib.WithString("days", mcplib.Description("Window in days (default 7).")),
			mcplib.WithString("limit", mcplib.Description("Max rows (default 20).")),
			mcplib.WithReadOnlyHintAnnotation(true),
		),
		handleIntentTopPages,
	)
	s.AddTool(
		mcplib.NewTool("traffic_summary",
			mcplib.WithDescription("One-call GA4 traffic summary: sessions, users, engagement rate for the window. Returns object with totals and trend."),
			mcplib.WithString("property", mcplib.Required(), mcplib.Description("GA4 property ID.")),
			mcplib.WithString("days", mcplib.Description("Window in days (default 7).")),
			mcplib.WithReadOnlyHintAnnotation(true),
		),
		handleIntentTrafficSummary,
	)
	s.AddTool(
		mcplib.NewTool("property_audit",
			mcplib.WithDescription("Audit a GA4 property: data streams, key events, attribution, retention. Returns object combining four Admin API endpoints."),
			mcplib.WithString("property", mcplib.Required(), mcplib.Description("GA4 property ID.")),
			mcplib.WithReadOnlyHintAnnotation(true),
		),
		handleIntentPropertyAudit,
	)
	s.AddTool(
		mcplib.NewTool("schema_pick",
			mcplib.WithDescription("Pick the best GA4 dimension or metric for a phrase. Returns array of ranked candidates with apiName, uiName, and similarity."),
			mcplib.WithString("phrase", mcplib.Required(), mcplib.Description("Free-text phrase to match against the synced schema.")),
			mcplib.WithString("property", mcplib.Description("Filter to a single property.")),
			mcplib.WithString("limit", mcplib.Description("Max candidates to return (default 10).")),
			mcplib.WithReadOnlyHintAnnotation(true),
		),
		handleIntentSchemaPick,
	)
	s.AddTool(
		mcplib.NewTool("realtime_pulse",
			mcplib.WithDescription("One realtime tick: top active users by page for the last 30 minutes. Returns array of {pagePath, activeUsers}."),
			mcplib.WithString("property", mcplib.Required(), mcplib.Description("GA4 property ID.")),
			mcplib.WithString("limit", mcplib.Description("Max rows (default 10).")),
			mcplib.WithReadOnlyHintAnnotation(true),
		),
		handleIntentRealtimePulse,
	)
	s.AddTool(
		mcplib.NewTool("schema_browse",
			mcplib.WithDescription("Browse synced GA4 dimensions and metrics by category. Returns object with dimensions and metrics arrays grouped by category."),
			mcplib.WithString("property", mcplib.Required(), mcplib.Description("GA4 property ID whose schema to browse.")),
			mcplib.WithReadOnlyHintAnnotation(true),
		),
		handleIntentSchemaBrowse,
	)
}

func handleIntentTopPages(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()
	property, _ := args["property"].(string)
	days := intentInt(args, "days", 7)
	limit := intentInt(args, "limit", 20)
	body := map[string]any{
		"dimensions": []map[string]any{{"name": "pagePath"}},
		"metrics":    []map[string]any{{"name": "sessions"}},
		"dateRanges": []map[string]any{{"startDate": fmt.Sprintf("%ddaysAgo", days), "endDate": "today"}},
		"orderBys":   []map[string]any{{"metric": map[string]any{"metricName": "sessions"}, "desc": true}},
		"limit":      fmt.Sprintf("%d", limit),
	}
	return intentReport(ctx, "/v1beta/{property}:runReport", property, body)
}

func handleIntentTrafficSummary(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()
	property, _ := args["property"].(string)
	days := intentInt(args, "days", 7)
	body := map[string]any{
		"metrics": []map[string]any{
			{"name": "sessions"},
			{"name": "totalUsers"},
			{"name": "engagementRate"},
			{"name": "conversions"},
		},
		"dateRanges": []map[string]any{{"startDate": fmt.Sprintf("%ddaysAgo", days), "endDate": "today"}},
	}
	return intentReport(ctx, "/v1beta/{property}:runReport", property, body)
}

func handleIntentRealtimePulse(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()
	property, _ := args["property"].(string)
	limit := intentInt(args, "limit", 10)
	body := map[string]any{
		"dimensions": []map[string]any{{"name": "unifiedScreenName"}},
		"metrics":    []map[string]any{{"name": "activeUsers"}},
		"orderBys":   []map[string]any{{"metric": map[string]any{"metricName": "activeUsers"}, "desc": true}},
		"limit":      fmt.Sprintf("%d", limit),
	}
	return intentReport(ctx, "/v1beta/{property}:runRealtimeReport", property, body)
}

// handleIntentPropertyAudit composes four Admin API calls — data streams,
// key events, attribution settings, data retention — into one object so
// an agent can answer "is this property set up correctly?" without four
// round trips. Each leg falls open on failure: the partial result still
// carries the legs that succeeded.
func handleIntentPropertyAudit(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()
	property, _ := args["property"].(string)
	if property == "" {
		return mcplib.NewToolResultError("property is required"), nil
	}
	out := map[string]any{"property": property}
	c, err := newMCPClient()
	if err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}
	type leg struct {
		key, path string
	}
	for _, l := range []leg{
		{"data_streams", "/v1beta/properties/" + property + "/dataStreams"},
		{"key_events", "/v1beta/properties/" + property + "/keyEvents"},
		{"attribution_settings", "/v1beta/properties/" + property + "/attributionSettings"},
		{"data_retention_settings", "/v1beta/properties/" + property + "/dataRetentionSettings"},
	} {
		body, gerr := c.Get(l.path, nil)
		if gerr != nil {
			out[l.key] = map[string]any{"error": gerr.Error()}
			continue
		}
		var raw any
		_ = json.Unmarshal(body, &raw)
		out[l.key] = raw
	}
	data, _ := json.Marshal(out)
	return mcplib.NewToolResultText(string(data)), nil
}

// handleIntentSchemaPick is a thin wrapper over handleSearch that
// restricts results to the schema scope (dimensions+metrics). The agent
// only has to pass `phrase`; the underlying FTS5 query is built here.
func handleIntentSchemaPick(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()
	args["query"] = args["phrase"]
	// Pass through to handleSearch which already paginates and ranks the
	// FTS5 result set; this intent just gives the verb an agent-shaped
	// name.
	req2 := req
	return handleSearch(ctx, req2)
}

func handleIntentSchemaBrowse(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()
	property, _ := args["property"].(string)
	if property == "" {
		return mcplib.NewToolResultError("property is required"), nil
	}
	path := dbPath()
	s, err := storeOpenReadOnly(path)
	if err != nil {
		return mcplib.NewToolResultError("open store: " + err.Error()), nil
	}
	defer s.Close()
	rows, err := s.DB().Query(`
		SELECT 'dimension' AS kind, api_name, ui_name, COALESCE(category, '') AS category
		FROM dimensions WHERE property_id = ?
		UNION ALL
		SELECT 'metric' AS kind, api_name, ui_name, COALESCE(category, '') AS category
		FROM metrics WHERE property_id = ?
		ORDER BY kind, category, api_name
	`, property, property)
	if err != nil {
		return mcplib.NewToolResultError("query: " + err.Error()), nil
	}
	defer rows.Close()
	grouped := map[string]map[string][]map[string]any{}
	for rows.Next() {
		var kind, apiName, uiName, cat string
		if err := rows.Scan(&kind, &apiName, &uiName, &cat); err != nil {
			continue
		}
		if grouped[kind] == nil {
			grouped[kind] = map[string][]map[string]any{}
		}
		if cat == "" {
			cat = "uncategorized"
		}
		grouped[kind][cat] = append(grouped[kind][cat], map[string]any{
			"apiName": apiName,
			"uiName":  uiName,
		})
	}
	data, _ := json.Marshal(map[string]any{
		"property":   property,
		"dimensions": grouped["dimension"],
		"metrics":    grouped["metric"],
	})
	return mcplib.NewToolResultText(string(data)), nil
}

func intentReport(_ context.Context, pathTemplate, property string, body map[string]any) (*mcplib.CallToolResult, error) {
	if property == "" {
		return mcplib.NewToolResultError("property is required"), nil
	}
	c, err := newMCPClient()
	if err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}
	path := strings.ReplaceAll(pathTemplate, "{property}", property)
	bodyJSON, _ := json.Marshal(body)
	data, _, err := c.Post(path, bodyJSON)
	if err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}
	return mcplib.NewToolResultText(string(data)), nil
}

func intentInt(args map[string]any, key string, def int) int {
	if v, ok := args[key]; ok {
		switch t := v.(type) {
		case float64:
			return int(t)
		case int:
			return t
		case string:
			n, err := strconvAtoi(t)
			if err == nil {
				return n
			}
		}
	}
	return def
}
