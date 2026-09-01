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

	// SessionPersistence, when true, omits --no-session-persistence from
	// the constructed argument list, allowing the Claude Code CLI to write
	// its transcript file. The default (false) preserves today's behaviour
	// exactly: --no-session-persistence is emitted for both invocation kinds.
	SessionPersistence bool

	// DerivedTools is the tool allowlist derived from the invoked agent's
	// deployed tools frontmatter. BuildArgs reads this to emit --allowedTools
	// for Claude Code's dontAsk permission mode. BuildGHCPCLIArgs reads this
	// to emit --allow-tool entries for Partial Allowlist mode.
	//
	// This field is intentionally separate from AllowedTools. AllowedTools is
	// populated by Tools/AgentTest from a test-authored allowed_tools value (a
	// different source, different purpose); DerivedTools is populated by
	// Tools/Runner from the agent's own deployed frontmatter. No builder reads
	// both fields. This separation guarantees the non-collision constraint:
	// Runner's new deterministic behavior cannot alter AgentTest's invocations.
	//
	// When DerivedTools is nil or empty, BuildArgs falls back to
	// --permission-mode auto (backward-compatible with callers that do not
	// populate this field, notably AgentTest's SpawnPlan methods).
	DerivedTools []string

	// GHCPCLIMode selects the permission strategy for the GHCP CLI arg
	// builder. The zero value (GHCPCLIModeUnresolved) causes
	// BuildGHCPCLIArgs to return ErrGHCPCLIModeUnresolved. Set to
	// GHCPCLIModeBlanket for --yolo behavior or
	// GHCPCLIModePartialAllowlist for per-tool --allow-tool entries derived
	// from DerivedTools.
	GHCPCLIMode GHCPCLIPermissionMode
}

// GHCPCLIPermissionMode selects one of the two supported GHCP CLI permission
// strategies. The zero value is invalid and rejected by BuildGHCPCLIArgs.
type GHCPCLIPermissionMode string

const (
	// GHCPCLIModeUnresolved is the zero value. BuildGHCPCLIArgs returns
	// ErrGHCPCLIModeUnresolved when it encounters this.
	GHCPCLIModeUnresolved GHCPCLIPermissionMode = ""

	// GHCPCLIModeBlanket selects the existing --yolo behavior: blanket
	// permission grant, documented by the CLI as equivalent to
	// --allow-all-tools --allow-all-paths --allow-all-urls.
	GHCPCLIModeBlanket GHCPCLIPermissionMode = "blanket"

	// GHCPCLIModePartialAllowlist selects per-tool --allow-tool entries
	// derived from the agent's deployed tools frontmatter, omitting --yolo.
	GHCPCLIModePartialAllowlist GHCPCLIPermissionMode = "allowlist"
)

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
