---
name: pp-ga4
description: "Every GA4 Data API endpoint, plus saved report profiles, drift detection, schema search, and quality guards no other... Trigger phrases: `ga4 report`, `google analytics report`, `what's hot on ga4`, `page views for`, `top movers ga4`, `use ga4-pp-cli`, `run ga4-pp-cli`."
author: "Adam Harris"
license: "Apache-2.0"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - ga4-pp-cli
---

# Google Analytics 4 — Printing Press CLI

> GA4 isn't just a web analytics tool. It's a behavior fingerprint across acquisition, engagement, and monetization surfaces. Every page hit, session, and conversion is a signal about how visitors really use the product — the `pages`, `funnel`, `drift`, `watch`, `traffic-anomalies`, and `bot-traffic` commands are how you read that fingerprint.

## Prerequisites: Install the CLI

This skill drives the `ga4-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer:
   ```bash
   npx -y @mvanhorn/printing-press install ga4 --cli-only
   ```
2. Verify: `ga4-pp-cli --version`
3. Ensure `$GOPATH/bin` (or `$HOME/go/bin`) is on `$PATH`.

If the `npx` install fails before this CLI has a public-library category, install Node or use the category-specific Go fallback after publish.

If `--version` reports "command not found" after install, the install step did not put the binary on `$PATH`. Do not proceed with skill commands until verification succeeds.

Built on top of the full GA4 Data API v1beta surface (runReport, runRealtimeReport, runPivotReport, batch, checkCompatibility, getMetadata) and absorbs the URL-centric tools from the spindle79 ga4-mcp-server. On top of that it adds saved report profiles, period-over-period drift detection, an FTS schema browser, a realtime watch loop, and automatic sampling/(other)/quota warnings on every response.

## When to Use This CLI

Use this CLI when you need GA4 Data API access from a script or agent, when you want offline-cached schema search across custom dimensions, or when you need period-over-period drift without composing two requests by hand. Use the GA4 web UI for funnel exploration with custom segments and for reports requiring user-interface filters that the Data API doesn't support.

## Unique Capabilities

These capabilities aren't available in any other tool for this API.

### Local state that compounds
- **`templates run`** — Save GA4 report templates by name and re-run them with one command.

  _When an agent needs the same report repeatedly (weekly content review, daily realtime check), a one-word recall beats re-typing 10 flags._

  ```bash
  ga4-pp-cli templates run weekly-content --agent
  ```
- **`drift pages`** — Show top page-level movers between two periods in one command.

  _Period-over-period analysis is the #1 GA4 web UI pain point; this collapses it into one command._

  ```bash
  ga4-pp-cli drift pages --window 7d --top 20 --agent
  ```

### Agent-native plumbing
- **`watch realtime`** — Poll runRealtimeReport on an interval and stream deltas as agent-friendly JSON.

  _Lets agents react to traffic spikes during launches without re-querying the same data over and over._

  ```bash
  ga4-pp-cli watch realtime --interval 30s --top 10 --agent
  ```
- **`schema search`** — Find the right GA4 dimension or metric by phrase, including custom dimensions/metrics for the property.

  _Stops agents hallucinating field names. Existing MCPs require you to know the apiName before calling._

  ```bash
  ga4-pp-cli schema search "session" --agent
  ```

### GA4-aware quality
- **`properties run-report`** — Every report response auto-surfaces sampling, (other)-bucket overflow, and remaining quota tokens as structured warnings.

  _Three of the four well-known GA4 data-quality gotchas — sampling, (other) bucket overflow, quota exhaustion — automatically surfaced on every call._

  ```bash
  ga4-pp-cli properties run-report --date-ranges '[{"startDate":"30daysAgo","endDate":"today"}]' --dimensions '[{"name":"pagePath"}]' --metrics '[{"name":"screenPageViews"}]' --agent
  ```
- **`templates compat`** — Run checkCompatibility for every metric in a saved report profile, returning a dimension-vs-metric compatibility matrix.

  _When a report 400s with 'incompatible dimension', this tells you exactly which combo broke instead of trial-and-erroring 10 metric variants._

  ```bash
  ga4-pp-cli templates compat weekly-content --agent
  ```

## Command Reference

**properties** — Manage properties

- `ga4-pp-cli properties batch-run-pivot-reports` — Returns multiple pivot reports in a batch. All reports must be for the same GA4 Property.
- `ga4-pp-cli properties batch-run-reports` — Returns multiple reports in a batch. All reports must be for the same GA4 Property.
- `ga4-pp-cli properties check-compatibility` — This compatibility method lists dimensions and metrics that can be added to a report request and maintain...
- `ga4-pp-cli properties get-metadata` — Returns metadata for dimensions and metrics available in reporting methods. Used to explore the dimensions and...
- `ga4-pp-cli properties run-pivot-report` — Returns a customized pivot report of your Google Analytics event data. Pivot reports are more advanced and...
- `ga4-pp-cli properties run-realtime-report` — Returns a customized report of realtime event data for your property. Events appear in realtime reports seconds...
- `ga4-pp-cli properties run-report` — Returns a customized report of your Google Analytics event data. Reports contain statistics derived from data...


### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
ga4-pp-cli which "<capability in your own words>"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query.

## Recipes


### Top performing pages this week

```bash
ga4-pp-cli top pages --period 7d --agent
```

Local-first answer to "what are my best pages right now". Returns rank + page_path + sessions from the synced pages_daily table; auto-refreshes if the local store is older than 6h. <100ms warm.

### Where is my traffic coming from

```bash
ga4-pp-cli top sources --period 7d --agent
```

Top traffic sources by sessions across source/medium pairs (organic/google, direct/(none), etc). Same table-of-rows shape as `top pages`. Use `--metric total_users` for unique-visitor ranking instead.

### What events are firing most

```bash
ga4-pp-cli top events --period 7d --agent
```

Top events by event_count. Reads events_daily — covers page_view, session_start, click, scroll, and every custom event firing on the property.

### Top countries by traffic

```bash
ga4-pp-cli top countries --period 7d --agent
```

Top countries by sessions. Pulls from devices_geo_daily (no separate countries table needed — the (date, device_category, country) PK keeps both pivots correct).

### Mobile vs desktop split

```bash
ga4-pp-cli top devices --period 7d --agent
```

Sessions broken out by device_category (desktop/mobile/tablet). Pair with `top countries` for a "where's my mobile traffic coming from" answer.

### Top performing campaigns

```bash
ga4-pp-cli top campaigns --period 7d --agent
```

Top sessionCampaignName values by sessions. Reads acquisition_daily. `(direct)` rolls up no-campaign sessions so the head of the list is your real marketing performance.

### Top movers this week

```bash
ga4-pp-cli drift pages --window 7d --top 20 --agent --select rows.pagePath,rows.screenPageViews_delta_pct
```

Runs current and prior 7-day windows in one batch, joins on pagePath, returns the largest absolute % deltas. Use --select to drop everything except the columns you actually want.

### Find the right metric before querying

```bash
ga4-pp-cli schema search "session" --agent
```

FTS5 over the local metadata cache returns matching dimensions/metrics including custom ones (customEvent:*). Run schema fetch once first.

### Realtime launch monitoring

```bash
ga4-pp-cli watch realtime --interval 30s --top 10 --agent
```

Streams a JSON delta every 30 seconds showing what's hot in the last 30 minutes. Each tick highlights new entrants and rank changes.

### Save a weekly content report

```bash
ga4-pp-cli templates save weekly-content --dimensions pagePath,country --metrics screenPageViews,engagementRate --date-range 7daysAgo,today
```

Stores the report definition. Re-run with `templates run weekly-content` — no need to re-type the flags.

### Why is my report 400ing?

```bash
ga4-pp-cli templates compat weekly-content --agent
```

Calls checkCompatibility for every metric in the saved profile and returns a matrix of which metric/dimension combos are compatible.

## Auth Setup

Authenticate with a Google service-account JSON. Set GOOGLE_APPLICATION_CREDENTIALS to the file path and GA_PROPERTY_ID to your default property (or pass --property-id per call). The service account needs the Viewer role on the GA4 property.

Run `ga4-pp-cli doctor` to verify setup.

## Agent Mode

Add `--agent` to any command. Expands to: `--json --compact --no-input --no-color --yes`.

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select` keeps a subset of fields. Dotted paths descend into nested structures; arrays traverse element-wise. Critical for keeping context small on verbose APIs:

  ```bash
  ga4-pp-cli properties batch-run-pivot-reports mock-value --agent --select id,name,status
  ```
