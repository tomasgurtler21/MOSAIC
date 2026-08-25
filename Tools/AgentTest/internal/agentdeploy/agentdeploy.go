// Package agentdeploy implements domain.AgentDeployer by delegating to the
// compiled mosaic-deploy binary. No harness-specific knowledge lives here: the
// port invokes the binary and parses its JSON output, exactly as internal/cost
// invokes mosaic-log-analyzer.
//
// This module must not import mosaic-deploy. Both modules are in Tools/go.work
// and an import would compile — and would then drag the deployment tool's
// harness knowledge across AgentTest's harness isolation boundary. The binary
// invocation avoids that entirely.
package agentdeploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"mosaic-agent-test/internal/domain"
)

// The deployment tool's stable exit codes, mirrored here as plain local
// constants. This package must not import mosaic-deploy to learn its own
// outcome contract — see Tools/Deployment/internal/cli/exitcodes.go for the
// authoritative source. An import would compile (both are in Tools/go.work)
// but would pull concrete harness knowledge across the isolation boundary.
const (
	exitSuccess = 0
	exitFailure = 1
	exitUsage   = 3
)

// Sentinel errors for every distinct failure class. Callers use errors.Is to
// detect the class and errors.As with *RenderError to read the detail.
var (
	// ErrToolUnavailable is returned when the deployment tool could not be
	// invoked at all — the binary is absent, not executable, or failed to
	// start.
	ErrToolUnavailable = errors.New("agentdeploy: deployment tool unavailable")

	// ErrTimedOut is returned when the invocation exceeded Options.Timeout.
	// The delegate is never awaited after this; the port guarantees no hang.
	ErrTimedOut = errors.New("agentdeploy: timed out")

	// ErrRenderFailed is returned when the tool ran and exited non-zero.
	// Always unwrapped from a *RenderError that carries the delegate's reason
	// code and diagnostic text. Callers branch with errors.Is to detect the
	// class and errors.As(&re) to read re.Reason for the specific condition.
	ErrRenderFailed = errors.New("agentdeploy: render failed")

	// ErrUnparseableOutput is returned when the tool exited zero but produced
	// output that could not be decoded as the expected JSON result.
	ErrUnparseableOutput = errors.New("agentdeploy: unparseable output")

	// ErrDeployFailed is returned when the tool ran and exited non-zero on a
	// deploy. Always unwrapped from a *DeployError.
	ErrDeployFailed = errors.New("agentdeploy: deploy failed")
)

// RenderError carries diagnostic detail for a failed render. It always wraps
// ErrRenderFailed so callers can distinguish it from other error classes with
// errors.Is, then inspect the reason with errors.As.
//
// The Reason strings are the deployment tool's own reason codes passed through
// unchanged. This package neither renames nor re-groups them, so there is
// exactly one vocabulary to keep in step across the process boundary.
type RenderError struct {
	// Reason is the delegate's stable failure code from its JSON failure
	// envelope. Empty when the delegate emitted no envelope (an older binary,
	// a crash before the writer ran, or empty stdout on non-zero exit). Callers
	// treat an empty Reason as "undifferentiated failure" and must never map it
	// to a specific known condition.
	Reason string
	// Detail is the offending input the delegate named (e.g. the workflow id
	// for "unknown-workflow", the agent key for "agent-not-in-catalog"). Empty
	// when the failure names no single input.
	Detail string
	// ToolMessage is the delegate's own human-readable diagnostic text, taken
	// from the envelope's message field when present and otherwise from
	// captured stderr. It is quoted verbatim in caller-facing messages and
	// never pattern-matched by code.
	ToolMessage string
	// ExitCode is the delegate's process exit code.
	ExitCode int
}

// DeployError carries diagnostic detail for a failed deploy. It always wraps
// ErrDeployFailed.
//
// The deploy subcommand emits no structured JSON failure envelope: on a
// service error it writes a plain-text line to stderr and exits, even under
// --output json. Reason and Detail are therefore empty on every failure the
// current tool can produce. They exist so that a future envelope can be
// decoded without a breaking change to this type; callers must treat an
// empty Reason as "undifferentiated failure" and must never map it to a
// specific known condition.
type DeployError struct {
	Reason      string // empty with the current tool; reserved
	Detail      string // empty with the current tool; reserved
	ToolMessage string // the delegate's own text, from stderr
	ExitCode    int
}

