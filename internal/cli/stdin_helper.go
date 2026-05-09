// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"io"
	"os"
)

func readAllFromStdin() ([]byte, error) {
	return io.ReadAll(os.Stdin)
}
