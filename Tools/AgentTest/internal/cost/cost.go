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
	"os/exec"
	"strconv"
	"strings"
	"time"

	"mosaic-agent-test/internal/domain"
)

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
//
// workingDir is the directory the command runs in. An empty workingDir means
// "inherit the parent process's working directory", which is exactly the
// behaviour every caller had before this parameter existed.
type CommandRunner func(ctx context.Context, path string, args []string, workingDir string) (stdout []byte, exitCode int, err error)

// Options configures the delegating cost provider.
type Options struct {
	// ExecutablePath is the log-analysis tool's binary.
	ExecutablePath string
	Timeout        time.Duration

	// WorkingDir is the directory the log-analysis subprocess is executed in.
	// The log analyzer resolves MosaicLogAnalyzer/config/pricing.yaml relative
	// to its own working directory and exposes no root-override flag, so this
	// is the only way to make it find a priced model. The composition root
	// supplies its already-resolved MOSAIC root here.
	//
	// Empty leaves the subprocess inheriting this process's working directory —
	// the previous behaviour, kept so an unwired context degrades rather than
	// executing against an empty directory.
	WorkingDir string

	// Invoke is the seam that makes the outcome mapping testable without the
	// real tool present. nil selects real process execution.
	Invoke CommandRunner
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

	runCtx := ctx
	if p.opts.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, p.opts.Timeout)
		defer cancel()
	}

	// When LogsRoot is available, pass it as --path so the analyser scans the
	// entire OrchestrationLogs tree. LogAnalyzer's TotalForRun handles the
	// unknown-run merge internally.
	scanPath := q.LogRoot
	if q.LogsRoot != "" {
		scanPath = q.LogsRoot
	}

	args := []string{"total", "--run", q.RunID, "--path", scanPath, "--format", "json"}
	stdout, exitCode, invokeErr := invoke(runCtx, p.opts.ExecutablePath, args, p.opts.WorkingDir)
	if invokeErr != nil {
		return Map(MapInput{
			ExecutablePath: p.opts.ExecutablePath,
			RunID:          q.RunID,
			LogRoot:        q.LogRoot,
			LogsRoot:       q.LogsRoot,
			ExitCode:       exitCode,
			InvokeErr:      invokeErr,
		}), nil
	}

	var total RunTotal
	if parseErr := json.Unmarshal(stdout, &total); parseErr != nil {
		return Map(MapInput{
			ExecutablePath: p.opts.ExecutablePath,
			RunID:          q.RunID,
			LogRoot:        q.LogRoot,
			LogsRoot:       q.LogsRoot,
			ExitCode:       exitCode,
			StdoutLen:      len(stdout),
			ParseErr:       parseErr,
		}), nil
	}

	return Map(MapInput{
		ExecutablePath: p.opts.ExecutablePath,
		RunID:          q.RunID,
		LogRoot:        q.LogRoot,
		LogsRoot:       q.LogsRoot,
		ExitCode:       exitCode,
		Total:          total,
	}), nil
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

	// UnpricedModels lists model names that could not be priced. Absent from
	// older analyser builds; decodes to nil, which the mapping treats as "no
	// information about which models were unpriced".
	UnpricedModels []string `json:"unpriced_models,omitempty"`

	// PartialMoney is the attributable amount when some models are priced and
	// others are not. Absent from older builds; decodes to nil.
	PartialMoney *MoneyValue `json:"partial_money,omitempty"`

	// UnknownRunMerged is the count of usage records merged from the
	// unknown-run bucket by LogAnalyzer. Zero or absent means no merge
	// occurred.
	UnknownRunMerged int `json:"unknown_run_merged,omitempty"`

	// UnknownRunResidual is the count of unknown-run records that remained
	// unattributed after merging. Zero or absent means all records were
	// attributed. Non-zero triggers a report-level error in AgentTest.
	UnknownRunResidual int `json:"unknown_run_residual,omitempty"`
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

// MapInput is everything known at the call site when the delegate's outcome
// is mapped to a report. Every field the diagnostic names is here by
// construction, which is what stops the exit code and the queried path from
// being dropped. See ContractsDesign.md's "cost mapping surface".
type MapInput struct {
	// ExecutablePath is the delegate that was invoked.
	ExecutablePath string
	// RunID, LogRoot and LogsRoot are what was queried.
	RunID    string
	LogRoot  string
	LogsRoot string // the OrchestrationLogs parent; passed as --path to LogAnalyzer so it can enumerate the full OrchestrationLogs tree

	// ExitCode is the delegate's exit code; meaningful only when InvokeErr is nil.
	ExitCode int
	// StdoutLen is how many bytes the delegate produced, so "exited non-zero
	// and said nothing" is distinguishable from "said something unparseable"
	// without carrying the payload itself into the mapping.
	StdoutLen int

	Total RunTotal

	// InvokeErr is non-nil only when the delegate could not be invoked or run
	// to completion at all — never for a delegate that ran and exited non-zero.
	InvokeErr error
	// ParseErr is non-nil when the delegate's stdout could not be decoded.
	ParseErr error
}

