// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"ga4-pp-cli/internal/client"
)

// clientAdapter satisfies clientForRunReport using the real client.Client.
// Hand-authored commands talk to it via the small interface so ga4helpers.go
// can stay generic.
type clientAdapter struct{ inner *client.Client }

func newClientAdapter(c *client.Client) *clientAdapter { return &clientAdapter{inner: c} }

func (a *clientAdapter) post(path string, body any) ([]byte, int, error) {
	raw, code, err := a.inner.Post(path, body)
	return []byte(raw), code, err
}

func (a *clientAdapter) get(path string, params map[string]string) ([]byte, error) {
	raw, err := a.inner.Get(path, params)
	return []byte(raw), err
}
