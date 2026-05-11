// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `templates` stores named runReport definitions on disk so users can recall a
// frequent query with one word. Distinct from the existing flag-profile
// command (`profile`): templates are full report bodies, profiles are flag
// presets.

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

type reportTemplate struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Property    string         `json:"property,omitempty"`
	Body        map[string]any `json:"body"`
	CreatedAt   string         `json:"created_at"`
}

func templatesDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".config", "ga4-pp-cli", "templates")
}

func templatePath(name string) string {
	return filepath.Join(templatesDir(), sanitizeName(name)+".json")
}

func sanitizeName(s string) string {
	out := strings.Builder{}
	for _, c := range strings.ToLower(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
			out.WriteRune(c)
		case c == ' ', c == '/':
			out.WriteRune('-')
		}
	}
	if out.Len() == 0 {
		return "unnamed"
	}
	return out.String()
}

func loadTemplate(name string) (*reportTemplate, error) {
	data, err := os.ReadFile(templatePath(name))
	if err != nil {
		return nil, err
	}
	var t reportTemplate
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func saveTemplate(t *reportTemplate) error {
	if err := os.MkdirAll(templatesDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(templatePath(t.Name), data, 0o644)
}

func newTemplatesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "templates",
		Aliases: []string{"template", "tpl"},
		Short:   "Saved runReport templates (named report definitions)",
		Long: `Save a runReport body shape under a name and recall it later. Templates
live as JSON files under ~/.config/ga4-pp-cli/templates/. The compat
subcommand runs checkCompatibility against every metric in the template.`,
	}
	cmd.AddCommand(
		newTemplatesSaveCmd(flags),
		newTemplatesListCmd(flags),
		newTemplatesRunCmd(flags),
		newTemplatesCompatCmd(flags),
		newTemplatesDeleteCmd(flags),
	)
	return cmd
}

func newTemplatesSaveCmd(flags *rootFlags) *cobra.Command {
	var dimensions, metrics, dateRangeSpec, dimensionFilter, orderBys, description, property string
	var stdinBody bool
	cmd := &cobra.Command{
		Use:   "save <name>",
		Short: "Save a runReport template by name",
		Example: `  ga4-pp-cli templates save weekly-content \
    --dimensions pagePath,country \
    --metrics screenPageViews,engagementRate \
    --date-range 7daysAgo,today
  echo '{"dimensions":[{"name":"date"}], ...}' | ga4-pp-cli templates save full --stdin`,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			t := &reportTemplate{
				Name:        args[0],
				Description: description,
				Property:    property,
				CreatedAt:   timeNowRFC3339(),
			}
			if stdinBody {
				data, err := readAllStdin()
				if err != nil {
					return err
				}
				if err := json.Unmarshal(data, &t.Body); err != nil {
					return fmt.Errorf("parsing stdin JSON: %w", err)
				}
			} else {
				body := map[string]any{}
				if dimensions != "" {
					body["dimensions"] = dimList(dimensions)
				}
				if metrics != "" {
					body["metrics"] = metricList(metrics)
				}
				if dateRangeSpec != "" {
					body["dateRanges"] = dateRange(dateRangeSpec)
				}
				if dimensionFilter != "" {
					var f any
					if err := json.Unmarshal([]byte(dimensionFilter), &f); err != nil {
						return fmt.Errorf("parsing --dimension-filter: %w", err)
					}
					body["dimensionFilter"] = f
				}
				if orderBys != "" {
					var o any
					if err := json.Unmarshal([]byte(orderBys), &o); err != nil {
						return fmt.Errorf("parsing --order-bys: %w", err)
					}
					body["orderBys"] = o
				}
				t.Body = body
			}
			if len(t.Body) == 0 {
				return fmt.Errorf("template body is empty: pass dimensions/metrics or --stdin")
			}
			if err := saveTemplate(t); err != nil {
				return fmt.Errorf("saving template: %w", err)
			}
			b, _ := json.MarshalIndent(map[string]any{
				"saved": t.Name,
				"path":  templatePath(t.Name),
			}, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	cmd.Flags().StringVar(&dimensions, "dimensions", "", "Comma-separated dimension apiNames")
	cmd.Flags().StringVar(&metrics, "metrics", "", "Comma-separated metric apiNames")
	cmd.Flags().StringVar(&dateRangeSpec, "date-range", "", "e.g. 7d / 28daysAgo,today / 2026-01-01,2026-01-31")
	cmd.Flags().StringVar(&dimensionFilter, "dimension-filter", "", "Raw JSON for dimensionFilter")
	cmd.Flags().StringVar(&orderBys, "order-bys", "", "Raw JSON for orderBys")
	cmd.Flags().StringVar(&description, "description", "", "Free-text description shown by 'templates list'")
	cmd.Flags().StringVar(&property, "property", "", "Pin this template to a specific GA4 property")
	cmd.Flags().BoolVar(&stdinBody, "stdin", false, "Read full runReport body as JSON from stdin")
	return cmd
}

func newTemplatesListCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "list",
		Short:       "List saved GA4 report templates with name, property, and description",
		Example:     "  ga4-pp-cli templates list --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			entries, err := os.ReadDir(templatesDir())
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			out := []map[string]any{}
			for _, e := range entries {
				if !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				name := strings.TrimSuffix(e.Name(), ".json")
				t, err := loadTemplate(name)
				if err != nil {
					continue
				}
				row := map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"property":    t.Property,
					"created_at":  t.CreatedAt,
				}
				if dims, ok := t.Body["dimensions"].([]any); ok {
					row["dimensions_count"] = len(dims)
				}
				if mets, ok := t.Body["metrics"].([]any); ok {
					row["metrics_count"] = len(mets)
				}
				out = append(out, row)
			}
			sort.Slice(out, func(i, j int) bool {
				return fmt.Sprintf("%v", out[i]["name"]) < fmt.Sprintf("%v", out[j]["name"])
			})
			b, _ := json.MarshalIndent(map[string]any{"count": len(out), "templates": out}, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	return cmd
}

func newTemplatesRunCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:         "run <name>",
		Aliases:     []string{"exec"},
		Short:       "Run a saved template",
		Example:     "  ga4-pp-cli templates run weekly-content --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			t, err := loadTemplate(args[0])
			if err != nil {
				return fmt.Errorf("loading template %q: %w", args[0], err)
			}
			prop := property
			if prop == "" {
				prop = t.Property
			}
			if prop == "" {
				p, err := resolveProperty(nil, flags)
				if err != nil {
					return err
				}
				prop = p
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			report, err := runReport(newClientAdapter(c), prop, t.Body)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			return printOutputWithFlags(cmd.OutOrStdout(), mustJSON(attachWarnings(report)), flags)
		},
	}
	cmd.Flags().StringVar(&property, "property", "", "Override the template's property")
	return cmd
}

