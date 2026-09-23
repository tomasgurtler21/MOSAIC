// Package testcheck implements the two-layer result verification for Runner
// test automation: (1) exit code comparison and (2) dispatch log sequence
// comparison.
//
// The package parses JSONL dispatch logs (matching the format written by
// internal/dispatchlog), loads expected-outcome sidecar JSON files, pairs
// request/response/error entries by agent_instance_id, and reports the first
// mismatch.
//
// Sidecar JSON files use the .expected.json extension and carry the
// ExpectedOutcome schema defined here. Authoring of sidecar files is done by
// Stage 5; this package defines the schema those files must satisfy.
package testcheck

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// MismatchKind categorizes a mismatch between expected and actual outcomes.
type MismatchKind int

const (
	MismatchExitCode       MismatchKind = iota // actual exit code differs from expected
	MismatchAgent                              // dispatch at index has wrong agent
	MismatchStatus                             // dispatch at index has wrong status
	MismatchExtraDispatch                      // actual log has more dispatches than expected
	MismatchMissingDispatch                    // actual log has fewer dispatches than expected
	MismatchTaskContains                       // task text does not contain expected substring
	MismatchErrorExpected                      // expected an error entry, got a normal response
	MismatchErrorUnexpected                    // expected a normal response, got an error entry
	MismatchInfraError                         // infrastructure error inside Check (e.g. dispatch log unreadable)
)

// Mismatch describes the first point of divergence between expected and actual
// outcomes.
type Mismatch struct {
	// Kind is the category of mismatch.
	Kind MismatchKind

	// Index is the zero-based position in the dispatch sequence where the
	// mismatch occurred. -1 for exit-code mismatches.
	Index int

	// Message is a human-readable description of the divergence.
	Message string

	// ExpectedValue is the expected value (exit code or agent/status string).
	ExpectedValue string

	// ActualValue is the actual value observed.
	ActualValue string
}

// CheckResult is the outcome of checking one test run.
type CheckResult struct {
	// Pass is true when both exit code and dispatch sequence matched.
	Pass bool

	// Mismatch is non-nil when Pass is false and describes the first divergence.
	Mismatch *Mismatch
}

// CheckInput bundles the actual run outcomes for checking.
type CheckInput struct {
	// ExitCode is the process exit code from the mosaic-run subprocess.
	ExitCode int

	// DispatchLog is the path to the JSONL dispatch log file for this run.
	DispatchLog string
}

// ExpectedOutcome is the Go representation of a .expected.json sidecar file.
type ExpectedOutcome struct {
	// ExitCode is the expected process exit code.
	ExitCode int `json:"exit_code"`

	// Dispatches is the expected dispatch sequence.
	Dispatches []ExpectedDispatch `json:"dispatches"`
}

// ExpectedDispatch is one expected entry in the dispatch sequence.
type ExpectedDispatch struct {
	// Agent is the expected agent_instance_id. Matched as a prefix:
	// expected "mosaictest-scripted" matches actual "mosaictest-scripted#1".
	Agent string `json:"agent"`

	// ExpectStatus is the expected status_code from the response
	// (e.g. "SUCCESS", "COMPLETED_NEEDS_ACTION").
	// Ignored when ExpectError is true.
	ExpectStatus string `json:"expect_status,omitempty"`

	// ExpectError is true when this dispatch is expected to produce a
	// dispatch-log error entry (harness invocation failure) rather than a
	// normal response. When true, ExpectStatus is ignored.
	ExpectError bool `json:"expect_error,omitempty"`

	// TaskContains, when non-empty, requires the request's task_description
	// to contain this substring. When empty, task text is not checked.
	TaskContains string `json:"task_contains,omitempty"`
}

// ObservedDispatch is one entry in the observed dispatch sequence, produced by
// pairing a request entry with its corresponding response or error entry.
type ObservedDispatch struct {
	// Agent is the agent_instance_id from the request entry.
	Agent string

	// Status is the status_code from the response entry. Empty when IsError
	// is true.
	Status string

	// IsError is true when the dispatch produced a dispatch-log error entry
	// instead of a normal response.
	IsError bool

	// TaskDescription is the task_description text from the request entry.
	TaskDescription string
}

// logEntry is used for initial type discrimination of JSONL lines.
type logEntry struct {
	Type string `json:"type"`
}

// logRequest is a dispatch-log request entry.
type logRequest struct {
	Type    string `json:"type"`
	Request struct {
		AgentInstanceID string `json:"agent_instance_id"`
		TaskDescription string `json:"task_description"`
	} `json:"request"`
}

// logResponse is a dispatch-log response entry.
type logResponse struct {
	Type            string `json:"type"`
	AgentInstanceID string `json:"agent_instance_id"`
	Response        struct {
		StatusCode string `json:"status_code"`
	} `json:"response"`
}

