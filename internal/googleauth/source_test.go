// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

package googleauth

import "testing"

func TestContainsType(t *testing.T) {
	cases := []struct {
		name   string
		body   []byte
		needle string
		want   bool
	}{
		{"empty body returns false", nil, `"type":"service_account"`, false},
		{"matching service_account", []byte(`{"type":"service_account","client_email":"x"}`), `"type":"service_account"`, true},
		{"matching authorized_user", []byte(`{"type":"authorized_user"}`), `"type":"authorized_user"`, true},
		{"non-matching needle", []byte(`{"type":"service_account"}`), `"type":"authorized_user"`, false},
		{"needle longer than haystack", []byte(`{}`), `"type":"service_account"`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := containsType(tc.body, tc.needle); got != tc.want {
				t.Fatalf("containsType(%q, %q) = %v, want %v", string(tc.body), tc.needle, got, tc.want)
			}
		})
	}
}

func TestStringIndex(t *testing.T) {
	cases := []struct {
		name string
		s    string
		sub  string
		want int
	}{
		{"sub at start", "abcdef", "abc", 0},
		{"sub in middle", "abcdef", "cde", 2},
		{"sub at end", "abcdef", "ef", 4},
		{"sub not present", "abcdef", "xyz", -1},
		{"sub longer than s", "ab", "abcd", -1},
		{"empty sub matches at 0", "abc", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stringIndex(tc.s, tc.sub); got != tc.want {
				t.Fatalf("stringIndex(%q, %q) = %d, want %d", tc.s, tc.sub, got, tc.want)
			}
		})
	}
}

func TestResetClearsCache(t *testing.T) {
	// Manually seed cache to verify Reset clears it without depending on real
	// credential resolution (which would require live Google ADC).
	mu.Lock()
	cachedFrom = "test-source"
	mu.Unlock()

	Reset()

	mu.Lock()
	defer mu.Unlock()
	if cachedSrc != nil || cachedToken != nil || cachedFrom != "" {
		t.Fatalf("Reset did not clear cache: src=%v token=%v from=%q", cachedSrc, cachedToken, cachedFrom)
	}
}

func TestScopeIsReadOnly(t *testing.T) {
	// Documenting invariant: this CLI never requests write scopes.
	want := "https://www.googleapis.com/auth/analytics.readonly"
	if Scope != want {
		t.Fatalf("Scope = %q, want %q (CLI must never request write scopes)", Scope, want)
	}
}