func (e *DeployError) Error() string {
	return fmt.Sprintf("deploy failed (exit %d): %s", e.ExitCode, e.ToolMessage)
}

// Unwrap returns ErrDeployFailed so callers can use errors.Is(err, ErrDeployFailed)
// to detect the class without knowing the concrete type.
func (e *DeployError) Unwrap() error { return ErrDeployFailed }

// FailureReason, FailureDetail, FailureToolMessage and FailureExitCode expose
// the four structured-failure fields through the authoring.StructuredFailure
// interface without importing the authoring package. The accessor set is
// satisfied structurally: no import in either direction.
func (e *DeployError) FailureReason() string      { return e.Reason }
func (e *DeployError) FailureDetail() string      { return e.Detail }
func (e *DeployError) FailureToolMessage() string { return e.ToolMessage }
func (e *DeployError) FailureExitCode() int       { return e.ExitCode }

func (e *RenderError) Error() string {
	return fmt.Sprintf("render failed (exit %d, reason %q, detail %q): %s",
		e.ExitCode, e.Reason, e.Detail, e.ToolMessage)
}

// Unwrap returns ErrRenderFailed so callers can use errors.Is(err, ErrRenderFailed)
// to detect the class without knowing the concrete type.
func (e *RenderError) Unwrap() error { return ErrRenderFailed }

// FailureReason, FailureDetail, FailureToolMessage and FailureExitCode expose
// the four structured-failure fields through the authoring.StructuredFailure
// interface without importing the authoring package. The accessor set is
// satisfied structurally: no import in either direction.
func (e *RenderError) FailureReason() string      { return e.Reason }
func (e *RenderError) FailureDetail() string      { return e.Detail }
func (e *RenderError) FailureToolMessage() string { return e.ToolMessage }
func (e *RenderError) FailureExitCode() int       { return e.ExitCode }

// CommandRunner is the seam that makes the outcome mapping testable without
// the real deployment tool present.
//
// Unlike internal/cost.CommandRunner, this seam returns stderr as well as
// stdout. The cost provider discards stderr because its only failure question
// is whether the tool worked; this port must additionally report why it did
// not, in the delegate's own words, so a preflight diagnostic can quote the
// deployment tool rather than paraphrase it. The real implementation therefore
// captures stderr into a buffer instead of io.Discard.
//
// stderr is for humans. Code never branches on it — branching is on the reason
// code carried in the delegate's JSON failure envelope.
type CommandRunner func(ctx context.Context, path string, args []string) (stdout, stderr []byte, exitCode int, err error)

// Options configures the delegating deployer.
type Options struct {
	// ExecutablePath is the deployment tool's binary.
	ExecutablePath string
	// MosaicRoot overrides the deployment tool's own root resolution. Empty
	// means "do not override" — the flag is simply not passed and the tool
	// resolves its own root.
	MosaicRoot string
	// CatalogFolder overrides the catalogue directory the deployment tool
	// sources agents and workflows from. Empty means "do not override" — the
	// flag is simply not passed and the tool resolves its own catalogue under
	// its own root.
	//
	// It lives here rather than on any per-call request struct because it
	// applies uniformly to every subprocess call this port makes, Render and
	// Deploy alike.
	CatalogFolder string
	// Timeout bounds the delegate's runtime. A positive value causes every
	// Render call to apply context.WithTimeout; zero means no added timeout
	// beyond the caller's own context.
	Timeout time.Duration
	// Invoke is the seam that makes the outcome mapping testable without the
	// real tool present. nil selects real process execution via execCommandRunner.
	Invoke CommandRunner
}

// New constructs a domain.AgentDeployer that delegates to the deployment tool
// described by opts.
func New(opts Options) domain.AgentDeployer {
	return &deployer{opts: opts}
}

type deployer struct {
	opts Options
}

// Compile-time assertion that *deployer satisfies domain.AgentDeployer.
var _ domain.AgentDeployer = (*deployer)(nil)

// wireResult mirrors the JSON tags of app.RenderAgentResult from
// Tools/Deployment/internal/app/render_contracts.go. It is a wire model and
// nothing else: no arithmetic and no re-derivation is performed here.
type wireResult struct {
	DestinationPath    string   `json:"destinationPath"`
	SourceVersion      string   `json:"sourceVersion"`
	AgentKey           string   `json:"agentKey"`
	CreatedDirectories []string `json:"createdDirectories"`
}

