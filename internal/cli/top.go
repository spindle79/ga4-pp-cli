// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `top` is the agent-native umbrella for the "what's the top N X by metric"
// questions an LLM most often gets asked of GA4 (top pages, top sources,
// top events, top countries, top devices, top campaigns). Each subcommand
// is a thin wrapper around one SQL GROUP BY against the matching daily
// mirror table, with built-in --period presets, --metric validation, and
// auto-refresh of the local store when stale.
//
// Why this exists: with raw `sql` or `properties run-report` an agent has to
// know table/dimension/metric names, write SQL, pick a date window, etc.
// With `top` it just calls the verb and gets the right shape back.

package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"ga4-pp-cli/internal/cliutil"
	"ga4-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

// topMaxAge governs auto-refresh for top-* commands. Picked to match
// defaultStaleAge in auto_refresh.go (6h) — past that we trigger a sync.
const topMaxAge = 6 * time.Hour

// topSpec describes one `top X` subcommand: what table to read, which
// dimension columns to group by, which metric columns are queryable, and
// the default metric.
type topSpec struct {
	use            string
	aliases        []string
	short          string
	example        string
	table          string
	scope          string // sync_state.scope name + sync subcommand suffix
	syncCmd        string // sync subcommand name (e.g. "acquisition")
	dimensions     []string
	defaultMetric  string
	allowedMetrics []string
}

// allTopSpecs is the canonical list — root.go consumes it to register the
// umbrella, and the doctor / agent-context introspectors could iterate it
// for self-description in the future.
func allTopSpecs() []topSpec {
	return []topSpec{
		{
			use: "pages", short: "Top performing pages",
			example: "  ga4-pp-cli top pages --period 7d --agent",
			table:   "pages_daily", scope: "pages", syncCmd: "pages",
			dimensions:    []string{"page_path"},
			defaultMetric: "sessions",
			allowedMetrics: []string{
				"sessions", "screen_page_views", "engaged_sessions",
				"total_users", "average_session_duration", "conversions",
			},
		},
		{
			use: "sources", aliases: []string{"traffic-sources"},
			short:   "Top traffic sources by session_source/session_medium",
			example: "  ga4-pp-cli top sources --period 7d --agent",
			table:   "acquisition_daily", scope: "acquisition", syncCmd: "acquisition",
			dimensions:    []string{"session_source", "session_medium"},
			defaultMetric: "sessions",
			allowedMetrics: []string{
				"sessions", "total_users", "new_users",
				"engaged_sessions", "conversions", "total_revenue",
			},
		},
		{
			use: "events", short: "Top events by event_count",
			example: "  ga4-pp-cli top events --period 7d --agent",
			table:   "events_daily", scope: "events", syncCmd: "events",
			dimensions:    []string{"event_name"},
			defaultMetric: "event_count",
			allowedMetrics: []string{
				"event_count", "event_value", "total_users", "conversions",
			},
		},
		{
			use: "countries", aliases: []string{"geo"},
			short:   "Top countries by sessions",
			example: "  ga4-pp-cli top countries --period 7d --agent",
			table:   "devices_geo_daily", scope: "devices_geo", syncCmd: "devices-geo",
			dimensions:    []string{"country"},
			defaultMetric: "sessions",
			allowedMetrics: []string{
				"sessions", "total_users", "engaged_sessions", "screen_page_views",
			},
		},
		{
			use:     "devices",
			short:   "Top device categories (mobile vs desktop vs tablet)",
			example: "  ga4-pp-cli top devices --period 7d --agent",
			table:   "devices_geo_daily", scope: "devices_geo", syncCmd: "devices-geo",
			dimensions:    []string{"device_category"},
			defaultMetric: "sessions",
			allowedMetrics: []string{
				"sessions", "total_users", "engaged_sessions", "screen_page_views",
			},
		},
		{
			use: "campaigns", short: "Top campaigns by sessions",
			example: "  ga4-pp-cli top campaigns --period 7d --agent",
			table:   "acquisition_daily", scope: "acquisition", syncCmd: "acquisition",
			dimensions:    []string{"session_campaign"},
			defaultMetric: "sessions",
			allowedMetrics: []string{
				"sessions", "total_users", "new_users",
				"engaged_sessions", "conversions", "total_revenue",
			},
		},
	}
}

func newTopCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "top",
		Short: "Top-N answers from the local store: pages, sources, events, countries, devices, campaigns",
		Long: `Agent-native verbs for "what are my top performing X this week" questions.

Each subcommand reads from a synced daily-mirror table and runs a single
GROUP BY + ORDER BY + LIMIT — answers in <100ms once the local store is
warm. --period accepts the common presets (today, yesterday, 7d, 28d, 30d,
90d, wtd). Auto-refresh kicks a background sync when the local store is
stale (>6h) so cold-cache calls don't need a manual sync first.

  ga4-pp-cli top pages --period 7d --agent
  ga4-pp-cli top sources --metric total_users --agent
  ga4-pp-cli top events --period 28d --limit 5 --agent`,
	}
	// Static AddCommand calls (one per spec) so the printing-press
	// verify-skill AST walker can resolve `top <leaf>` to this file.
	// A `for spec := range allTopSpecs()` loop builds the same tree at
	// runtime but the verifier walks the rootCmd.AddCommand graph
	// statically; it needs a named constructor per subcommand to bind
	// the path. Each constructor below is a one-line wrapper around the
	// shared newTopSubCmd factory.
	cmd.AddCommand(newTopPagesCmd(flags))
	cmd.AddCommand(newTopSourcesCmd(flags))
	cmd.AddCommand(newTopEventsCmd(flags))
	cmd.AddCommand(newTopCountriesCmd(flags))
	cmd.AddCommand(newTopDevicesCmd(flags))
	cmd.AddCommand(newTopCampaignsCmd(flags))
	return cmd
}

// One-line constructors per subcommand. Each pulls its spec from the
// canonical table so the metric whitelist / table / sync scope stays in
// one place; the verify-skill walker still gets a stable function name to
// resolve.

func topSpecByUse(use string) topSpec {
	for _, s := range allTopSpecs() {
		if s.use == use {
			return s
		}
	}
	// Programmer error — every Use here has a corresponding spec.
	panic(fmt.Sprintf("topSpecByUse: no spec for %q", use))
}

// Each per-leaf constructor below declares its own cobra.Command literal
// with an explicit `Use: "<leaf>"` so the printing-press verify-skill
// AST walker can resolve `top <leaf>` to a single file. RunE delegates
// to topRunE so the actual logic stays in one place.

func newTopPagesCmd(flags *rootFlags) *cobra.Command {
	tf := &topFlags{}
	spec := topSpecByUse("pages")
	cmd := &cobra.Command{
		Use:         "pages",
		Short:       "Top performing pages by sessions (default) from pages_daily",
		Example:     "  ga4-pp-cli top pages --period 7d --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return topRunE(cmd, args, flags, tf, spec)
		},
	}
	bindTopFlags(cmd, tf, spec)
	return cmd
}

func newTopSourcesCmd(flags *rootFlags) *cobra.Command {
	tf := &topFlags{}
	spec := topSpecByUse("sources")
	cmd := &cobra.Command{
		Use:         "sources",
		Aliases:     []string{"traffic-sources"},
		Short:       "Top traffic sources by sessions from acquisition_daily",
		Example:     "  ga4-pp-cli top sources --period 7d --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return topRunE(cmd, args, flags, tf, spec)
		},
	}
	bindTopFlags(cmd, tf, spec)
	return cmd
}

func newTopEventsCmd(flags *rootFlags) *cobra.Command {
	tf := &topFlags{}
	spec := topSpecByUse("events")
	cmd := &cobra.Command{
		Use:         "events",
		Short:       "Top events by event_count from events_daily",
		Example:     "  ga4-pp-cli top events --period 7d --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return topRunE(cmd, args, flags, tf, spec)
		},
	}
	bindTopFlags(cmd, tf, spec)
	return cmd
}

func newTopCountriesCmd(flags *rootFlags) *cobra.Command {
	tf := &topFlags{}
	spec := topSpecByUse("countries")
	cmd := &cobra.Command{
		Use:         "countries",
		Aliases:     []string{"geo"},
		Short:       "Top countries by sessions from devices_geo_daily",
		Example:     "  ga4-pp-cli top countries --period 7d --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return topRunE(cmd, args, flags, tf, spec)
		},
	}
	bindTopFlags(cmd, tf, spec)
	return cmd
}

func newTopDevicesCmd(flags *rootFlags) *cobra.Command {
	tf := &topFlags{}
	spec := topSpecByUse("devices")
	cmd := &cobra.Command{
		Use:         "devices",
		Short:       "Top device categories by sessions from devices_geo_daily",
		Example:     "  ga4-pp-cli top devices --period 7d --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return topRunE(cmd, args, flags, tf, spec)
		},
	}
	bindTopFlags(cmd, tf, spec)
	return cmd
}

