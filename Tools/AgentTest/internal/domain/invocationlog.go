package domain

import (
	"encoding/json"
	"time"
)

// RecordKind is the kind of one invocation-log record.
type RecordKind string

const (
	RecordStart RecordKind = "start"
	RecordEnd   RecordKind = "end"
	RecordError RecordKind = "error"
	RecordRun   RecordKind = "run" // run-level, not tied to one invocation
)

// LogRecord is one line of the JSONL invocation log.
//
// Timestamps carry nanosecond precision: peak-concurrency reconstruction
// distinguishes overlap from abutment, and coarse timestamps would collapse
// real concurrency to one.
type LogRecord struct {
	Kind      RecordKind `json:"kind"`
	TestID    string     `json:"test_id"`
	RunNumber int        `json:"run_number"`
	Timestamp time.Time  `json:"ts"`

	Seq              int                  `json:"seq,omitempty"`
	Ordinal          int                  `json:"ordinal,omitempty"`
	Identity         CollaboratorIdentity `json:"identity,omitempty"`
	Group            string               `json:"group,omitempty"`
	CorrelationToken string               `json:"token,omitempty"`

	// Start records
	Outcome OutcomeKind  `json:"outcome,omitempty"`
	Message *TaskMessage `json:"message,omitempty"`

	// End records
	Echo *EchoOutcome `json:"echo,omitempty"`

	// Run records
	Event  RunEventKind `json:"event,omitempty"`
	Detail string       `json:"detail,omitempty"`
}

// RunEventKind names a run-level event recorded on a RecordRun record.
type RunEventKind string

const (
	// A lock was reclaimed from a dead or stale holder: a state update may
	// have been lost, so every verdict computed afterwards is suspect.
	RunEventLockReclaimed       RunEventKind = "lock_reclaimed"
	RunEventEarlyExitTriggered  RunEventKind = "early_exit_triggered"
	RunEventExtractionRecovered RunEventKind = "extraction_recovered"
	RunEventExtractionFailed    RunEventKind = "extraction_failed"
	RunEventUnmatchedInvocation RunEventKind = "unmatched_invocation"
	RunEventSubjectSpawned      RunEventKind = "subject_spawned"
	RunEventSubjectExited       RunEventKind = "subject_exited"
)

// EchoOutcome is the result of comparing a stub's expected response against
// what was actually observed post-invocation.
type EchoOutcome struct {
	Match    bool            `json:"match"`
	Expected json.RawMessage `json:"expected,omitempty"`
	Observed string          `json:"observed,omitempty"`
	Diff     string          `json:"diff,omitempty"`
}