// wireFailure mirrors the JSON tags of cli.RenderFailure from
// Tools/Deployment/internal/cli/render_output.go. This is a cross-tool
// contract: adding a field here is compatible, but renaming or repurposing
// "reason" is a breaking change.
type wireFailure struct {
	Error    bool   `json:"error"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
	Detail   string `json:"detail"`
	ExitCode int    `json:"exitCode"`
}

// wireRunSummary mirrors the JSON shape of domain.RunSummary from
// Tools/Deployment/internal/domain/outcome.go. That type carries no JSON
// struct tags, so its keys are its Go field names verbatim. Only the fields
// this port reads are modelled; unknown keys are ignored by the decoder.
type wireRunSummary struct {
	Actions []wireActionRecord `json:"Actions"`
	Outcome string             `json:"Outcome"`
}

type wireActionRecord struct {
	Ref           wireArtifactRef `json:"Ref"`
	TargetPath    string          `json:"TargetPath"`
	Taken         string          `json:"Taken"`
	Err           string          `json:"Err"`
	SourceVersion string          `json:"SourceVersion"`
}

type wireArtifactRef struct {
	Kind string `json:"Kind"`
	Key  string `json:"Key"`
}

// artifactKindAgent is domain.ArtifactAgent's value, mirrored as a local
// constant. This package must not import mosaic-deploy to learn it.
const artifactKindAgent = "agent"

// buildArgs constructs the argument list for the deployment tool's render
// subcommand from the request and options. This is the only place the flag
// names are spelled: a flag spelled here and not passed is a contract break,
// and a flag spelled here that the tool no longer accepts is equally one.
func buildArgs(req domain.RenderAgentRequest, opts Options) []string {
	args := []string{"render"}

	// Source: exactly one of --source-agent or --source.
	if req.CatalogAgentKey != "" {
		args = append(args, "--source-agent", req.CatalogAgentKey)
	} else if req.SourcePath != "" {
		args = append(args, "--source", req.SourcePath)
	}

	// Destination: --harness and --workspace are always present. The harness
	// descriptor resolves where under the workspace root the file belongs, so
	// this package never knows or reconstructs that path.
	args = append(args, "--harness", req.HarnessID)
	args = append(args, "--workspace", req.WorkspaceRoot)

	// Optional flags: omit when the request carries no value.
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	// The nil/empty distinction: nil means "not specified" (flag absent);
	// non-nil empty means "explicitly none" (flag present with empty value).
	if req.Workflows != nil {
		args = append(args, "--workflows", strings.Join(req.Workflows, ","))
	}

	// --overwrite is always present: a freshly created sandbox may already
	// contain a seeded file at the destination, and a render that refuses to
	// overwrite would stall setup.
	args = append(args, "--overwrite")

	if req.DryRun {
		args = append(args, "--dry-run")
	}

	// --output json is always present so the deployer can parse the result
	// document rather than scraping human-readable output.
	args = append(args, "--output", "json")

	// --mosaic-root is omitted when empty, leaving the tool to resolve its own
	// root — the correct behaviour for a correctly staged distribution.
	if opts.MosaicRoot != "" {
		args = append(args, "--mosaic-root", opts.MosaicRoot)
	}

	// --catalog-folder is omitted when empty, leaving the tool to resolve its
	// own catalogue under its own root. Both root-scoped overrides sit together
	// at the end of the list.
	if opts.CatalogFolder != "" {
		args = append(args, "--catalog-folder", opts.CatalogFolder)
	}

	return args
}

// Render invokes the deployment tool through the CommandRunner seam, parses
// its JSON output, and maps every outcome to a typed result or error.
//
// Exit 0 with decodable JSON → success result.
// Exit 0 with undecodable output → ErrUnparseableOutput.
// Non-zero exit with decodable envelope → ErrRenderFailed wrapped in *RenderError.
// Non-zero exit without decodable envelope → ErrRenderFailed with empty Reason.
// Invocation failure (binary missing, etc.) → ErrToolUnavailable.
// Port timeout exceeded → ErrTimedOut.
func (d *deployer) Render(ctx context.Context, req domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
	invoke := d.opts.Invoke
	if invoke == nil {
		invoke = execCommandRunner
	}

	runCtx := ctx
	if d.opts.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, d.opts.Timeout)
		defer cancel()
	}

	args := buildArgs(req, d.opts)
	stdout, stderr, exitCode, invokeErr := invoke(runCtx, d.opts.ExecutablePath, args)

	if invokeErr != nil {
		// The port's own timeout fired: the delegate did not respond in time.
		// This is distinguished from a caller cancellation by checking whether
		// our runCtx has expired with DeadlineExceeded, which only our
		// WithTimeout can cause (a cancellation from the caller produces Canceled).
		if runCtx.Err() == context.DeadlineExceeded {
			return domain.RenderAgentResult{}, fmt.Errorf(
				"%w: deployment tool %q did not respond within the configured timeout of %v",
				ErrTimedOut, d.opts.ExecutablePath, d.opts.Timeout,
			)
		}
		// Caller's context was cancelled (context.Canceled) or the binary could
		// not be invoked at all (exec error). Context cancellations are returned
		// as-is; exec/OS failures are wrapped in ErrToolUnavailable, which names
		// the binary path so the operator knows what was searched.
		if errors.Is(invokeErr, context.Canceled) || errors.Is(invokeErr, context.DeadlineExceeded) {
			return domain.RenderAgentResult{}, fmt.Errorf(
				"agentdeploy: invocation of %q cancelled: %w",
				d.opts.ExecutablePath, invokeErr,
			)
		}
		return domain.RenderAgentResult{}, fmt.Errorf(
			"%w: deployment tool %q could not be invoked: %v",
			ErrToolUnavailable, d.opts.ExecutablePath, invokeErr,
		)
	}

	if exitCode == exitSuccess {
		var result wireResult
		if err := json.Unmarshal(stdout, &result); err != nil {
			return domain.RenderAgentResult{}, fmt.Errorf(
				"%w: deployment tool %q exited 0 but produced %d bytes of stdout that could not be decoded: %v",
				ErrUnparseableOutput, d.opts.ExecutablePath, len(stdout), err,
			)
		}
		return domain.RenderAgentResult{
			DestinationPath:    result.DestinationPath,
			SourceVersion:      result.SourceVersion,
			AgentKey:           result.AgentKey,
			CreatedDirectories: result.CreatedDirectories,
		}, nil
	}

	// Non-zero exit: the delegate rejected the request or hit a service error.
	// Try to decode the structured failure envelope (AD-8). If it cannot be
	// decoded (older binary, crash dump, empty stdout), degrade to
	// undifferentiated failure with the stderr text as the human message.
	var failure wireFailure
	if err := json.Unmarshal(stdout, &failure); err != nil || !failure.Error {
		// No envelope: reason is empty ("undifferentiated"), message from stderr.
		return domain.RenderAgentResult{}, &RenderError{
			Reason:      "",
			Detail:      "",
			ToolMessage: string(stderr),
			ExitCode:    exitCode,
		}
	}
	return domain.RenderAgentResult{}, &RenderError{
		Reason:      failure.Reason,
		Detail:      failure.Detail,
		ToolMessage: failure.Message,
		ExitCode:    failure.ExitCode,
	}
}

// buildDeployArgs constructs the argument list for the deployment tool's deploy
// subcommand from the request, options, and an optional selections file path.
// This is the only place the deploy subcommand's flag names are spelled.
//
// Unlike buildArgs for render, --workflows is always emitted — even when the
// caller supplies nil — to prevent a nil selection from resolving silently to
// an empty deployment that succeeds while writing only the orchestrator.
func buildDeployArgs(req domain.DeployRequest, opts Options, selectionsPath string) []string {
	args := []string{"deploy"}

	// --harness and --workspace are always present.
	args = append(args, "--harness", req.HarnessID)
	args = append(args, "--workspace", req.WorkspaceRoot)

	// --workflows is ALWAYS emitted on the deploy subcommand. nil Workflows
	// ("caller did not specify") still emits the flag with an empty value rather
	// than omitting it: an omitted flag causes the tool to silently resolve to
	// an empty list, deploying only the orchestrator while reporting success.
	// strings.Join handles nil and empty slices identically, producing "".
	args = append(args, "--workflows", strings.Join(req.Workflows, ","))

	// --auto-confirm suppresses the plan-review gate, which fires before every
	// deploy including dry runs. Without it every call fails on the gate.
	args = append(args, "--auto-confirm")

	if req.DryRun {
		args = append(args, "--dry-run")
	}

	// --output json is always present so the deployer can parse the run summary.
	args = append(args, "--output", "json")

	// --mosaic-root, --catalog-folder, and --log-dir all follow the
	// empty-means-omitted convention: the flag is omitted when the field is
	// empty, and emitted verbatim when non-empty. All three root-scoped
	// overrides are kept together so the convention is visible in one place.
	if opts.MosaicRoot != "" {
		args = append(args, "--mosaic-root", opts.MosaicRoot)
	}
	if opts.CatalogFolder != "" {
		args = append(args, "--catalog-folder", opts.CatalogFolder)
	}
	// --log-dir overrides the directory the deployment tool writes its two
	// sink files (latest.log and history.log) to. Omitted when empty so the
	// tool falls back to its own default location — the correct behaviour for
	// callers that have not opted into per-run isolation. When non-empty the
	// value is the run's own sandbox location so concurrent invocations each
	// write to their own directory and cannot contend on a shared path.
	if req.LogDir != "" {
		args = append(args, "--log-dir", req.LogDir)
	}

	// --selections is appended when a temporary selections file was written
	// (carrying tier_models and/or infrastructure_agents).
	if selectionsPath != "" {
		args = append(args, "--selections", selectionsPath)
	}

	return args
}

// writeSelectionsFile creates a temporary YAML file carrying the caller's
// tier-model pre-answers and infrastructure-agent selection. The file path is
// returned so the caller can pass it to --selections and remove it
// unconditionally.
//
// tier_models is written only when the map is non-empty. infrastructure_agents
// is always written when infrastructureAgentIDs is non-nil: a non-nil empty
// slice produces "infrastructure_agents: []" (explicitly none) and a populated
// slice produces each ID as a list item. A nil slice omits the key entirely,
// leaving the deploy tool free to ask its interactive question.
//
// No workflows key is written: an emitted workflows key — even empty — would
// fight the --workflows flag the same Deploy call carries.
func writeSelectionsFile(tierModels map[string]string, infrastructureAgentIDs []string) (string, error) {
	f, err := os.CreateTemp("", "agentdeploy-selections-*.yaml")
	if err != nil {
		return "", err
	}
	defer f.Close()

	if len(tierModels) > 0 {
		if _, err := fmt.Fprintln(f, "tier_models:"); err != nil {
			return f.Name(), err
		}
		for k, v := range tierModels {
			if _, err := fmt.Fprintf(f, "  %s: %s\n", k, v); err != nil {
				return f.Name(), err
			}
		}
	}

	if infrastructureAgentIDs != nil {
		if len(infrastructureAgentIDs) == 0 {
			if _, err := fmt.Fprintln(f, "infrastructure_agents: []"); err != nil {
				return f.Name(), err
			}
		} else {
			if _, err := fmt.Fprintln(f, "infrastructure_agents:"); err != nil {
				return f.Name(), err
			}
			for _, id := range infrastructureAgentIDs {
				if _, err := fmt.Fprintf(f, "- %s\n", id); err != nil {
					return f.Name(), err
				}
			}
		}
	}

	return f.Name(), nil
}

// Deploy invokes the deployment tool through the CommandRunner seam, parses
// its JSON run summary, and maps every outcome to a typed result or error.
//
// Exit 0 with decodable JSON → DeployResult with agent list, nil error.
// Exit 0 with undecodable output → ErrUnparseableOutput.
// Non-zero exit → *DeployError wrapping ErrDeployFailed; ToolMessage from stderr.
// Invocation failure (binary missing, etc.) → ErrToolUnavailable.
// Port timeout exceeded → ErrTimedOut.
// Caller cancellation → the cancellation error propagated as-is.
func (d *deployer) Deploy(ctx context.Context, req domain.DeployRequest) (domain.DeployResult, error) {
	invoke := d.opts.Invoke
	if invoke == nil {
		invoke = execCommandRunner
	}

	runCtx := ctx
	if d.opts.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, d.opts.Timeout)
		defer cancel()
	}

	// Write the temporary selections file when the caller supplies tier-model
	// pre-answers or infrastructure-agent IDs. The defer ensures removal on
	// every exit path: success, non-zero exit, undecodable output,
	// tool-unavailable, timeout, and caller cancellation.
	var selectionsPath string
	if len(req.TierModels) > 0 || req.InfrastructureAgentIDs != nil {
		path, err := writeSelectionsFile(req.TierModels, req.InfrastructureAgentIDs)
		if err != nil {
			return domain.DeployResult{}, fmt.Errorf("agentdeploy: could not write selections file: %w", err)
		}
		selectionsPath = path
		defer os.Remove(selectionsPath) //nolint:errcheck
	}

	args := buildDeployArgs(req, d.opts, selectionsPath)
	stdout, stderr, exitCode, invokeErr := invoke(runCtx, d.opts.ExecutablePath, args)

	if invokeErr != nil {
		// The port's own timeout fired: the delegate did not respond in time.
		if runCtx.Err() == context.DeadlineExceeded {
			return domain.DeployResult{}, fmt.Errorf(
				"%w: deployment tool %q did not respond within the configured timeout of %v",
				ErrTimedOut, d.opts.ExecutablePath, d.opts.Timeout,
			)
		}
		// Caller's context was cancelled: propagate as-is, never remap to
		// ErrTimedOut or ErrToolUnavailable.
		if errors.Is(invokeErr, context.Canceled) || errors.Is(invokeErr, context.DeadlineExceeded) {
			return domain.DeployResult{}, fmt.Errorf(
				"agentdeploy: invocation of %q cancelled: %w",
				d.opts.ExecutablePath, invokeErr,
			)
		}
		// Binary missing or not startable.
		return domain.DeployResult{}, fmt.Errorf(
			"%w: deployment tool %q could not be invoked: %v",
			ErrToolUnavailable, d.opts.ExecutablePath, invokeErr,
		)
	}

	if exitCode == exitSuccess {
		var summary wireRunSummary
		if err := json.Unmarshal(stdout, &summary); err != nil {
			return domain.DeployResult{}, fmt.Errorf(
				"%w: deployment tool %q exited 0 but produced %d bytes of stdout that could not be decoded: %v",
				ErrUnparseableOutput, d.opts.ExecutablePath, len(stdout), err,
			)
		}
		// Filter to agent artifacts only; all other kinds (skills, hooks,
		// manifests) are dropped. Order is preserved from the tool's output.
		var agents []domain.DeployedAgent
		for _, action := range summary.Actions {
			if action.Ref.Kind == artifactKindAgent {
				agents = append(agents, domain.DeployedAgent{
					Key:             action.Ref.Key,
					DestinationPath: action.TargetPath,
					SourceVersion:   action.SourceVersion,
				})
			}
		}
		return domain.DeployResult{
			Agents:             agents,
			CreatedDirectories: nil, // run summary carries no created-directory list
		}, nil
	}

	// Non-zero exit: the deploy subcommand emits no structured failure envelope,
	// so the degrade path is the only path. ToolMessage comes from stderr.
	// Reason and Detail are empty by design — reserved for a future envelope.
	return domain.DeployResult{}, &DeployError{
		Reason:      "",
		Detail:      "",
		ToolMessage: string(stderr),
		ExitCode:    exitCode,
	}
}

// execCommandRunner is the real process-execution implementation of
// CommandRunner. It captures both stdout and stderr, which this port needs
// (unlike internal/cost, which discards stderr).
func execCommandRunner(ctx context.Context, path string, args []string) (stdout, stderr []byte, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, path, args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	if runErr == nil {
		return stdoutBuf.Bytes(), stderrBuf.Bytes(), 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return stdoutBuf.Bytes(), stderrBuf.Bytes(), exitErr.ExitCode(), nil
	}
	// The delegate could not be started or run at all — the binary is missing,
	// not executable, or killed by a signal. Return the error so the caller can
	// map it to ErrToolUnavailable.
	return nil, stderrBuf.Bytes(), 0, runErr
}

// discardSink is the zero-value of io.Writer used only to ensure execCommandRunner
// does not discard stderr.
var _ io.Writer = (*bytes.Buffer)(nil)
