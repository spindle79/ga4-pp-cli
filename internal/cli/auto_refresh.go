// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// auto_refresh.go wires `cliutil.EnsureFresh` into the root command's
// PersistentPreRunE so any read command running under `--data-source auto`
// gets a heads-up when the local cache is stale. The trigger is policy-only:
// staleness emits a one-line warning to stderr and (best-effort) kicks an
// out-of-process `sync` so the next invocation reads fresh data. It never
// blocks the current command — agents would rather see slightly-stale
// numbers than wait on a background fetch.
//
// Auto-refresh is a no-op when:
//   - --data-source != "auto" (live and local opt out by definition)
//   - the command being run is itself a sync/doctor/auth/feedback subcommand
//   - GA4_DISABLE_AUTO_REFRESH=1 is set in the environment
//   - the local store file doesn't exist yet (no point checking freshness)

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"ga4-pp-cli/internal/cliutil"
	"ga4-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

// defaultStaleAge is how old sync_state.last_run_at must be before
// auto-refresh fires. Picked at 6h because GA4 has a 24h+ ingestion lag
// for backfilled data anyway, so a sub-six-hour cursor is "fresh enough"
// for analytical reads.
const defaultStaleAge = 6 * time.Hour

// autoRefreshIfStale checks the freshness of the local store for the
// scopes relevant to the current command and, if stale, prints a
// one-line WARN to stderr and detaches a `sync` subprocess so the next
// invocation gets fresh data. Never blocks the current command.
//
// The function is safe to call from PersistentPreRunE on any command;
// it self-disables for sync/doctor/auth/feedback subcommands.
func autoRefreshIfStale(cmd *cobra.Command, flags *rootFlags) {
	if flags == nil || flags.dataSource != "auto" {
		return
	}
	if os.Getenv("GA4_DISABLE_AUTO_REFRESH") == "1" {
		return
	}
	cmdPath := cmd.CommandPath()
	for _, skip := range []string{" sync", " doctor", " auth", " feedback", " agent-context", " profile", " version", " import"} {
		if strings.Contains(cmdPath, skip) {
			return
		}
	}
	path := store.DefaultPath()
	if _, err := os.Stat(path); err != nil {
		return // no store yet — first-run is intentionally a no-op
	}
	s, err := store.OpenReadOnly(path)
	if err != nil {
		return
	}
	defer s.Close()

	// Per-scope freshness: we refresh whichever scope the running
	// command actually consumes. Conservative default: only refresh
	// "pages" — it's the firehose and the only scope that changes
	// every hour. Schema and properties barely move.
	scope := scopeForCommandPath(cmdPath)
	stale, _, err := cliutil.EnsureFresh(cmd.Context(), s.DB(), "", scope, defaultStaleAge)
	if err != nil || !stale {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(),
		"WARN local store scope %q is stale (>%s) — kicking background 'sync %s'; set GA4_DISABLE_AUTO_REFRESH=1 to silence\n",
		scope, defaultStaleAge, scope,
	)
	kickBackgroundSync(scope)
}

// scopeForCommandPath maps a command path like "ga4-pp-cli traffic-anomalies"
// to the sync_state scope it should consider for freshness. The default is
// "pages" because that's the scope that actually goes stale on a human
// timescale; schema and properties rarely change.
func scopeForCommandPath(p string) string {
	switch {
	case strings.Contains(p, " schema "), strings.HasSuffix(p, " schema"):
		return "dimensions"
	case strings.Contains(p, " accounts "), strings.HasSuffix(p, " accounts"):
		return "properties"
	default:
		return "pages"
	}
}

// kickBackgroundSync best-effort spawns `<self> sync <scope> --agent` and
// detaches. Output is discarded; if the spawn fails (binary not found,
// permission error) we silently give up — auto-refresh is policy, not
// guarantee.
func kickBackgroundSync(scope string) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	// We intentionally let `cancel` leak into the goroutine that owns the
	// process — once the process exits the goroutine wakes, calls cancel,
	// and returns.
	go func() {
		defer cancel()
		cmd := exec.CommandContext(ctx, self, "sync", scope, "--agent")
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		_ = cmd.Run()
	}()
}
