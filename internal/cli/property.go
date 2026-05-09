// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"strings"

	"ga4-pp-cli/internal/config"
)

// resolveProperty returns the GA4 property ID for a command, falling back to
// GA_PROPERTY_ID (loaded into config) when the positional arg is omitted.
//
// Both `12345` and `properties/12345` are accepted; the returned value is the
// bare numeric ID since the URL templates already wrap it in `properties/...`.
func resolveProperty(args []string, flags *rootFlags) (string, error) {
	var raw string
	if len(args) > 0 {
		raw = args[0]
	} else {
		cfg, err := config.Load(flags.configPath)
		if err != nil {
			return "", err
		}
		raw = cfg.PropertyID
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("no property id: pass <property> or set GA_PROPERTY_ID")
	}
	// Normalize "properties/12345" → "12345" so callers don't have to care.
	if strings.HasPrefix(raw, "properties/") {
		raw = strings.TrimPrefix(raw, "properties/")
	}
	return raw, nil
}