func newTopCampaignsCmd(flags *rootFlags) *cobra.Command {
	tf := &topFlags{}
	spec := topSpecByUse("campaigns")
	cmd := &cobra.Command{
		Use:         "campaigns",
		Short:       "Top campaigns by sessions from acquisition_daily",
		Example:     "  ga4-pp-cli top campaigns --period 7d --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return topRunE(cmd, args, flags, tf, spec)
		},
	}
	bindTopFlags(cmd, tf, spec)
	return cmd
}

// topFlags is the shared flag bag bound by every per-leaf constructor and
// read by topRunE. Keeping the values in a single struct lets each
// constructor own its own cobra.Command literal (so verify-skill can see
// the Use: line) while still sharing the actual run logic.
type topFlags struct {
	property string
	metric   string
	period   string
	limit    int
}

// bindTopFlags wires --property/--metric/--period/--limit onto cmd.
func bindTopFlags(cmd *cobra.Command, tf *topFlags, spec topSpec) {
	cmd.Flags().StringVar(&tf.property, "property", "", "GA4 property ID (defaults to GA_PROPERTY_ID)")
	cmd.Flags().StringVar(&tf.metric, "metric", "",
		fmt.Sprintf("Metric to rank by (default %s; allowed: %s)",
			spec.defaultMetric, strings.Join(spec.allowedMetrics, ", ")))
	cmd.Flags().StringVar(&tf.period, "period", "7d", "Date window: today|yesterday|7d|28d|30d|90d|wtd")
	cmd.Flags().IntVar(&tf.limit, "limit", 10, "Max rows to return")
}

// topRunE is the shared RunE shape for every top X subcommand. It uses
// the spec to know which table to query, which dimensions to group on,
// and which metric whitelist to enforce.
func topRunE(cmd *cobra.Command, args []string, flags *rootFlags, tf *topFlags, spec topSpec) error {
	if dryRunOK(flags) {
		return nil
	}
	if tf.metric == "" {
		tf.metric = spec.defaultMetric
	}
	if !containsString(spec.allowedMetrics, tf.metric) {
		return usageErr(fmt.Errorf(
			"unknown --metric %q for `top %s`; valid: %s",
			tf.metric, spec.use, strings.Join(spec.allowedMetrics, ", "),
		))
	}
	startDate, endDate, perr := resolveTopPeriod(tf.period)
	if perr != nil {
		return usageErr(perr)
	}
	if tf.limit <= 0 {
		tf.limit = 10
	}
	argv := args
	if len(argv) == 0 && tf.property != "" {
		argv = []string{tf.property}
	}
	prop, err := resolveProperty(argv, flags)
	if err != nil {
		return err
	}
	source, err := topResolveDataSource(cmd, flags, spec, prop, startDate, endDate, tf.metric, tf.limit)
	if err != nil {
		return err
	}
	out := map[string]any{
		"command":     "top " + spec.use,
		"property":    prop,
		"metric":      tf.metric,
		"period":      tf.period,
		"start_date":  startDate,
		"end_date":    endDate,
		"limit":       tf.limit,
		"dimensions":  spec.dimensions,
		"data_source": source.dataSource,
		"rows":        source.rows,
		"count":       len(source.rows),
	}
	if source.note != "" {
		out["note"] = source.note
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
}

// newTopSubCmd is retained as a one-stop builder for callers (the original
// dynamic-spec path). The per-leaf constructors below replicate the same
// cobra.Command shape with a literal Use: so the verify-skill AST walker
// can resolve each `top <leaf>` path to a single file.
func newTopSubCmd(flags *rootFlags, spec topSpec) *cobra.Command {
	tf := &topFlags{}
	cmd := &cobra.Command{
		Use:         spec.use,
		Aliases:     spec.aliases,
		Short:       spec.short,
		Example:     spec.example,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return topRunE(cmd, args, flags, tf, spec)
		},
	}
	bindTopFlags(cmd, tf, spec)
	return cmd
}

// topResult is the small struct passed back from the local/live resolver
// to RunE so the JSON envelope assembly stays in one place.
type topResult struct {
	rows       []map[string]any
	dataSource string
	note       string
}

