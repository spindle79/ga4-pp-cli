// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

package cli

import "encoding/json"

// jsonMarshalIndent is a thin wrapper so multiple files can call it without
// each importing encoding/json with a separate alias when most of the file
// is non-JSON code.
func jsonMarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(v, prefix, indent)
}
