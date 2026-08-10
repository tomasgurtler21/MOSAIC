package domain

import (
	"encoding/json"
	"time"
)

// TestSuite is the top-level authored document (`*.suite.yaml`) listing the
// test definitions to run, with defaults every entry inherits unless it
// overrides them.
type TestSuite struct {
	SchemaVersion int
	ID            string
	Description   string
	Defaults      RunSettings
	Entries       []SuiteEntry
	SourcePath    string
}

// SuiteEntry names one test definition a suite runs, plus any per-entry
// override of the suite's defaults.
type SuiteEntry struct {
	Path     string // path to a test definition, relative to the suite file
	Settings RunSettings
}

// RunSettings are the knobs a suite, a definition and CLI flags all express,
// resolved in that precedence order. Pointer fields distinguish "not stated"
// from "stated as the zero value".
type RunSettings struct {
	Timeout              *time.Duration
	TurnLimit            *int
	Repetitions          *int
	PassRate             *float64
	StopAfterInvocations *int
}

// TestDefinition is the authored document (`*.test.yaml`) describing one
// test: the subject to exercise, the stub registry to intercept against, and
// the assertions its run must satisfy.
type TestDefinition struct {
	SchemaVersion int
	ID            string
	Description   string
	Layer         TestLayer
	// Negative inverts assertion outcomes. It never inverts echo fidelity or
	// state integrity: a test cannot "expect" the infrastructure to be
	// broken.
	Negative bool

	Subject          SubjectUnderTest
	StubRegistryPath string
	Settings         RunSettings
	SeedFiles        []SeedFile
	ParallelGroups   []ParallelGroup
	Assertions       Assertions
	SourcePath       string
}

// SeedFile pre-populates the subject directory, for a test that starts
// mid-workflow rather than from nothing.
type SeedFile struct {
	Path    string // relative to the subject dir
	Content string // mutually exclusive with Ref
	Ref     string // $ref into the fixture root
}

// ParallelGroup declares a set of collaborators expected to be in flight
// together, so peak-concurrency assertions have something to name.
type ParallelGroup struct {
	Name    string
	Members []CollaboratorIdentity
}

// StubRegistry is the authored document (`*.stubs.json`) declaring how each
// intercepted collaborator call is answered.
type StubRegistry struct {
	SchemaVersion int
	TestID        string
	OnUnmatched   UnmatchedPolicy
	// GenericResponse is required when OnUnmatched is UnmatchedGenericResponse.
	GenericResponse json.RawMessage
	Stubs           []Stub
	SourcePath      string
}

// UnmatchedPolicy declares what happens when an intercepted call matches no
// stub entry.
type UnmatchedPolicy string

const (
	// Halt the call. The default: an unexpected invocation is evidence.
	UnmatchedHalt UnmatchedPolicy = "halt"
	// Return the registry's generic response.
	UnmatchedGenericResponse UnmatchedPolicy = "generic-response"
	// Let the call proceed untouched.
	UnmatchedPassthrough UnmatchedPolicy = "passthrough"
)

// Stub is one registry entry: the call it answers, the payload it returns,
// and any file effects that must exist before the response is returned.
type Stub struct {
	Match StubMatch
	// Response is the payload the collaborator is made to produce, carried
	// verbatim so a faithful echo compares equal.
	Response    json.RawMessage
	SideEffects []FileEffect
}

// StubMatch keys a Stub on the composite collaborator identity plus the
// per-identity invocation ordinal (1-based). The global sequence number is
// never used for matching.
type StubMatch struct {
	Identity CollaboratorIdentity
	// Invocation is the 1-based per-identity ordinal this entry answers.
	Invocation int
}

// FileEffect is a file a stub declares should exist before its response is
// returned. Applied by the interceptor, recorded in the ledger, removed at
// teardown.
type FileEffect struct {
	Path    string // relative to the subject dir; must not escape it
	Content string // mutually exclusive with Ref
	Ref     string // $ref into the fixture root
}

// EffectLedger records exactly what a SideEffectApplier created, so teardown
// removes exactly that and nothing else.
type EffectLedger struct {
	Files []string // relative to the subject dir, in creation order
	Dirs  []string // directories created, in creation order
}

// Merge appends other's recorded files and directories onto l, so a ledger
// can accumulate across multiple Apply calls.
func (l *EffectLedger) Merge(other EffectLedger) {
	l.Files = append(l.Files, other.Files...)
	l.Dirs = append(l.Dirs, other.Dirs...)
}
