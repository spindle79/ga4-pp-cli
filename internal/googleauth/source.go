// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// Package googleauth wraps Google Application Default Credentials (ADC) into
// a TokenSource scoped to analytics.readonly. The CLI uses service-account
// JSON in production; ADC also resolves user-credentials and metadata-server
// tokens when running on GCP.
package googleauth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Scope required to call the GA4 Data API. Read-only is sufficient for every
// endpoint this CLI exposes; we never mutate property data.
const Scope = "https://www.googleapis.com/auth/analytics.readonly"

var (
	mu          sync.Mutex
	cachedSrc   oauth2.TokenSource
	cachedToken *oauth2.Token
	cachedFrom  string // resolved credential source, surfaced via doctor/auth status
)

// Available reports whether a credential is reachable without minting a token.
// Used by callers that want to decide between Google ADC and a manual OAuth
// fallback before paying the round-trip cost.
func Available() bool {
	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
		return true
	}
	// FindDefaultCredentials also resolves gcloud user creds and the GCE
	// metadata server, so this is the authoritative check.
	_, err := google.FindDefaultCredentials(context.Background(), Scope)
	return err == nil
}

// Source returns a TokenSource for ADC. The source is cached at process scope:
// re-resolving on every request would re-read the SA JSON and rebuild the JWT
// signer, which adds latency on hot paths like watch/realtime polling.
func Source(ctx context.Context) (oauth2.TokenSource, string, error) {
	mu.Lock()
	defer mu.Unlock()
	if cachedSrc != nil {
		return cachedSrc, cachedFrom, nil
	}
	creds, err := google.FindDefaultCredentials(ctx, Scope)
	if err != nil {
		return nil, "", fmt.Errorf("resolving Google credentials: %w", err)
	}
	cachedSrc = creds.TokenSource
	cachedFrom = describeCredsSource(creds)
	return cachedSrc, cachedFrom, nil
}

// Token mints (or returns a cached) bearer token. The wrapping TokenSource
// already caches expiry; we add a thin guard to keep `doctor --json` checks
// idempotent within a few seconds.
func Token(ctx context.Context) (*oauth2.Token, string, error) {
	src, from, err := Source(ctx)
	if err != nil {
		return nil, "", err
	}
	mu.Lock()
	if cachedToken != nil && cachedToken.Valid() && time.Until(cachedToken.Expiry) > 30*time.Second {
		t := cachedToken
		mu.Unlock()
		return t, from, nil
	}
	mu.Unlock()

	tok, err := src.Token()
	if err != nil {
		return nil, from, fmt.Errorf("minting Google access token: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, from, errors.New("Google credentials returned an empty access token")
	}
	mu.Lock()
	cachedToken = tok
	mu.Unlock()
	return tok, from, nil
}

// Reset drops the cached source and token. Used by tests and `auth logout` to
// guarantee the next call resolves credentials again.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	cachedSrc = nil
	cachedToken = nil
	cachedFrom = ""
}

// describeCredsSource returns a short label for doctor/auth status output.
// google.Credentials does not expose the resolution path directly, so we
// infer from the env var and the JSON shape.
func describeCredsSource(creds *google.Credentials) string {
	if path := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); path != "" {
		return "service_account_json:" + path
	}
	if len(creds.JSON) > 0 {
		// gcloud-cached user credentials embed "type":"authorized_user".
		if containsType(creds.JSON, `"type":"authorized_user"`) {
			return "gcloud_user_credentials"
		}
		if containsType(creds.JSON, `"type":"service_account"`) {
			return "service_account_json:adc-discovered"
		}
	}
	return "metadata_server"
}

func containsType(b []byte, needle string) bool {
	if len(b) == 0 {
		return false
	}
	// Cheap substring check; we don't need to parse JSON for a label.
	hay := string(b)
	return len(hay) >= len(needle) && stringIndex(hay, needle) >= 0
}

// stringIndex avoids pulling in strings just for one substring check.
func stringIndex(s, sub string) int {
	if len(sub) > len(s) {
		return -1
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
