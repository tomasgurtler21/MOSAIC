package cli

// render_output.go contains the output rendering and the failure envelope for the render
// subcommand. The sentinel-to-reason mapping is the stable cross-tool contract described
// in the design; renaming a reason string is a breaking change.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"mosaic-deploy/internal/app"
)

// RenderFailure is what the render subcommand writes to stdout under --output json when the
// render did not happen. Its presence means nothing was written, exactly as a non-nil service
// error means in-process.
//
// It exists because a consuming tool across a process boundary otherwise receives only an exit
// code, which cannot distinguish one reason from another.
type RenderFailure struct {
	// Error is always true so a consumer that decoded the wrong shape can tell immediately.
	Error bool `json:"error"`

	// Reason is the stable machine-readable failure code. It is the only field a consumer
	// may branch on. An unrecognised value must be treated as "undifferentiated failure".
	Reason string `json:"reason"`

	// Message is the tool's own human-readable diagnostic — the same text written to stderr.
	// For humans only: it is not stable and must never be pattern-matched.
	Message string `json:"message"`

	// Detail names the offending input when the failure identifies one. Empty when the
	// failure names no single input.
	Detail string `json:"detail,omitempty"`

	// ExitCode is the process exit code accompanying this document.
	ExitCode int `json:"exitCode"`
}

// renderRenderAgentOutput writes the render result and returns the appropriate exit code.
// On success it writes the result in the chosen format. On failure it writes the error to
// errOut and, when format is "json", also emits a RenderFailure envelope to out.
// Render has no partial-success state, so the only non-error exit code is ExitSuccess.
func renderRenderAgentOutput(out, errOut io.Writer, format string, result app.RenderAgentResult, svcErr error) int {
	if svcErr != nil {
		msg := svcErr.Error()
		fmt.Fprintf(errOut, "error: %v\n", svcErr)
		if strings.EqualFold(format, "json") {
			env := RenderFailure{
				Error:    true,
				Reason:   renderSentinelToReason(svcErr),
				Message:  msg,
				ExitCode: ExitFailure,
			}
			enc := json.NewEncoder(out)
			_ = enc.Encode(env)
		}
		return ExitFailure
	}

	if strings.EqualFold(format, "json") {
		enc := json.NewEncoder(out)
		_ = enc.Encode(result)
	} else {
		renderRenderAgentHuman(out, result)
	}
	return ExitSuccess
}

// renderRenderAgentUsageError writes a CLI-detected usage error for the render subcommand.
// The message is written to errOut. When format is "json", a RenderFailure envelope is also
// written to out so a programmatic caller sees the same shape for usage failures and service
// failures. Returns ExitUsage.
func renderRenderAgentUsageError(out, errOut io.Writer, format string, msg string, reason string) int {
	fmt.Fprintf(errOut, "error: %s\n", msg)
	if strings.EqualFold(format, "json") {
		env := RenderFailure{
			Error:    true,
			Reason:   reason,
			Message:  msg,
			ExitCode: ExitUsage,
		}
		enc := json.NewEncoder(out)
		_ = enc.Encode(env)
	}
	return ExitUsage
}

// renderSentinelToReason maps an ErrRender* sentinel to its documented stable reason code.
// The mapping is total: every sentinel has exactly one code, and the unmapped case yields
// "internal". The reason strings are part of the cross-tool contract; renaming one is a
// breaking change.
func renderSentinelToReason(err error) string {
	switch {
	case errors.Is(err, app.ErrRenderSourceRequired):
		return "source-required"
	case errors.Is(err, app.ErrRenderSourceAmbiguous):
		return "source-ambiguous"
	case errors.Is(err, app.ErrRenderSourceNotFound):
		return "source-not-found"
	case errors.Is(err, app.ErrRenderSourceNotFile):
		return "source-not-file"
	case errors.Is(err, app.ErrRenderAgentNotInCatalog):
		return "agent-not-in-catalog"
	case errors.Is(err, app.ErrRenderSourceNotGeneric):
		return "source-not-generic"
	case errors.Is(err, app.ErrRenderInvalidRole):
		return "invalid-role"
	case errors.Is(err, app.ErrRenderHarnessRequired):
		return "harness-required"
	case errors.Is(err, app.ErrRenderHarnessUnresolvable):
		return "harness-unresolvable"
	case errors.Is(err, app.ErrRenderDestinationRequired):
		return "destination-required"
	case errors.Is(err, app.ErrRenderDestinationAmbiguous):
		return "destination-ambiguous"
	case errors.Is(err, app.ErrRenderDestinationExists):
		return "destination-exists"
	case errors.Is(err, app.ErrRenderUnknownWorkflow):
		return "unknown-workflow"
	case errors.Is(err, app.ErrRenderWorkflowsNotApplicable):
		return "workflows-not-applicable"
	default:
		return "internal"
	}
}

// renderRenderAgentHuman writes a human-readable summary of a render to out. The output
// names the source and destination paths, the harness, the agent key, and the version.
// For an orchestrator result with workflow ids, the workflow ids are listed. A dry-run
// result carries a "dry run: " prefix so the user knows nothing was written.
func renderRenderAgentHuman(out io.Writer, result app.RenderAgentResult) {
	dryPrefix := ""
	if result.DryRun {
		dryPrefix = "dry run: "
	}
	fmt.Fprintf(out, "%srender complete\nsource: %s\ndestination: %s\nharness: %s\nagent: %s\nversion: %s\n",
		dryPrefix, result.SourcePath, result.DestinationPath, result.TargetHarnessID,
		result.AgentKey, result.SourceVersion)
	if len(result.Workflows) > 0 {
		fmt.Fprintf(out, "workflows: %s\n", strings.Join(result.Workflows, ", "))
	}
}
