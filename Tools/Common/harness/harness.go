// Package harness provides a module-neutral "spawn an agent through a
// harness CLI and get a structured response back" capability, shared by
// mosaic-run and mosaic-agent-test.
//
// This package owns its own vocabulary because mosaic-common cannot import
// any tool module's internal/domain: an agent reference, a spawn request, a
// structured response, and a diagnostic logging seam. It never speaks any
// one tool's protocol or workflow vocabulary.
package harness

import (
	"context"
	"errors"
	"time"
)

// InvocationKind selects the CLI invocation strategy.
type InvocationKind string

const (
	InvocationOrdinary     InvocationKind = "ordinary"
	InvocationOrchestrator InvocationKind = "orchestrator"
)

// AgentRef is a resolved agent: an identifier plus the definition file path.
type AgentRef struct {
	Identifier     string
	DefinitionPath string
	Kind           InvocationKind
}

// SpawnRequest is everything needed to run one agent turn through a CLI.
type SpawnRequest struct {
	Agent        AgentRef
	Prompt       string
	Model        string
	MaxTurns     int // 0 means unlimited; the flag is omitted
	AllowedTools []string
	OutputFormat string // "json" | "stream-json"
	WorkingDir   string
	Env          []string // appended to the inherited environment
	ExtraArgs    []string
	Timeout      time.Duration

	// SystemPrompt is additional system-prompt content, already resolved to
	// text by the caller. It exists because a harness may offer no CLI flag
	// for appending to the system prompt: an argument builder that needed
	// such content would otherwise have to read a file, and argument
	// builders in this package are pure. Resolving it — reading a file,
	// synthesizing an <env> block — belongs to the caller.
	//
	// The Claude Code builder ignores this field: it has
	// --append-system-prompt-file and needs no substitute.
	SystemPrompt string
}

// Response is the structured result of a spawn.
//
// Protocol carries the Communication Protocol message as raw JSON bytes.
// This package deliberately does not decode it: each consumer unmarshals
// into its own domain type, which is what keeps this package free of any
// one tool's protocol vocabulary.
type Response struct {
	ExitCode int
	Stdout   []byte
	Stderr   string
	Text     string // the assistant text extracted from the output envelope
	Protocol []byte // the extracted protocol JSON object, or nil
	Duration time.Duration
}

// Spawner runs one agent turn.
//
// On success returns a Response with Protocol populated.
// On failure returns an error wrapping one of the sentinels below; timeout
// and parent-context cancellation remain distinguishable to the caller.
type Spawner interface {
	Spawn(ctx context.Context, req SpawnRequest) (Response, error)
}

// TextSpawner runs one agent turn and returns the assistant text from the
// CLI's output envelope, without requiring — or attempting to locate — a
// Communication Protocol message in it.
//
// It is the transport for payloads that are not protocol messages: the
// Runner-to-orchestrator consultation contract has its own request and
// response schemas, and a routing instruction is never a protocol envelope.
//
// SpawnText performs exactly the same steps as Spawn — ResolveExecutable,
// the per-harness argument builder, Run, the per-harness envelope parser —
// and stops before ExtractProtocolJSON.
//
// On success the returned Response has Text populated and Protocol nil.
// Text is never empty on a nil error: an empty or whitespace-only CLI output
// is reported by the envelope parser as ErrEmptyResponse, exactly as on the
// protocol path.
//
// On failure it returns an error wrapping one of the package sentinels, with
// the same classification the protocol path produces for the same condition:
// ErrExecutableNotFound, ErrNonZeroExit, ErrTimeout, ErrCancelled,
// ErrEmptyResponse, ErrMalformedJSON. ErrProtocolNotExtractable is never
// returned by this path.
//
// Timeout, context cancellation and subprocess termination behave identically
// to Spawn, because both reach the same Run.
type TextSpawner interface {
	SpawnText(ctx context.Context, req SpawnRequest) (Response, error)
}

// Sink is the diagnostic seam. Consumers adapt their own event vocabulary to
// it. Contract: Log never returns an error, never panics, never blocks.
type Sink interface {
	Log(ev Event)
}

// Event is one diagnostic record handed to a Sink.
type Event struct {
	Name    string // short dotted category, e.g. "spawn.stdout"
	Message string // may be multi-line and arbitrarily large
	Fields  []Field
}

// Field is one key/value pair attached to an Event.
type Field struct{ Key, Value string }

// Sentinel errors for the spawn/parse taxonomy. Tests and consumers use
// errors.Is to distinguish them.
var (
	// ErrExecutableNotFound is returned when the configured executable cannot
	// be found or executed.
	ErrExecutableNotFound = errors.New("harness: executable not found")

	// ErrNonZeroExit is returned when the CLI process exits with a non-zero
	// status. The wrapped error includes the exit code and captured stderr.
	ErrNonZeroExit = errors.New("harness: non-zero exit")

	// ErrTimeout is returned when the invocation exceeds the configured
	// timeout. The subprocess is killed before this error is returned.
	ErrTimeout = errors.New("harness: invocation timed out")

	// ErrCancelled is returned when the parent context is cancelled. The
	// subprocess is killed before this error is returned. Distinguishable
	// from ErrTimeout so a caller can tell which condition ended the run.
	ErrCancelled = errors.New("harness: invocation cancelled")

	// ErrEmptyResponse is returned when the CLI produces no stdout output.
	ErrEmptyResponse = errors.New("harness: empty response")

	// ErrMalformedJSON is returned when the CLI output is not valid JSON at
	// all.
	ErrMalformedJSON = errors.New("harness: output is not valid JSON")

	// ErrProtocolNotExtractable is returned when the CLI output is valid JSON
	// but the Communication Protocol response cannot be located or parsed
	// within it.
	ErrProtocolNotExtractable = errors.New("harness: protocol response not extractable from output")
)

// Command is a resolved executable plus any wrapper arguments the platform
// requires ahead of the caller's own arguments.
type Command struct {
	Path       string
	PrefixArgs []string
}

// RunOptions carries everything the plan-execution entry point needs beyond
// the resolved command and arguments.
type RunOptions struct {
	WorkingDir string
	Env        []string
	Stdin      []byte
	Timeout    time.Duration
	Sink       Sink
}

// Option configures a Spawner constructed by NewClaudeCode.
type Option func(*spawnerConfig)

// spawnerConfig holds the configuration accumulated from Options.
type spawnerConfig struct {
	executablePath string
	timeout        time.Duration
	sink           Sink
}
