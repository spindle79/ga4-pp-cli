// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `schema` syncs GA4 dimensions and metrics for a property into a JSON cache,
// then provides offline list/search over that cache. Replaces the
// surendranb/google-analytics-mcp `search_schema` tool with an offline-first
// implementation: zero API calls after `schema fetch` is run once.

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

type schemaEntry struct {
	APIName     string `json:"apiName"`
	UIName      string `json:"uiName"`
	Description string `json:"description"`
	Category    string `json:"category,omitempty"`
	Custom      bool   `json:"customDefinition,omitempty"`
	Kind        string `json:"kind"` // "dimension" or "metric"
}

type schemaCache struct {
	Property   string        `json:"property"`
	FetchedAt  string        `json:"fetched_at"`
	Dimensions []schemaEntry `json:"dimensions"`
	Metrics    []schemaEntry `json:"metrics"`
}

func schemaCachePath(property string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".config", "ga4-pp-cli", fmt.Sprintf("schema-%s.json", property))
}

func loadSchemaCache(property string) (*schemaCache, error) {
	data, err := os.ReadFile(schemaCachePath(property))
	if err != nil {
		return nil, err
	}
	var sc schemaCache
	if err := json.Unmarshal(data, &sc); err != nil {
		return nil, err
	}
	return &sc, nil
}

func saveSchemaCache(sc *schemaCache) error {
	p := schemaCachePath(sc.Property)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func newSchemaCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Browse GA4 dimensions and metrics (offline JSON cache; prefer 'sync schema' + 'search')",
		Long: `'schema fetch' calls getMetadata once and caches the result; subsequent
'schema list' and 'schema search' calls run offline against the cache. The
cache includes custom dimensions/metrics registered on the property
(customEvent:* / customUser:*).

Back-compat aliases. The Printing Press-standard equivalents live at the top
level and read the SQLite-backed local store:

  schema fetch  → ga4-pp-cli sync schema
  schema search → ga4-pp-cli search

New code should prefer the top-level commands; this JSON cache is retained
so existing automations keep working.`,
	}
	cmd.AddCommand(
		newSchemaFetchCmd(flags),
		newSchemaListCmd(flags),
		newSchemaSearchCmd(flags),
	)
	return cmd
}

func newSchemaFetchCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:     "fetch [property]",
		Aliases: []string{"sync"},
		Short:   "Fetch dimensions/metrics metadata from GA4 and cache to disk",
		Example: "  ga4-pp-cli schema fetch    # uses GA_PROPERTY_ID",
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
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			adapter := newClientAdapter(c)
			data, err := adapter.get("/v1beta/properties/"+prop+"/metadata", nil)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			var raw struct {
				Dimensions []map[string]any `json:"dimensions"`
				Metrics    []map[string]any `json:"metrics"`
			}
			if err := json.Unmarshal(data, &raw); err != nil {
				return fmt.Errorf("parsing metadata: %w", err)
			}
			sc := &schemaCache{
				Property:  prop,
				FetchedAt: nowRFC3339(),
			}
			for _, d := range raw.Dimensions {
				sc.Dimensions = append(sc.Dimensions, schemaEntry{
					APIName:     str(d["apiName"]),
					UIName:      str(d["uiName"]),
					Description: str(d["description"]),
					Category:    str(d["category"]),
					Custom:      boolish(d["customDefinition"]),
					Kind:        "dimension",
				})
			}
			for _, m := range raw.Metrics {
				sc.Metrics = append(sc.Metrics, schemaEntry{
					APIName:     str(m["apiName"]),
					UIName:      str(m["uiName"]),
					Description: str(m["description"]),
					Category:    str(m["category"]),
					Custom:      boolish(m["customDefinition"]),
					Kind:        "metric",
				})
			}
			if err := saveSchemaCache(sc); err != nil {
				return fmt.Errorf("saving cache: %w", err)
			}
			out := map[string]any{
				"property":         prop,
				"dimensions_count": len(sc.Dimensions),
				"metrics_count":    len(sc.Metrics),
				"cache_path":       schemaCachePath(prop),
				"fetched_at":       sc.FetchedAt,
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID (defaults to GA_PROPERTY_ID or positional arg)")
	return cmd
}