// topResolveDataSource implements the auto/local/live policy for top-* and
// returns ranked rows already grouped by the spec dimensions.
func topResolveDataSource(cmd *cobra.Command, flags *rootFlags, spec topSpec, prop, startDate, endDate, metric string, limit int) (topResult, error) {
	switch flags.dataSource {
	case "local", "auto":
		// Try local first. If the store is missing or empty for this scope
		// AND we're in 'auto', fall through to live; if 'local', surface
		// the error so the user knows to run sync.
		res, err := topFromLocal(cmd, flags, spec, prop, startDate, endDate, metric, limit)
		if err == nil && len(res.rows) > 0 {
			return res, nil
		}
		if err != nil && flags.dataSource == "local" {
			return topResult{}, err
		}
		if len(res.rows) == 0 && flags.dataSource == "local" {
			return topResult{
				rows:       []map[string]any{},
				dataSource: "local",
				note:       fmt.Sprintf("local store has no %s rows for %s..%s — run `ga4-pp-cli sync %s` first", spec.table, startDate, endDate, spec.syncCmd),
			}, nil
		}
		// auto + empty/error: live fallback (kicks a sync in the background too).
		fallthrough
	case "live":
		return topFromLive(cmd, flags, spec, prop, startDate, endDate, metric, limit)
	}
	return topResult{}, fmt.Errorf("invalid --data-source %q", flags.dataSource)
}

// topFromLocal executes the GROUP BY against the local SQLite store. It
// also nudges a background sync when the per-scope sync_state is stale.
func topFromLocal(cmd *cobra.Command, flags *rootFlags, spec topSpec, prop, startDate, endDate, metric string, limit int) (topResult, error) {
	path := store.DefaultPath()
	if _, err := os.Stat(path); err != nil {
		return topResult{}, fmt.Errorf("local store missing at %s (run `ga4-pp-cli sync %s` first)", path, spec.syncCmd)
	}
	s, err := store.OpenReadOnly(path)
	if err != nil {
		return topResult{}, fmt.Errorf("open store (read-only): %w", err)
	}
	defer s.Close()

	// Freshness check is informational here — the policy in
	// autoRefreshIfStale already covers it for many commands, but the
	// top-* scope mapping isn't in scopeForCommandPath, so kick it
	// directly when 'auto' and stale.
	if flags.dataSource == "auto" && os.Getenv("GA4_DISABLE_AUTO_REFRESH") != "1" {
		ctx, cancel := context.WithTimeout(cmd.Context(), 1*time.Second)
		defer cancel()
		stale, _, _ := cliutil.EnsureFresh(ctx, s.DB(), prop, spec.scope, topMaxAge)
		if stale {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"WARN local %q is stale (>%s) — kicking background `sync %s`\n",
				spec.scope, topMaxAge, spec.syncCmd)
			kickBackgroundSync(spec.syncCmd)
		}
	}

	// Build the GROUP BY. Dimensions and metric come from the spec's
	// whitelist, so we can interpolate without SQL-injection risk; values
	// (property, dates, limit) are bound via parameters.
	dims := strings.Join(spec.dimensions, ", ")
	query := fmt.Sprintf(`
		SELECT %s, SUM(%s) AS metric_value
		FROM %s
		WHERE property_id = ? AND date >= ? AND date <= ?
		GROUP BY %s
		ORDER BY metric_value DESC
		LIMIT ?
	`, dims, metric, spec.table, dims)

	rows, err := s.DB().Query(query, prop, startDate, endDate, limit)
	if err != nil {
		return topResult{}, fmt.Errorf("query %s: %w", spec.table, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return topResult{}, err
	}
	result := []map[string]any{}
	rank := 1
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return topResult{}, err
		}
		row := map[string]any{"rank": rank}
		for i, c := range cols {
			row[c] = normalizeSQLValue(vals[i])
		}
		// Alias the SUM column from "metric_value" → the actual metric name
		// so the agent's response uses the same vocabulary the caller chose.
		if v, ok := row["metric_value"]; ok {
			row[metric] = v
			delete(row, "metric_value")
		}
		result = append(result, row)
		rank++
	}
	if err := rows.Err(); err != nil {
		return topResult{}, err
	}
	return topResult{rows: result, dataSource: "local"}, nil
}

