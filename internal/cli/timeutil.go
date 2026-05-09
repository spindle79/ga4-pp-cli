// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

package cli

import "time"

func timeNowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
