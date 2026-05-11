// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// GuardReadOnlySQL is the exported alias the MCP package uses to reject
// non-SELECT statements before they hit the store. Keeping the export
// thin lets the CLI command and the typed MCP tool share one predicate
// rather than reimplementing the parser-light guard twice.

package cli

// GuardReadOnlySQL rejects anything that isn't SELECT/WITH/EXPLAIN/PRAGMA.
func GuardReadOnlySQL(q string) error { return guardReadOnlySQL(q) }