// logError is a dispatch-log error entry (harness-level failure).
type logError struct {
	Type            string `json:"type"`
	AgentInstanceID string `json:"agent_instance_id"`
	Error           string `json:"error"`
}

// pendingRequest holds an unmatched request entry waiting to be paired with a
// response or error.
type pendingRequest struct {
	agentInstanceID string
	taskDescription string
}

// LoadExpected reads and deserializes an expected-outcome sidecar JSON file at
// the given path. Returns an error if the file does not exist or contains
// invalid JSON.
func LoadExpected(path string) (*ExpectedOutcome, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("testcheck: LoadExpected: read %q: %w", path, err)
	}
	var outcome ExpectedOutcome
	if err := json.Unmarshal(data, &outcome); err != nil {
		return nil, fmt.Errorf("testcheck: LoadExpected: parse %q: %w", path, err)
	}
	return &outcome, nil
}

// ParseDispatchLog reads a JSONL dispatch log file at the given path and
// returns the observed dispatch sequence. Entries of type "version" and
// "correlation" are skipped. Request entries are paired with subsequent
// response or error entries by agent_instance_id, in file order. Each
// response or error is paired with the most recent unmatched request sharing
// the same agent_instance_id. Orphan response or error entries (whose
// agent_instance_id has no pending request) are silently skipped for forward
// compatibility. Unmatched requests remaining at the end of the log are
// treated as infrastructure errors (the run was interrupted) and are emitted
// as ObservedDispatch entries with IsError=true. Returns an error if the file
// cannot be read or contains malformed JSON.
func ParseDispatchLog(path string) ([]ObservedDispatch, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("testcheck: ParseDispatchLog: open %q: %w", path, err)
	}
	defer f.Close()

	// pending maps agent_instance_id -> stack of unmatched requests (LIFO
	// to match "most recent unmatched" semantics).
	pending := make(map[string][]pendingRequest)
	// dispatches accumulates completed pairs in the order they are completed.
	var dispatches []ObservedDispatch

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Discriminate by type first.
		var entry logEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("testcheck: ParseDispatchLog: line %d: %w", lineNum, err)
		}

		switch entry.Type {
		case "version", "correlation":
			// Skip metadata entries.
			continue

		case "request":
			var req logRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				return nil, fmt.Errorf("testcheck: ParseDispatchLog: line %d (request): %w", lineNum, err)
			}
			id := req.Request.AgentInstanceID
			pending[id] = append(pending[id], pendingRequest{
				agentInstanceID: id,
				taskDescription: req.Request.TaskDescription,
			})

		case "response":
			var resp logResponse
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				return nil, fmt.Errorf("testcheck: ParseDispatchLog: line %d (response): %w", lineNum, err)
			}
			id := resp.AgentInstanceID
			stack := pending[id]
			if len(stack) == 0 {
				// Orphan response -- skip.
				continue
			}
			req := stack[len(stack)-1]
			pending[id] = stack[:len(stack)-1]
			dispatches = append(dispatches, ObservedDispatch{
				Agent:           req.agentInstanceID,
				Status:          resp.Response.StatusCode,
				IsError:         false,
				TaskDescription: req.taskDescription,
			})

		case "error":
			var errEntry logError
			if err := json.Unmarshal([]byte(line), &errEntry); err != nil {
				return nil, fmt.Errorf("testcheck: ParseDispatchLog: line %d (error): %w", lineNum, err)
			}
			id := errEntry.AgentInstanceID
			stack := pending[id]
			if len(stack) == 0 {
				// Orphan error -- skip.
				continue
			}
			req := stack[len(stack)-1]
			pending[id] = stack[:len(stack)-1]
			dispatches = append(dispatches, ObservedDispatch{
				Agent:           req.agentInstanceID,
				Status:          "",
				IsError:         true,
				TaskDescription: req.taskDescription,
			})

		default:
			// Unknown entry types are silently skipped for forward compatibility.
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("testcheck: ParseDispatchLog: scan %q: %w", path, err)
	}

	// Unmatched requests at the end of the log represent a run that was
	// interrupted before responses were written. Emit each as an error
	// dispatch so the checker sees the correct sequence length and agent
	// identity. Agent IDs are sorted for deterministic output order.
	if len(pending) > 0 {
		agentIDs := make([]string, 0, len(pending))
		for id := range pending {
			agentIDs = append(agentIDs, id)
		}
		sort.Strings(agentIDs)
		for _, id := range agentIDs {
			stack := pending[id]
			// Emit in LIFO order (most recent request first).
			for i := len(stack) - 1; i >= 0; i-- {
				dispatches = append(dispatches, ObservedDispatch{
					Agent:           stack[i].agentInstanceID,
					IsError:         true,
					TaskDescription: stack[i].taskDescription,
				})
			}
		}
	}

	return dispatches, nil
}

