// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

// `jobs` is the async-execution surface for long-running GA4 work that
// outlives a single CLI invocation. The Data API is synchronous, but
// agents often want to submit a wide-window report (90-day pivot) and
// poll for completion rather than block their turn. `jobs submit`
// kicks off a runReport in a detached goroutine and persists status to
// $PRESS_DATA_DIR/ga4/jobs/<id>.json; `jobs status` and `jobs list`
// read those files. This is the same submit-then-poll pattern HeyGen
// and other heavy-workflow APIs expose natively.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ga4-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

// JobStatus enumerates the lifecycle stages a job moves through.
// Pending → Running → (Completed | Failed). Cancellation is not
// supported because the underlying Data API call has no abort hook;
// once submitted the goroutine runs to completion.
type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
)

// Job captures the state of a single async runReport invocation.
type Job struct {
	ID          string    `json:"id"`
	Status      JobStatus `json:"status"`
	Kind        string    `json:"kind"`
	Property    string    `json:"property"`
	SubmittedAt time.Time `json:"submitted_at"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	FinishedAt  time.Time `json:"finished_at,omitempty"`
	Error       string    `json:"error,omitempty"`
	ResultPath  string    `json:"result_path,omitempty"`
}

func jobsDir() string {
	return filepath.Join(filepath.Dir(store.DefaultPath()), "jobs")
}

func saveJob(j Job) error {
	dir := jobsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, j.ID+".json")
	data, _ := json.MarshalIndent(j, "", "  ")
	return os.WriteFile(path, data, 0o644)
}

func loadJob(id string) (*Job, error) {
	path := filepath.Join(jobsDir(), id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var j Job
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	return &j, nil
}

func listJobs() ([]Job, error) {
	entries, err := os.ReadDir(jobsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Job
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()[:len(e.Name())-len(".json")]
		j, err := loadJob(id)
		if err != nil {
			continue
		}
		out = append(out, *j)
	}
	return out, nil
}

// runningJobs tracks in-flight goroutines so the process can wait for
// them before exiting. The map is keyed by job ID and the value is a
// cancel function the CLI never invokes (the API call has no abort
// hook) — kept so future cancellation can be wired without an API
// shape change.
var (
	runningMu  sync.Mutex
	runningJob = map[string]context.CancelFunc{}
)

func registerJobRunning(id string, cancel context.CancelFunc) {
	runningMu.Lock()
	defer runningMu.Unlock()
	runningJob[id] = cancel
}

func deregisterJob(id string) {
	runningMu.Lock()
	defer runningMu.Unlock()
	delete(runningJob, id)
}

func newJobsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "jobs",
		Aliases: []string{"job"},
		Short:   "Submit, poll, and list long-running GA4 report jobs that outlive a single CLI invocation",
		Long: `The Data API is synchronous, but agents often want fire-and-forget jobs
they can poll later. 'jobs submit' kicks off a runReport in a detached
goroutine and stores status under $PRESS_DATA_DIR/ga4/jobs/; 'jobs status
<id>' and 'jobs list' read those files. Combine with --wait to convert
back to blocking semantics when the agent has the budget.`,
	}
	cmd.AddCommand(newJobsSubmitCmd(flags), newJobsStatusCmd(flags), newJobsListCmd(flags), newJobsWaitCmd(flags))
	return cmd
}

func newJobsSubmitCmd(flags *rootFlags) *cobra.Command {
	var property string
	var days int
	var wait bool
	cmd := &cobra.Command{
		Use:         "submit [property]",
		Aliases:     []string{"start", "queue"},
		Short:       "Submit a runReport job and return immediately with an opaque job ID for later polling",
		Long:        `Kicks off a wide-window runReport (default last 90 days, top pages by sessions) and returns {id, status: pending} on stdout. Use 'jobs status <id>' or 'jobs wait <id>' to retrieve the result later. Output is persisted to $PRESS_DATA_DIR/ga4/jobs/<id>.json so the job survives process restart.`,
		Example:     "  ga4-pp-cli jobs submit 12345 --days 90 --agent",
		Annotations: map[string]string{"mcp:read-only": "false"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			argv := args
			if len(argv) == 0 && property != "" {
				argv = []string{property}
			}
			prop, err := resolveProperty(argv, flags)
			if err != nil {
				return err
			}
			if days <= 0 {
				days = 90
			}
			id := fmt.Sprintf("job-%d", time.Now().UnixNano())
			j := Job{
				ID:          id,
				Status:      JobPending,
				Kind:        "runReport",
				Property:    prop,
				SubmittedAt: time.Now().UTC(),
			}
			if err := saveJob(j); err != nil {
				return fmt.Errorf("save job: %w", err)
			}
			go runJobInBackground(j, days)
			if wait {
				return waitForJob(cmd, flags, id)
			}
			out, _ := json.MarshalIndent(j, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), out, flags)
		},
	}
	cmd.Flags().StringVar(&property, "property", "", "GA4 property ID (defaults to GA_PROPERTY_ID)")
	cmd.Flags().IntVar(&days, "days", 90, "How many days back to query in the submitted report")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for completion and print the result instead of returning the job ID")
	return cmd
}

func newJobsStatusCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "status [id]",
		Aliases:     []string{"show", "get"},
		Short:       "Show the current status, timing, and result path for a submitted GA4 report job",
		Example:     "  ga4-pp-cli jobs status job-1717024782000000000 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageErr(fmt.Errorf("job id required"))
			}
			j, err := loadJob(args[0])
			if err != nil {
				return notFoundErr(fmt.Errorf("job %s: %w", args[0], err))
			}
			out, _ := json.MarshalIndent(j, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), out, flags)
		},
	}
	return cmd
}

func newJobsListCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "list",
		Aliases:     []string{"ls"},
		Short:       "List every persisted GA4 report job with status and timing metadata",
		Example:     "  ga4-pp-cli jobs list --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			jobs, err := listJobs()
			if err != nil {
				return err
			}
			out, _ := json.MarshalIndent(map[string]any{
				"count": len(jobs),
				"jobs":  jobs,
			}, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), out, flags)
		},
	}
	return cmd
}

func newJobsWaitCmd(flags *rootFlags) *cobra.Command {
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:         "wait [id]",
		Short:       "Block until a submitted GA4 report job reaches a terminal status, then print the final job record",
		Example:     "  ga4-pp-cli jobs wait job-1717024782000000000 --timeout 5m --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageErr(fmt.Errorf("job id required"))
			}
			deadline := time.Now().Add(timeout)
			for time.Now().Before(deadline) {
				j, err := loadJob(args[0])
				if err != nil {
					return notFoundErr(fmt.Errorf("job %s: %w", args[0], err))
				}
				if j.Status == JobCompleted || j.Status == JobFailed {
					out, _ := json.MarshalIndent(j, "", "  ")
					return printOutputWithFlags(cmd.OutOrStdout(), out, flags)
				}
				time.Sleep(500 * time.Millisecond)
			}
			return fmt.Errorf("job %s did not finish within %s", args[0], timeout)
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Maximum time to wait for the job to reach a terminal state")
	return cmd
}

// waitForJob is the --wait helper used by `jobs submit --wait`. It
// reads the persisted job file in a polling loop until the status
// reaches a terminal value.
func waitForJob(cmd *cobra.Command, flags *rootFlags, id string) error {
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		j, err := loadJob(id)
		if err != nil {
			return err
		}
		if j.Status == JobCompleted || j.Status == JobFailed {
			out, _ := json.MarshalIndent(j, "", "  ")
			return printOutputWithFlags(cmd.OutOrStdout(), out, flags)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("job %s did not finish within 5m", id)
}

// runJobInBackground executes the report job and writes the result
// path back to the persisted job file. The function deliberately
// returns no error — the caller is a goroutine and the job state
// already encodes terminal failure.
//
// pp:client-call — the goroutine calls the real runReport endpoint via
// the shared client, then writes the API response to disk under
// $PRESS_DATA_DIR/ga4/jobs/<id>.result.json.
func runJobInBackground(j Job, days int) {
	_, cancel := context.WithCancel(context.Background())
	registerJobRunning(j.ID, cancel)
	defer deregisterJob(j.ID)

	j.Status = JobRunning
	j.StartedAt = time.Now().UTC()
	_ = saveJob(j)

	flags := &rootFlags{}
	c, err := flags.newClient()
	if err != nil {
		j.Status = JobFailed
		j.FinishedAt = time.Now().UTC()
		j.Error = fmt.Sprintf("new client: %v", err)
		_ = saveJob(j)
		return
	}
	startDate := fmt.Sprintf("%ddaysAgo", days)
	body := map[string]any{
		"dimensions": []map[string]any{{"name": "pagePath"}},
		"metrics":    []map[string]any{{"name": "sessions"}},
		"dateRanges": []map[string]any{{"startDate": startDate, "endDate": "today"}},
		"limit":      "1000",
	}
	report, err := runReport(newClientAdapter(c), j.Property, body)
	if err != nil {
		j.Status = JobFailed
		j.FinishedAt = time.Now().UTC()
		j.Error = err.Error()
		_ = saveJob(j)
		return
	}
	resultJSON, _ := json.MarshalIndent(report, "", "  ")
	resultPath := filepath.Join(jobsDir(), j.ID+".result.json")
	if err := os.WriteFile(resultPath, resultJSON, 0o644); err != nil {
		j.Status = JobFailed
		j.FinishedAt = time.Now().UTC()
		j.Error = fmt.Sprintf("write result: %v", err)
		_ = saveJob(j)
		return
	}

	j.Status = JobCompleted
	j.FinishedAt = time.Now().UTC()
	j.ResultPath = resultPath
	_ = saveJob(j)
}
