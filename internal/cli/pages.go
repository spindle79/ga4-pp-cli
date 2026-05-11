// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `pages` mirrors the spindle79/ga4-mcp-server URL-centric tool shape
// (getUrlPageViews / getUrlEngagement / getUrlSourceTraffic / getUrlConversions
// / getUrlAnalytics) on top of the GA4 Data API runReport endpoint. Each
// command takes a pagePath plus an optional --timeframe and an optional
// --match-type so users aren't locked to EXACT match like the reference MCP.

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newPagesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pages",
		Aliases: []string{"page"},
		Short:   "URL-centric reports (views, engagement, traffic sources, conversions)",
		Long: `Pre-shaped runReport calls keyed on pagePath. Match types: EXACT (default),
CONTAINS, BEGINS_WITH, ENDS_WITH, FULL_REGEXP. Supports the same timeframe
presets as the spindle79 ga4-mcp-server plus arbitrary --start/--end.`,
	}
	cmd.AddCommand(
		newPagesViewsCmd(flags),
		newPagesEngagementCmd(flags),
		newPagesSourcesCmd(flags),
		newPagesConversionsCmd(flags),
		newPagesAnalyticsCmd(flags),
	)
	return cmd
}

type pagesShared struct {
	timeframe string
	startDate string
	endDate   string
	matchType string
	property  string
	limit     int
}

func (p *pagesShared) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&p.timeframe, "timeframe", "", "today|yesterday|7d|28d|30d|90d (overrides --start/--end)")
	cmd.Flags().StringVar(&p.startDate, "start", "", "YYYY-MM-DD start date (default 30daysAgo)")
	cmd.Flags().StringVar(&p.endDate, "end", "", "YYYY-MM-DD end date (default today)")
	cmd.Flags().StringVar(&p.matchType, "match-type", "EXACT", "pagePath match: EXACT|CONTAINS|BEGINS_WITH|ENDS_WITH|FULL_REGEXP")
	cmd.Flags().StringVar(&p.property, "property", "", "GA4 property ID (defaults to GA_PROPERTY_ID)")
	cmd.Flags().IntVar(&p.limit, "limit", 0, "Max rows to return (0 = API default)")
}

func (p *pagesShared) dateRanges() []map[string]any {
	if p.timeframe != "" {
		return dateRange(p.timeframe)
	}
	if p.startDate != "" || p.endDate != "" {
		s := p.startDate
		e := p.endDate
		if s == "" {
			s = "30daysAgo"
		}
		if e == "" {
			e = "today"
		}
		return []map[string]any{{"startDate": s, "endDate": e}}
	}
	return []map[string]any{{"startDate": "30daysAgo", "endDate": "today"}}
}

func (p *pagesShared) resolveProp(flags *rootFlags) (string, error) {
	if p.property != "" {
		return p.property, nil
	}
	prop, err := resolveProperty(nil, flags)
	if err != nil {
		return "", err
	}
	return prop, nil
}

func newPagesViewsCmd(flags *rootFlags) *cobra.Command {
	p := &pagesShared{}
	cmd := &cobra.Command{
		Use:         "views <pagePath>",
		Aliases:     []string{"pageviews"},
		Short:       "Page views for a path (matches spindle79 getUrlPageViews + arbitrary match types)",
		Example:     "  ga4-pp-cli pages views /blog/launch --timeframe 7d --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			prop, err := p.resolveProp(flags)
			if err != nil {
				return err
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			body := map[string]any{
				"dimensions":          dimList("pagePath,date"),
				"metrics":             metricList("screenPageViews"),
				"dateRanges":          p.dateRanges(),
				"dimensionFilter":     pagePathFilter(args[0], p.matchType),
				"returnPropertyQuota": true,
			}
			if p.limit > 0 {
				body["limit"] = fmt.Sprintf("%d", p.limit)
			}
			report, err := runReport(newClientAdapter(c), prop, body)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), mustJSON(attachWarnings(report)), flags)
		},
	}
	p.bind(cmd)
	return cmd
}

func newPagesEngagementCmd(flags *rootFlags) *cobra.Command {
	p := &pagesShared{}
	cmd := &cobra.Command{
		Use:         "engagement <pagePath>",
		Short:       "Engagement metrics for a path (bounceRate, engagedSessions, avg session duration, screenPageViewsPerSession)",
		Example:     "  ga4-pp-cli pages engagement /pricing --timeframe last-week --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			prop, err := p.resolveProp(flags)
			if err != nil {
				return err
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			body := map[string]any{
				"dimensions":          dimList("pagePath"),
				"metrics":             metricList("averageSessionDuration,bounceRate,engagedSessions,screenPageViewsPerSession"),
				"dateRanges":          p.dateRanges(),
				"dimensionFilter":     pagePathFilter(args[0], p.matchType),
				"returnPropertyQuota": true,
			}
			report, err := runReport(newClientAdapter(c), prop, body)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), mustJSON(attachWarnings(report)), flags)
		},
	}
	p.bind(cmd)
	return cmd
}