// Check performs the two-layer verification of a single test run:
//  1. Exit code comparison (actual vs expected).
//  2. Dispatch sequence comparison (observed vs expected), if exit codes match.
//
// Returns CheckResult with Pass=true if both layers match. On mismatch,
// returns the first divergence. Dispatch comparison is skipped when exit codes
// differ.
func Check(actual CheckInput, expected *ExpectedOutcome) CheckResult {
	// Layer 1: exit code comparison.
	if actual.ExitCode != expected.ExitCode {
		return CheckResult{
			Pass: false,
			Mismatch: &Mismatch{
				Kind:          MismatchExitCode,
				Index:         -1,
				Message:       fmt.Sprintf("exit code mismatch: expected %d, got %d", expected.ExitCode, actual.ExitCode),
				ExpectedValue: fmt.Sprintf("%d", expected.ExitCode),
				ActualValue:   fmt.Sprintf("%d", actual.ExitCode),
			},
		}
	}

	// Layer 2: dispatch sequence comparison.
	observed, err := ParseDispatchLog(actual.DispatchLog)
	if err != nil {
		return CheckResult{
			Pass: false,
			Mismatch: &Mismatch{
				Kind:    MismatchInfraError,
				Index:   -1,
				Message: fmt.Sprintf("failed to parse dispatch log: %v", err),
			},
		}
	}

	exp := expected.Dispatches
	nObs := len(observed)
	nExp := len(exp)
	limit := nObs
	if nExp < limit {
		limit = nExp
	}

	for i := 0; i < limit; i++ {
		obs := observed[i]
		ex := exp[i]

		// Agent prefix match.
		if !strings.HasPrefix(obs.Agent, ex.Agent) {
			return CheckResult{
				Pass: false,
				Mismatch: &Mismatch{
					Kind:          MismatchAgent,
					Index:         i,
					Message:       fmt.Sprintf("dispatch[%d]: agent mismatch: expected prefix %q, got %q", i, ex.Agent, obs.Agent),
					ExpectedValue: ex.Agent,
					ActualValue:   obs.Agent,
				},
			}
		}

		// Error vs response check.
		if ex.ExpectError {
			if !obs.IsError {
				return CheckResult{
					Pass: false,
					Mismatch: &Mismatch{
						Kind:          MismatchErrorExpected,
						Index:         i,
						Message:       fmt.Sprintf("dispatch[%d]: expected error entry for agent %q, got normal response with status %q", i, obs.Agent, obs.Status),
						ExpectedValue: "error",
						ActualValue:   obs.Status,
					},
				}
			}
		} else {
			if obs.IsError {
				return CheckResult{
					Pass: false,
					Mismatch: &Mismatch{
						Kind:          MismatchErrorUnexpected,
						Index:         i,
						Message:       fmt.Sprintf("dispatch[%d]: expected response with status %q for agent %q, got error entry", i, ex.ExpectStatus, obs.Agent),
						ExpectedValue: ex.ExpectStatus,
						ActualValue:   "error",
					},
				}
			}
			// Status check (only when not expecting error).
			if obs.Status != ex.ExpectStatus {
				return CheckResult{
					Pass: false,
					Mismatch: &Mismatch{
						Kind:          MismatchStatus,
						Index:         i,
						Message:       fmt.Sprintf("dispatch[%d]: status mismatch for agent %q: expected %q, got %q", i, obs.Agent, ex.ExpectStatus, obs.Status),
						ExpectedValue: ex.ExpectStatus,
						ActualValue:   obs.Status,
					},
				}
			}
		}

		// TaskContains check.
		if ex.TaskContains != "" && !strings.Contains(obs.TaskDescription, ex.TaskContains) {
			return CheckResult{
				Pass: false,
				Mismatch: &Mismatch{
					Kind:          MismatchTaskContains,
					Index:         i,
					Message:       fmt.Sprintf("dispatch[%d]: task description for agent %q does not contain %q; got %q", i, obs.Agent, ex.TaskContains, obs.TaskDescription),
					ExpectedValue: ex.TaskContains,
					ActualValue:   obs.TaskDescription,
				},
			}
		}
	}

	// Length mismatch: missing dispatches.
	if nExp > nObs {
		return CheckResult{
			Pass: false,
			Mismatch: &Mismatch{
				Kind:          MismatchMissingDispatch,
				Index:         nObs,
				Message:       fmt.Sprintf("dispatch[%d]: expected dispatch for agent %q but log ended", nObs, exp[nObs].Agent),
				ExpectedValue: exp[nObs].Agent,
				ActualValue:   "",
			},
		}
	}

	// Length mismatch: extra dispatches.
	if nObs > nExp {
		return CheckResult{
			Pass: false,
			Mismatch: &Mismatch{
				Kind:          MismatchExtraDispatch,
				Index:         nExp,
				Message:       fmt.Sprintf("dispatch[%d]: unexpected extra dispatch for agent %q", nExp, observed[nExp].Agent),
				ExpectedValue: "",
				ActualValue:   observed[nExp].Agent,
			},
		}
	}

	return CheckResult{Pass: true}
}