- **Previewable** — `--dry-run` shows the request without sending
- **Non-interactive** — never prompts, every input is a flag
- **Explicit retries** — use `--idempotent` only when an already-existing create should count as success

## Agent Feedback

When you (or the agent) notice something off about this CLI, record it:

```
ga4-pp-cli feedback "the --since flag is inclusive but docs say exclusive"
ga4-pp-cli feedback --stdin < notes.txt
ga4-pp-cli feedback list --json --limit 10
```

Entries are stored locally at `~/.ga4-pp-cli/feedback.jsonl`. They are never POSTed unless `GA4_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `GA4_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

Write what *surprised* you, not a bug report. Short, specific, one line: that is the part that compounds.

## Output Delivery

Every command accepts `--deliver <sink>`. The output goes to the named sink in addition to (or instead of) stdout, so agents can route command results without hand-piping. Three sinks are supported:

| Sink | Effect |
|------|--------|
| `stdout` | Default; write to stdout only |
| `file:<path>` | Atomically write output to `<path>` (tmp + rename) |
| `webhook:<url>` | POST the output body to the URL (`application/json` or `application/x-ndjson` when `--compact`) |

Unknown schemes are refused with a structured error naming the supported set. Webhook failures return non-zero and log the URL + HTTP status on stderr.

## Named Profiles

A profile is a saved set of flag values, reused across invocations. Use it when a scheduled agent calls the same command every run with the same configuration - HeyGen's "Beacon" pattern.

```
ga4-pp-cli profile save briefing --json
ga4-pp-cli --profile briefing properties batch-run-pivot-reports mock-value
ga4-pp-cli profile list --json
ga4-pp-cli profile show briefing
ga4-pp-cli profile delete briefing --yes
```

Explicit flags always win over profile values; profile values win over defaults. `agent-context` lists all available profiles under `available_profiles` so introspecting agents discover them at runtime.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Usage error (wrong arguments) |
| 3 | Resource not found |
| 4 | Authentication required |
| 5 | API error (upstream issue) |
| 7 | Rate limited (wait and retry) |
| 10 | Config error |

## Argument Parsing

Parse `$ARGUMENTS`:

1. **Empty, `help`, or `--help`** → show `ga4-pp-cli --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

Install the MCP binary from this CLI's published public-library entry or pre-built release, then register it:

```bash
claude mcp add ga4-pp-mcp -- ga4-pp-mcp
```

Verify: `claude mcp list`

## Direct Use

1. Check if installed: `which ga4-pp-cli`
   If not found, offer to install (see Prerequisites at the top of this skill).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   ga4-pp-cli <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `ga4-pp-cli <command> --help`.