func newPagesSourcesCmd(flags *rootFlags) *cobra.Command {
	p := &pagesShared{}
	cmd := &cobra.Command{
		Use:         "sources <pagePath>",
		Aliases:     []string{"traffic-sources"},
		Short:       "Traffic sources (source/medium) for a path",
		Example:     "  ga4-pp-cli pages sources /home --timeframe 30d --limit 20 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			prop, err := p.resolveProp(flags)
			if err != nil {
				return err
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			body := map[string]any{
				"dimensions":      dimList("sessionSource,sessionMedium"),
				"metrics":         metricList("screenPageViews,totalUsers"),
				"dateRanges":      p.dateRanges(),
				"dimensionFilter": pagePathFilter(args[0], p.matchType),
				"orderBys": []map[string]any{
					{"metric": map[string]any{"metricName": "screenPageViews"}, "desc": true},
				},
				"returnPropertyQuota": true,
			}
			if p.limit > 0 {
				body["limit"] = fmt.Sprintf("%d", p.limit)
			} else {
				body["limit"] = "10"
			}
			report, err := runReport(newClientAdapter(c), prop, body)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), mustJSON(attachWarnings(report)), flags)
		},
	}
	p.bind(cmd)
	return cmd
}

func newPagesConversionsCmd(flags *rootFlags) *cobra.Command {
	p := &pagesShared{}
	cmd := &cobra.Command{
		Use:         "conversions <pagePath>",
		Short:       "Conversion events triggered on a path",
		Example:     "  ga4-pp-cli pages conversions /signup --timeframe 28d --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			prop, err := p.resolveProp(flags)
			if err != nil {
				return err
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			body := map[string]any{
				"dimensions":      dimList("eventName"),
				"metrics":         metricList("eventCount,conversions,totalRevenue"),
				"dateRanges":      p.dateRanges(),
				"dimensionFilter": pagePathFilter(args[0], p.matchType),
				"orderBys": []map[string]any{
					{"metric": map[string]any{"metricName": "conversions"}, "desc": true},
				},
				"returnPropertyQuota": true,
			}
			if p.limit > 0 {
				body["limit"] = fmt.Sprintf("%d", p.limit)
			}
			report, err := runReport(newClientAdapter(c), prop, body)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), mustJSON(attachWarnings(report)), flags)
		},
	}
	p.bind(cmd)
	return cmd
}

func newPagesAnalyticsCmd(flags *rootFlags) *cobra.Command {
	p := &pagesShared{}
	cmd := &cobra.Command{
		Use:         "analytics <pagePath>",
		Aliases:     []string{"all"},
		Short:       "Combined views + engagement + sources + conversions in one batchRunReports call",
		Example:     "  ga4-pp-cli pages analytics /landing --timeframe last-week --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			prop, err := p.resolveProp(flags)
			if err != nil {
				return err
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			filter := pagePathFilter(args[0], p.matchType)
			ranges := p.dateRanges()
			body := map[string]any{
				"requests": []map[string]any{
					{
						"dimensions":      dimList("pagePath"),
						"metrics":         metricList("screenPageViews,totalUsers"),
						"dateRanges":      ranges,
						"dimensionFilter": filter,
					},
					{
						"dimensions":      dimList("pagePath"),
						"metrics":         metricList("averageSessionDuration,bounceRate,engagedSessions,screenPageViewsPerSession"),
						"dateRanges":      ranges,
						"dimensionFilter": filter,
					},
					{
						"dimensions":      dimList("sessionSource,sessionMedium"),
						"metrics":         metricList("screenPageViews"),
						"dateRanges":      ranges,
						"dimensionFilter": filter,
						"limit":           "10",
					},
					{
						"dimensions":      dimList("eventName"),
						"metrics":         metricList("eventCount,conversions"),
						"dateRanges":      ranges,
						"dimensionFilter": filter,
					},
				},
			}
			report, err := batchRunReports(newClientAdapter(c), prop, body)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			// batchRunReports returns {reports:[...]} — collect warnings across each report
			out := map[string]any{
				"path":    args[0],
				"reports": map[string]any{},
			}
			if reports, ok := report["reports"].([]any); ok && len(reports) >= 4 {
				labels := []string{"views", "engagement", "sources", "conversions"}
				outR := map[string]any{}
				var allWarnings []string
				for i, r := range reports {
					rm, _ := r.(map[string]any)
					outR[labels[i]] = rm
					allWarnings = append(allWarnings, extractGA4Warnings(rm)...)
				}
				out["reports"] = outR
				if len(allWarnings) > 0 {
					out["_warnings"] = allWarnings
				}
			} else {
				out["raw"] = report
			}
			return printOutputWithFlags(cmd.OutOrStdout(), mustJSON(out), flags)
		},
	}
	p.bind(cmd)
	return cmd
}

// mustJSON marshals a value to JSON bytes; the helpers all read JSON, this
// keeps the caller code one line shorter and consistent with printAutoTable's
// signature.
func mustJSON(v any) []byte {
	b, err := jsonMarshalIndent(v, "", "  ")
	if err != nil {
		return []byte(fmt.Sprintf("{\"_error\":\"json marshal: %s\"}", err))
	}
	return b
}