// Map is the pure mapping from the delegated tool's outcome to a cost
// report. See ContractsDesign.md's "cost mapping surface" for the full
// mapping contract.
func Map(in MapInput) domain.CostReport {
	if in.InvokeErr != nil {
		return domain.CostReport{
			Attribution: domain.AttributionUnavailable,
			Detail:      fmt.Sprintf("cost: could not be invoked: the log-analysis tool %q failed to run: %v", in.ExecutablePath, in.InvokeErr),
		}
	}

	if in.ParseErr != nil {
		if in.ExitCode == exitSuccess {
			return domain.CostReport{
				Attribution: domain.AttributionUnavailable,
				Detail: fmt.Sprintf(
					"cost: the log-analysis tool %q ran successfully but produced output that could not be decoded (%d bytes of stdout)",
					in.ExecutablePath, in.StdoutLen,
				),
			}
		}
		return domain.CostReport{
			Attribution: domain.AttributionUnavailable,
			Detail: fmt.Sprintf(
				"cost: the log-analysis tool %q exited with code %d and produced output that could not be decoded, querying run %q under log root %q",
				in.ExecutablePath, in.ExitCode, in.RunID, in.LogRoot,
			),
		}
	}

	switch in.ExitCode {
	case exitSuccess:
		return mapSuccess(in.Total)
	case exitNoData:
		return domain.CostReport{Attribution: domain.AttributionAttributed, TotalUSD: 0}
	case exitFailure:
		return domain.CostReport{
			Attribution: domain.AttributionUnavailable,
			Detail:      "cost: the log-analysis tool reported an infrastructure failure",
		}
	case exitUsage:
		return domain.CostReport{
			Attribution: domain.AttributionUnavailable,
			Detail:      fmt.Sprintf("cost: no usage data found -- queried %q which does not exist", in.LogRoot),
		}
	case exitUnusable:
		return domain.CostReport{
			Attribution: domain.AttributionUnavailable,
			Detail:      "cost: the log-analysis tool reported an unusable log source",
		}
	default:
		return domain.CostReport{
			Attribution: domain.AttributionUnavailable,
			Detail:      fmt.Sprintf("cost: the log-analysis tool reported an unrecognised exit code %d", in.ExitCode),
		}
	}
}

func mapSuccess(total RunTotal) domain.CostReport {
	var report domain.CostReport
	switch total.Money.State {
	case "known":
		report = domain.CostReport{
			Attribution: domain.AttributionAttributed,
			TotalUSD:    parseAmount(total.Money.Amount),
		}
		if total.Provisional || !total.Complete {
			report.Detail = "cost: total is partial (provisional or incomplete)"
		}
	case "unpriced":
		// When the analyser names the unpriced models and provides a partial
		// amount, report the attributable cost as partial rather than zeroing
		// the run. One unpriced model among several must not silence the rest.
		if len(total.UnpricedModels) > 0 && total.PartialMoney != nil {
			detail := fmt.Sprintf(
				"cost: usage captured but the following model(s) had no price entry: %s",
				strings.Join(total.UnpricedModels, ", "),
			)
			report = domain.CostReport{
				Attribution:    domain.AttributionPartial,
				TotalUSD:       parseAmount(total.PartialMoney.Amount),
				Detail:         detail,
				UnpricedModels: total.UnpricedModels,
			}
		} else {
			// Without model identity we cannot name what we do not know, but we
			// must acknowledge that usage was captured — not redirect the user to
			// pricing configuration.
			report = domain.CostReport{
				Attribution: domain.AttributionUnavailable,
				Detail:      "cost: usage was captured but the model(s) responsible could not be identified for attribution",
			}
		}
	case "no_data":
		report = domain.CostReport{Attribution: domain.AttributionAttributed, TotalUSD: 0}
	default:
		report = domain.CostReport{
			Attribution: domain.AttributionUnavailable,
			Detail:      fmt.Sprintf("cost: unrecognised money state %q", total.Money.State),
		}
	}

	// Carry merge residual signal into the cost report for downstream
	// report-level error emission.
	report.UnknownRunResidual = total.UnknownRunResidual
	return report
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

// execCommandRunner is the real process-execution implementation of
// CommandRunner. workingDir sets exec.Cmd.Dir; an empty string leaves the
// subprocess inheriting this process's working directory.
func execCommandRunner(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = workingDir
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
