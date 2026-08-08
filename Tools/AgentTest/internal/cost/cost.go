// Package cost implements domain.CostProvider by delegating to the existing
// log-analysis tool's stable per-run total contract. No log parsing exists
// in this module.
package cost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"mosaic-agent-test/internal/domain"
)

// UnknownRunBucket is the run folder the MOSAIC logger writes into when it
// cannot resolve a run's identity. Its presence alongside an empty result
// for the queried run is what distinguishes "this run cost nothing" from
// "this run's events could not be attributed".
const UnknownRunBucket = "unknown-run"

// The log-analysis tool's stable exit codes (mirrored here as plain ints:
// this package must not import mosaic-log-analyzer to learn its own outcome
// contract — see Tools/LogAnalyzer/internal/cli/exitcodes.go).
const (
	exitSuccess  = 0
	exitFailure  = 1
	exitNoData   = 2
	exitUsage    = 3
	exitUnusable = 4
)

// CommandRunner is the seam that makes the outcome mapping testable without
// the real log-analysis tool present.
type CommandRunner func(ctx context.Context, path string, args []string) (stdout []byte, exitCode int, err error)

// Options configures the delegating cost provider.
type Options struct {
	// ExecutablePath is the log-analysis tool's binary.
	ExecutablePath string
	Timeout        time.Duration

	// Invoke is the seam that makes the outcome mapping testable without the
	// real tool present. nil selects real process execution.
	Invoke CommandRunner

	// StatDir reports whether a directory exists. nil selects the real
	// filesystem. Used only to detect the fallback attribution bucket.
	StatDir func(path string) bool
}

// New constructs a domain.CostProvider that delegates to the log-analysis
// tool described by opts.
func New(opts Options) domain.CostProvider {
	return &provider{opts: opts}
}

type provider struct {
	opts Options
}

var _ domain.CostProvider = (*provider)(nil)

func (p *provider) Cost(ctx context.Context, q domain.CostQuery) (domain.CostReport, error) {
	invoke := p.opts.Invoke
	if invoke == nil {
		invoke = execCommandRunner
	}
	statDir := p.opts.StatDir
	if statDir == nil {
		statDir = defaultStatDir
	}

	runCtx := ctx
	if p.opts.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, p.opts.Timeout)
		defer cancel()
	}

	args := []string{"total", "--run", q.RunID, "--path", q.LogRoot, "--format", "json"}
	stdout, exitCode, invokeErr := invoke(runCtx, p.opts.ExecutablePath, args)
	if invokeErr != nil {
		return Map(exitCode, RunTotal{}, false, invokeErr), nil
	}

	var total RunTotal
	if parseErr := json.Unmarshal(stdout, &total); parseErr != nil {
		return Map(exitCode, RunTotal{}, false, parseErr), nil
	}

	unknownBucketPresent := false
	if exitCode == exitNoData {
		unknownBucketPresent = statDir(filepath.Join(q.LogRoot, UnknownRunBucket))
	}

	return Map(exitCode, total, unknownBucketPresent, nil), nil
}

// RunTotal mirrors the log-analysis tool's stable per-run total wire shape.
// It is a wire model and nothing else: no arithmetic and no aggregation is
// performed over it here.
type RunTotal struct {
	SchemaVersion string     `json:"schema_version"`
	RunID         string     `json:"run_id"`
	Provisional   bool       `json:"provisional"`
	Currency      string     `json:"currency"`
	Tokens        TokenUsage `json:"tokens"`
	Money         MoneyValue `json:"money"`
	Complete      bool       `json:"complete"`
}

// TokenUsage categories are pointers because absent and zero are different
// facts on this wire.
type TokenUsage struct {
	Input         *int64 `json:"input"`
	CacheRead     *int64 `json:"cache_read"`
	CacheCreation *int64 `json:"cache_creation"`
	Output        *int64 `json:"output"`
}

// MoneyValue.State is "known", "unpriced" or "no_data". Amount is present
// only for "known" and is an exact decimal string, never a JSON number.
type MoneyValue struct {
	State  string  `json:"state"`
	Amount *string `json:"amount,omitempty"`
}

// Map is the pure mapping from the delegated tool's outcome to a cost
// report.
func Map(exitCode int, total RunTotal, unknownBucketPresent bool, invokeErr error) domain.CostReport {
	if invokeErr != nil {
		return domain.CostReport{
			Attribution: domain.AttributionUnavailable,
			Detail:      fmt.Sprintf("cost: delegating to the log-analysis tool failed: %v", invokeErr),
		}
	}

	switch exitCode {
	case exitSuccess:
		return mapSuccess(total)
	case exitNoData:
		if unknownBucketPresent {
			return domain.CostReport{
				Attribution: domain.AttributionUnknownBucket,
				Detail:      "cost: no data for this run, but the fallback bucket is present — a cost exists but could not be attributed",
			}
		}
		return domain.CostReport{Attribution: domain.AttributionAttributed, TotalUSD: 0}
	case exitFailure:
		return domain.CostReport{
			Attribution: domain.AttributionUnavailable,
			Detail:      "cost: the log-analysis tool reported an infrastructure failure",
		}
	case exitUsage:
		return domain.CostReport{
			Attribution: domain.AttributionUnavailable,
			Detail:      "cost: the log-analysis tool reported a usage error",
		}
	case exitUnusable:
		return domain.CostReport{
			Attribution: domain.AttributionUnavailable,
			Detail:      "cost: the log-analysis tool reported an unusable log source",
		}
	default:
		return domain.CostReport{
			Attribution: domain.AttributionUnavailable,
			Detail:      fmt.Sprintf("cost: the log-analysis tool reported an unrecognised exit code %d", exitCode),
		}
	}
}

func mapSuccess(total RunTotal) domain.CostReport {
	switch total.Money.State {
	case "known":
		report := domain.CostReport{
			Attribution: domain.AttributionAttributed,
			TotalUSD:    parseAmount(total.Money.Amount),
		}
		if total.Provisional || !total.Complete {
			report.Detail = "cost: total is partial (provisional or incomplete)"
		}
		return report
	case "unpriced":
		return domain.CostReport{
			Attribution: domain.AttributionUnavailable,
			Detail:      "cost: total reports usage priced against unpriced models",
		}
	case "no_data":
		return domain.CostReport{Attribution: domain.AttributionAttributed, TotalUSD: 0}
	default:
		return domain.CostReport{
			Attribution: domain.AttributionUnavailable,
			Detail:      fmt.Sprintf("cost: unrecognised money state %q", total.Money.State),
		}
	}
}

func parseAmount(amount *string) float64 {
	if amount == nil {
		return 0
	}
	v, err := strconv.ParseFloat(*amount, 64)
	if err != nil {
		return 0
	}
	return v
}

// defaultStatDir is the real filesystem implementation of Options.StatDir.
func defaultStatDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// execCommandRunner is the real process-execution implementation of
// CommandRunner.
func execCommandRunner(ctx context.Context, path string, args []string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard

	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.Bytes(), exitErr.ExitCode(), nil
	}
	return nil, 0, err
}
