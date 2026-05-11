// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"os"
	"strings"

	"ga4-pp-cli/internal/config"
	"ga4-pp-cli/internal/store"
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
		// Last-resort fallback: pick the first property from the local store
		// if one is already synced. Lets `drift pages`, `watch realtime`, and
		// the other property-scoped novel commands work in environments where
		// only auth (e.g. GOOGLE_APPLICATION_CREDENTIALS) is set and the user
		// has previously run `ga4-pp-cli sync admin`.
		if p := firstStoreProperty(); p != "" {
			return p, nil
		}
		return "", fmt.Errorf("no property id: pass <property> or set GA_PROPERTY_ID")
	}
	// Normalize "properties/12345" → "12345" so callers don't have to care.
	if strings.HasPrefix(raw, "properties/") {
		raw = strings.TrimPrefix(raw, "properties/")
	}
	// GA4 property IDs are numeric; anything else is a typo or a stray
	// sentinel from a dogfood probe. Rejecting up front gives the caller a
	// usage-style error (exit code != 0) instead of a confusing 404 from the
	// API.
	if !isAllDigits(raw) {
		return "", fmt.Errorf("invalid property id %q: must be numeric (e.g. 12345)", raw)
	}
	return raw, nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// firstStoreProperty returns the lowest-numbered property_id from the local
// SQLite store, or "" when the store is missing or empty. Best-effort and
// silent: errors collapse to "" so the caller falls back to its own error.
func firstStoreProperty() string {
	path := store.DefaultPath()
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	s, err := store.OpenReadOnly(path)
	if err != nil {
		return ""
	}
	defer s.Close()
	var prop string
	if err := s.DB().QueryRow(`SELECT property_id FROM properties ORDER BY property_id LIMIT 1`).Scan(&prop); err != nil {
		return ""
	}
	return strings.TrimSpace(prop)
}