func newSchemaListCmd(flags *rootFlags) *cobra.Command {
	var kind string
	var customOnly bool
	var property string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List dimensions and/or metrics from the local schema cache",
		Long: `Reads from the cache written by 'schema fetch'. Use --kind to filter to
dimensions or metrics; --custom shows only custom definitions registered on
the property.`,
		Example: "  ga4-pp-cli schema list --kind metric --custom --agent",
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
			sc, err := loadSchemaCache(prop)
			if err != nil {
				return fmt.Errorf("no cached schema for property %s: run `ga4-pp-cli schema fetch %s` first (%w)", prop, prop, err)
			}
			out := []schemaEntry{}
			if kind == "" || kind == "dimension" || kind == "dimensions" {
				for _, e := range sc.Dimensions {
					if customOnly && !e.Custom {
						continue
					}
					out = append(out, e)
				}
			}
			if kind == "" || kind == "metric" || kind == "metrics" {
				for _, e := range sc.Metrics {
					if customOnly && !e.Custom {
						continue
					}
					out = append(out, e)
				}
			}
			b, _ := json.MarshalIndent(map[string]any{
				"property": prop,
				"count":    len(out),
				"items":    out,
			}, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "Filter: dimension|metric (empty = both)")
	cmd.Flags().BoolVar(&customOnly, "custom", false, "Only show custom (property-registered) entries")
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID (defaults to GA_PROPERTY_ID)")
	return cmd
}

func newSchemaSearchCmd(flags *rootFlags) *cobra.Command {
	var property string
	var kind string
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Token-rank search across cached dimension/metric apiNames, uiNames, and descriptions",
		Example: "  ga4-pp-cli schema search engagement --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			prop, err := resolveProperty(nil, flags)
			if err != nil && property == "" {
				return err
			}
			if property != "" {
				prop = property
			}
			sc, err := loadSchemaCache(prop)
			if err != nil {
				return fmt.Errorf("no cached schema for property %s: run `ga4-pp-cli schema fetch %s` first (%w)", prop, prop, err)
			}
			query := strings.ToLower(strings.Join(args, " "))
			tokens := strings.Fields(query)
			pool := []schemaEntry{}
			if kind == "" || kind == "dimension" || kind == "dimensions" {
				pool = append(pool, sc.Dimensions...)
			}
			if kind == "" || kind == "metric" || kind == "metrics" {
				pool = append(pool, sc.Metrics...)
			}
			type scored struct {
				schemaEntry
				Score int `json:"_score"`
			}
			results := []scored{}
			for _, e := range pool {
				score := scoreTokens(tokens, e)
				if score > 0 {
					results = append(results, scored{e, score})
				}
			}
			sort.Slice(results, func(i, j int) bool {
				if results[i].Score != results[j].Score {
					return results[i].Score > results[j].Score
				}
				return results[i].APIName < results[j].APIName
			})
			b, _ := json.MarshalIndent(map[string]any{
				"property": prop,
				"query":    query,
				"count":    len(results),
				"results":  results,
			}, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID (defaults to GA_PROPERTY_ID)")
	cmd.Flags().StringVar(&kind, "kind", "", "Filter: dimension|metric (empty = both)")
	return cmd
}

// scoreTokens ranks an entry against the query tokens. Heavier weight on
// apiName and uiName matches, lighter on description; multi-token AND scoring
// (every token must match somewhere or score is 0) approximates FTS5 BM25
// well enough for a CLI catalog with O(few hundred) entries.
func scoreTokens(tokens []string, e schemaEntry) int {
	if len(tokens) == 0 {
		return 0
	}
	apiName := strings.ToLower(e.APIName)
	uiName := strings.ToLower(e.UIName)
	desc := strings.ToLower(e.Description)
	cat := strings.ToLower(e.Category)
	score := 0
	matchedAll := true
	for _, t := range tokens {
		hit := false
		if strings.Contains(apiName, t) {
			score += 5
			hit = true
		}
		if strings.Contains(uiName, t) {
			score += 4
			hit = true
		}
		if strings.Contains(cat, t) {
			score += 2
			hit = true
		}
		if strings.Contains(desc, t) {
			score += 1
			hit = true
		}
		if !hit {
			matchedAll = false
		}
	}
	if !matchedAll {
		return 0
	}
	if e.Custom {
		score += 2 // small boost: custom definitions are more specific to the user
	}
	return score
}

// helpers
func str(v any) string {
	s, _ := v.(string)
	return s
}

func boolish(v any) bool {
	b, _ := v.(bool)
	return b
}

func nowRFC3339() string {
	return timeNowRFC3339()
}
