# Google Analytics 4 CLI

> GA4 isn't just a web analytics tool. It's a behavior fingerprint across acquisition, engagement, and monetization surfaces. Every page hit, session, and conversion is a signal about how visitors really use the product — the `pages`, `funnel`, `drift`, `watch`, `traffic-anomalies`, and `bot-traffic` commands are how you read that fingerprint.

**Every GA4 Data API endpoint, plus saved report profiles, drift detection, schema search, and quality guards no other GA4 tool ships.**

Built on top of the full GA4 Data API v1beta surface (runReport, runRealtimeReport, runPivotReport, batch, checkCompatibility, getMetadata) and absorbs the URL-centric tools from the spindle79 ga4-mcp-server. On top of that it adds saved report profiles, period-over-period drift detection, an FTS schema browser, a realtime watch loop, and automatic sampling/(other)/quota warnings on every response.

Learn more at [Google Analytics 4](https://google.com).

Printed by [@spindle79](https://github.com/spindle79) (Adam Harris).

## Install

The recommended path installs both the `ga4-pp-cli` binary and the `pp-ga4` agent skill in one shot:

```bash
npx -y @mvanhorn/printing-press install ga4
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press install ga4 --cli-only
```


### Without Node

The generated install path is category-agnostic until this CLI is published. If `npx` is not available before publish, install Node or use the category-specific Go fallback from the public-library entry after publish.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/ga4-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-ga4 --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-ga4 --force
```

## Install for OpenClaw

Tell your OpenClaw agent (copy this):

```
Install the pp-ga4 skill from https://github.com/mvanhorn/printing-press-library/tree/main/cli-skills/pp-ga4. The skill defines how its required CLI can be installed.
```

## Authentication

Authenticate with a Google service-account JSON. Set GOOGLE_APPLICATION_CREDENTIALS to the file path and GA_PROPERTY_ID to your default property (or pass --property-id per call). The service account needs the Viewer role on the GA4 property.

## Quick Start

```bash
# Validates SA JSON, mints a token, hits getMetadata to confirm the property is reachable.
ga4-pp-cli doctor


# One-time sync of dimensions and metrics (including custom) into the local store.
ga4-pp-cli schema fetch


# Find the metric you actually want before writing a runReport call.
ga4-pp-cli schema search "session" --agent


# URL-centric pageviews — the spindle79 MCP shape, plus regex/contains match types and --select.
ga4-pp-cli pages views /blog/launch --timeframe last-7-days --agent


# Top page movers vs prior week in one command.
ga4-pp-cli drift pages --window 7d --top 20 --agent

```

## Unique Features

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

### Offline local store (SQLite)
- **`sync schema` / `sync pages` / `sync properties`** — Populate a local SQLite store at `$PRESS_DATA_DIR/ga4/data.db` (falls back to `~/.local/share/ga4-pp-cli/data.db`). Tables: `properties`, `dimensions`, `metrics`, `pages_daily`, `sync_state`, with FTS5 mirrors for dimensions and metrics.

  _When an agent asks the same question hourly, the repeat queries hit local SQLite instead of burning GA4 quota._

  ```bash
  ga4-pp-cli sync schema --agent
  ga4-pp-cli sync pages --days 30 --agent
  ```
- **`search <query>`** — FTS5 over dimensions, metrics, and synced page paths in one call. Honors `--data-source local|auto|live`.

  ```bash
  ga4-pp-cli search engagement --agent
  ```
- **`sql "<SELECT …>"`** — Read-only raw SQL over the local store. Refuses anything that isn't `SELECT/WITH/EXPLAIN/PRAGMA`.

  ```bash
  ga4-pp-cli sql "SELECT page_path, SUM(sessions) FROM pages_daily GROUP BY page_path ORDER BY 2 DESC LIMIT 10"
  ```
- **`traffic-anomalies --days 30 --threshold 2.0`** — Per-page z-scores over the rolling-window sessions series; flags pages whose latest day exceeds the threshold.
- **`bot-traffic --days 7`** — Heuristic scan of `pages_daily` for near-zero engagement, near-zero session duration, and high sessions-per-user, ranked by total sessions.

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

## Cookbook

Each recipe is a self-contained agent workflow. Commands run as-is once
`GOOGLE_APPLICATION_CREDENTIALS` and (optionally) `GA_PROPERTY_ID` are set.

1. **Discover every property the service account can see.**

   ```bash
   ga4-pp-cli accounts summaries --ids-only --agent
   ```

2. **Find the right metric without guessing the apiName.**

   ```bash
   ga4-pp-cli sync schema 12345 --agent
   ga4-pp-cli schema search "engagement rate" --agent
   ```

3. **Top 20 pages over the last 7 days.**

   ```bash
   ga4-pp-cli pages views / --timeframe last-7-days --top 20 --agent
   ```

4. **Detect a traffic anomaly today vs the 30-day baseline.**

   ```bash
   ga4-pp-cli sync pages 12345 --days 30 --agent
   ga4-pp-cli traffic-anomalies --days 30 --threshold 2.0 --agent
   ```

5. **Spot bot or scraper traffic in the last week.**

   ```bash
   ga4-pp-cli bot-traffic --days 7 --agent
   ```

6. **Watch realtime during a product launch.**

   ```bash
   ga4-pp-cli watch realtime --interval 30s --top 10 --agent
   ```

7. **Schedule a weekly content review with a saved template.**

   ```bash
   ga4-pp-cli templates save weekly-content --dimensions pagePath,pageTitle --metrics sessions,engagementRate
   ga4-pp-cli templates run weekly-content --agent
   ```

8. **Period-over-period drift report for top pages.**

   ```bash
   ga4-pp-cli drift pages --window 7d --top 20 --agent
   ```

9. **Export pages_daily to a CSV for a spreadsheet review.**

   ```bash
   ga4-pp-cli export --table pages_daily --csv > pages.csv
   ```

10. **Run an ad-hoc SQL aggregate against the synced store.**

    ```bash
    ga4-pp-cli sql "SELECT page_path, SUM(sessions) FROM pages_daily GROUP BY 1 ORDER BY 2 DESC LIMIT 10" --agent
    ```

11. **Audit configuration before debugging a missing-data report.**

    ```bash
    ga4-pp-cli data-streams 12345 --agent
    ga4-pp-cli key-events 12345 --agent
    ga4-pp-cli attribution-settings 12345 --agent
    ```

12. **Submit a 90-day report job and pick up the result later.**

    ```bash
    JOB=$(ga4-pp-cli jobs submit 12345 --days 90 --agent | jq -r .id)
    ga4-pp-cli jobs wait "$JOB" --timeout 5m --agent
    ```

## Usage

Run `ga4-pp-cli --help` for the full command reference and flag list.

## Commands

### properties

Manage properties

- **`ga4-pp-cli properties batch-run-pivot-reports`** - Returns multiple pivot reports in a batch. All reports must be for the same GA4 Property.
- **`ga4-pp-cli properties batch-run-reports`** - Returns multiple reports in a batch. All reports must be for the same GA4 Property.
- **`ga4-pp-cli properties check-compatibility`** - This compatibility method lists dimensions and metrics that can be added to a report request and maintain compatibility. This method fails if the request's dimensions and metrics are incompatible. In Google Analytics, reports fail if they request incompatible dimensions and/or metrics; in that case, you will need to remove dimensions and/or metrics from the incompatible report until the report is compatible. The Realtime and Core reports have different compatibility rules. This method checks compatibility for Core reports.
- **`ga4-pp-cli properties get-metadata`** - Returns metadata for dimensions and metrics available in reporting methods. Used to explore the dimensions and metrics. In this method, a Google Analytics GA4 Property Identifier is specified in the request, and the metadata response includes Custom dimensions and metrics as well as Universal metadata. For example if a custom metric with parameter name `levels_unlocked` is registered to a property, the Metadata response will contain `customEvent:levels_unlocked`. Universal metadata are dimensions and metrics applicable to any property such as `country` and `totalUsers`.
- **`ga4-pp-cli properties run-pivot-report`** - Returns a customized pivot report of your Google Analytics event data. Pivot reports are more advanced and expressive formats than regular reports. In a pivot report, dimensions are only visible if they are included in a pivot. Multiple pivots can be specified to further dissect your data.
- **`ga4-pp-cli properties run-realtime-report`** - Returns a customized report of realtime event data for your property. Events appear in realtime reports seconds after they have been sent to the Google Analytics. Realtime reports show events and usage data for the periods of time ranging from the present moment to 30 minutes ago (up to 60 minutes for Google Analytics 360 properties). For a guide to constructing realtime requests & understanding responses, see [Creating a Realtime Report](https://developers.google.com/analytics/devguides/reporting/data/v1/realtime-basics).
- **`ga4-pp-cli properties run-report`** - Returns a customized report of your Google Analytics event data. Reports contain statistics derived from data collected by the Google Analytics tracking code. The data returned from the API is as a table with columns for the requested dimensions and metrics. Metrics are individual measurements of user activity on your property, such as active users or event count. Dimensions break down metrics across some common criteria, such as country or event name. For a guide to constructing requests & understanding responses, see [Creating a Report](https://developers.google.com/analytics/devguides/reporting/data/v1/basics).


## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
ga4-pp-cli properties batch-run-pivot-reports mock-value

# JSON for scripting and agents
ga4-pp-cli properties batch-run-pivot-reports mock-value --json

# Filter to specific fields
ga4-pp-cli properties batch-run-pivot-reports mock-value --json --select id,name,status

# Dry run — show the request without sending
ga4-pp-cli properties batch-run-pivot-reports mock-value --dry-run

# Agent mode — JSON + compact + no prompts in one flag
ga4-pp-cli properties batch-run-pivot-reports mock-value --agent
```

## Agent Usage

This CLI is designed for AI agent consumption:

- **Non-interactive** - never prompts, every input is a flag
- **Pipeable** - `--json` output to stdout, errors to stderr
- **Filterable** - `--select id,name` returns only fields you need
- **Previewable** - `--dry-run` shows the request without sending
- **Explicit retries** - add `--idempotent` to create retries when a no-op success is acceptable
- **Confirmable** - `--yes` for explicit confirmation of destructive actions
- **Piped input** - write commands can accept structured input when their help lists `--stdin`
- **Agent-safe by default** - no colors or formatting unless `--human-friendly` is set

Exit codes (Printing Press convention): `0` success, `2` usage error (bad flags, missing args, bad config), `3` auth error (401/403, missing credentials), `4` not found (404, unknown resource), `5` rate limited (429), `7` server error (5xx, generic API failure).

## Use with Claude Code

Install the focused skill — it auto-installs the CLI on first invocation:

```bash
npx skills add mvanhorn/printing-press-library/cli-skills/pp-ga4 -g
```

Then invoke `/pp-ga4 <query>` in Claude Code. The skill is the most efficient path — Claude Code drives the CLI directly without an MCP server in the middle.

<details>
<summary>Use as an MCP server in Claude Code (advanced)</summary>

If you'd rather register this CLI as an MCP server in Claude Code, install the MCP binary first:


Install the MCP binary from this CLI's published public-library entry or pre-built release.

Then register it:

```bash
claude mcp add ga4 ga4-pp-mcp -e GOOGLE_ANALYTICS_DATA_OAUTH2C=<your-token>
```

</details>

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/ga4-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.
3. Fill in `GOOGLE_ANALYTICS_DATA_OAUTH2C` when Claude Desktop prompts you.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.


Install the MCP binary from this CLI's published public-library entry or pre-built release.

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "ga4": {
      "command": "ga4-pp-mcp",
      "env": {
        "GOOGLE_ANALYTICS_DATA_OAUTH2C": "<your-key>"
      }
    }
  }
}
```

</details>

## Health Check

```bash
ga4-pp-cli doctor
```

Verifies configuration, credentials, and connectivity to the API.

## Configuration

Config file: `~/.config/google-analytics-data-pp-cli/config.toml`

Static request headers can be configured under `headers`; per-command header overrides take precedence.

Environment variables:

| Name | Kind | Required | Description |
| --- | --- | --- | --- |
| `GOOGLE_ANALYTICS_DATA_OAUTH2C` | per_call | Yes | Set to your API credential. |

## Troubleshooting
**Authentication errors (exit code 3)**
- Run `ga4-pp-cli doctor` to check credentials
- Verify the environment variable is set: `echo $GOOGLE_ANALYTICS_DATA_OAUTH2C`

**Not found errors (exit code 4)**
- Check the resource ID is correct
- Run the `list` command to see available items

**Rate limited (exit code 5)**
- Back off and retry; GA4 quotas reset on the hour for most tokens.

**Server errors (exit code 7)**
- Generic API failure (5xx). Re-run with `--json` to see the raw response.

### API-specific

- **PERMISSION_DENIED / 403** — Add the service account email as a Viewer on the GA4 property (Admin → Property Access Management).
- **GOOGLE_APPLICATION_CREDENTIALS unset** — export GOOGLE_APPLICATION_CREDENTIALS=/absolute/path/to/sa.json — must be a path, not the JSON contents.
- **Sampling active but unnoticed** — Run any report with --agent and check the _warnings array; if sampling_active is present, narrow the date range or reduce dimensions.
- **(other) bucket >50% of rows** — Cardinality is too high for the dimension at this date range — use a more selective filter or a coarser dimension.

---

## Sources & Inspiration

This CLI was built by studying these projects and resources:

- [**google-analytics-mcp (official)**](https://github.com/googleanalytics/google-analytics-mcp) — Python
- [**ga4-mcp-server**](https://github.com/spindle79/ga4-mcp-server) — TypeScript
- [**surendranb/google-analytics-mcp**](https://github.com/surendranb/google-analytics-mcp) — TypeScript
- [**Bin-Huang/google-analytics-cli**](https://github.com/Bin-Huang/google-analytics-cli) — TypeScript
- [**gago**](https://github.com/MarkEdmondson1234/gago) — Go

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