func newTemplatesCompatCmd(flags *rootFlags) *cobra.Command {
	var property string
	cmd := &cobra.Command{
		Use:   "compat <name>",
		Short: "Run checkCompatibility for every metric in a template (dimension×metric matrix)",
		Long: `Calls /properties/<id>:checkCompatibility once per metric in the saved
template, paired with the template's full dimension list. Returns a matrix
showing which metrics are compatible with the chosen dimensions and which
would 400 the underlying runReport.`,
		Example:     "  ga4-pp-cli templates compat weekly-content --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			t, err := loadTemplate(args[0])
			if err != nil {
				return fmt.Errorf("loading template %q: %w", args[0], err)
			}
			prop := property
			if prop == "" {
				prop = t.Property
			}
			if prop == "" {
				p, err := resolveProperty(nil, flags)
				if err != nil {
					return err
				}
				prop = p
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			adapter := newClientAdapter(c)
			dims, _ := t.Body["dimensions"].([]any)
			if dims == nil {
				if d, ok := t.Body["dimensions"].([]map[string]any); ok {
					for _, dd := range d {
						dims = append(dims, dd)
					}
				}
			}
			metricsAny, _ := t.Body["metrics"].([]any)
			if metricsAny == nil {
				if m, ok := t.Body["metrics"].([]map[string]any); ok {
					for _, mm := range m {
						metricsAny = append(metricsAny, mm)
					}
				}
			}
			matrix := []map[string]any{}
			for _, m := range metricsAny {
				mm, _ := m.(map[string]any)
				body := map[string]any{
					"dimensions": dims,
					"metrics":    []any{mm},
				}
				path := "/v1beta/properties/" + prop + ":checkCompatibility"
				raw, _, err := adapter.post(path, body)
				row := map[string]any{
					"metric": mm["name"],
				}
				if err != nil {
					row["compatible"] = false
					row["error"] = err.Error()
					matrix = append(matrix, row)
					continue
				}
				var resp map[string]any
				_ = json.Unmarshal(raw, &resp)
				if compatMets, ok := resp["metricCompatibilities"].([]any); ok && len(compatMets) > 0 {
					first, _ := compatMets[0].(map[string]any)
					row["compatible"] = first["compatibility"] == "COMPATIBLE"
					row["compatibility"] = first["compatibility"]
				} else {
					row["compatible"] = true
				}
				matrix = append(matrix, row)
			}
			b, _ := json.MarshalIndent(map[string]any{
				"template":      t.Name,
				"property":      prop,
				"dimensions":    dims,
				"compatibility": matrix,
			}, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property", "", "Override the template's property")
	return cmd
}

func newTemplatesDeleteCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm"},
		Short:   "Delete a saved template",
		Example: "  ga4-pp-cli templates delete weekly-content --agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			path := templatePath(args[0])
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("removing %s: %w", path, err)
			}
			b, _ := json.MarshalIndent(map[string]any{"deleted": args[0]}, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), b, flags)
		},
	}
	return cmd
}

func readAllStdin() ([]byte, error) {
	return readAllFromStdin()
}