// topFromLive runs the equivalent runReport call against the GA4 Data API.
// Used when --data-source=live or 'auto' with no local rows yet.
func topFromLive(cmd *cobra.Command, flags *rootFlags, spec topSpec, prop, startDate, endDate, metric string, limit int) (topResult, error) {
	c, err := flags.newClient()
	if err != nil {
		return topResult{}, err
	}
	// Map snake_case columns back to GA4 dimension/metric API names.
	apiMetric := toAPICamelCase(metric)
	dims := make([]map[string]any, 0, len(spec.dimensions))
	for _, d := range spec.dimensions {
		dims = append(dims, map[string]any{"name": toAPICamelCase(d)})
	}
	body := map[string]any{
		"dimensions": dims,
		"metrics":    []map[string]any{{"name": apiMetric}},
		"dateRanges": []map[string]any{{"startDate": startDate, "endDate": endDate}},
		"orderBys": []map[string]any{
			{"metric": map[string]any{"metricName": apiMetric}, "desc": true},
		},
		"limit":               fmt.Sprintf("%d", limit),
		"returnPropertyQuota": true,
	}
	report, rerr := runReport(newClientAdapter(c), prop, body)
	if rerr != nil {
		return topResult{}, classifyAPIError(rerr, flags)
	}
	out := []map[string]any{}
	rowsAny, _ := report["rows"].([]any)
	for i, ra := range rowsAny {
		row, _ := ra.(map[string]any)
		if row == nil {
			continue
		}
		dimsList, _ := row["dimensionValues"].([]any)
		mets, _ := row["metricValues"].([]any)
		r := map[string]any{"rank": i + 1}
		for j, d := range spec.dimensions {
			r[d] = dimensionAt(dimsList, j)
		}
		r[metric] = metricAt(mets, 0)
		out = append(out, r)
	}
	return topResult{rows: out, dataSource: "live"}, nil
}

// resolveTopPeriod maps a `--period` preset to absolute YYYY-MM-DD start/end
// dates. Dates are computed in UTC so the same period string is stable
// across timezones; GA4 itself reports in the property's configured TZ,
// but for "top N this week" granularity the UTC anchor is fine.
func resolveTopPeriod(period string) (string, string, error) {
	now := time.Now().UTC()
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "", "7d", "last-7-days", "this-week":
		return day(now.AddDate(0, 0, -7)), day(now), nil
	case "today":
		return day(now), day(now), nil
	case "yesterday":
		y := now.AddDate(0, 0, -1)
		return day(y), day(y), nil
	case "28d", "last-28-days":
		return day(now.AddDate(0, 0, -28)), day(now), nil
	case "30d", "last-30-days":
		return day(now.AddDate(0, 0, -30)), day(now), nil
	case "90d":
		return day(now.AddDate(0, 0, -90)), day(now), nil
	case "wtd", "week-to-date":
		// Monday-anchored week-to-date. Go's Weekday Sunday=0, Monday=1, …
		// adjust so Monday is "0 back".
		offset := int(now.Weekday()) - 1
		if offset < 0 {
			offset = 6
		}
		return day(now.AddDate(0, 0, -offset)), day(now), nil
	}
	return "", "", fmt.Errorf("invalid --period %q (use today, yesterday, 7d, 28d, 30d, 90d, wtd)", period)
}

// toAPICamelCase converts a snake_case store column to the GA4 Data API's
// camelCase dimension/metric name. Only used by the live fallback so the
// agent never sees an apiName.
func toAPICamelCase(snake string) string {
	parts := strings.Split(snake, "_")
	if len(parts) == 1 {
		return snake
	}
	var b strings.Builder
	for i, p := range parts {
		if i == 0 {
			b.WriteString(p)
			continue
		}
		if p == "" {
			continue
		}
		b.WriteByte(byte(strings.ToUpper(p[:1])[0]))
		b.WriteString(p[1:])
	}
	out := b.String()
	// GA4 has a couple of special-case names where the camelCase form
	// doesn't match the snake_case mapping directly.
	switch out {
	case "sessionCampaign":
		return "sessionCampaignName"
	}
	return out
}

// containsString is a tiny helper to keep the metric whitelist checks
// readable without dragging in slices.Contains (which would require
// bumping the go.mod toolchain target on older Go).
func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// normalizeSQLValue is declared in sql.go; this file uses it from
// topFromLocal. Duplicated reference comment kept here so future readers
// don't go hunting.
var _ = sql.ErrNoRows

// kickBackgroundSyncTopShim is identical in spirit to kickBackgroundSync
// in auto_refresh.go but accepts the sync subcommand name directly so the
// top-* code path doesn't have to map scope → command.
// (Unused; kickBackgroundSync already takes the subcommand name. Kept as
// documentation of the intent.)
var _ = exec.CommandContext
