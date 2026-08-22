package preflight

import (
	"fmt"
	"strings"

	"mosaic-agent-test/internal/authoring"
)

// FailureContext carries the AgentTest-side context a diagnostic helper
// needs to produce a structured, human-readable diagnostic from a
// delegate-tool failure. Every field is supplied by the call site, not
// extracted from the delegate's error.
type FailureContext struct {
	// Operation describes what was being attempted when the failure
	// occurred, e.g. "subject declaration dry-run render",
	// "stub definition dry-run render", "subject declaration dry-run deploy".
	Operation string

	// Provenance describes which configuration produced the input that
	// led to the failure, e.g. "--mosaic-root default: selfDir/../../..".
	// Always non-empty: the call site knows which config tier resolved
	// the value and must state it. If a future call site violates this
	// invariant, DelegateToolDiagnostic omits the provenance clause from
	// the message rather than embedding an empty string or a placeholder.
	Provenance string

	// DiagnosticCode is the stable machine-readable code for the resulting
	// authoring.Diagnostic, e.g. "unrenderable-subject-declaration",
	// "unrenderable-stub-definition", "undeployable-subject-declaration".
	DiagnosticCode string

	// Path is the authored file path for the diagnostic (the suite or
	// test definition file).
	Path string

	// Pointer is the structural location within the authored file,
	// e.g. "subject", "stub_agents[0].source".
	Pointer string
}

// DelegateToolDiagnostic builds a structured, human-readable
// authoring.Diagnostic from a delegate tool's structured failure fields
// and the AgentTest-side context in fc.
//
// The diagnostic's Message includes the operation being attempted and the
// configuration provenance. The Hint is keyed off the reason code: known
// codes produce specific, actionable suggestions; unknown or empty codes
// produce a generic hint. The ToolMessage is included in the Message body
// as supplementary detail but is never the sole content (FR-8).
//
// sf carries the four structured failure fields via the
// authoring.StructuredFailure interface. Both *agentdeploy.RenderError and
// *agentdeploy.DeployError already satisfy this interface, so a call site
// that has performed errors.As(err, &re) can pass re directly -- no
// additional type assertions are needed. This matches the pattern used by
// authoring.RunFailureReport.
func DelegateToolDiagnostic(
	fc FailureContext,
	sf authoring.StructuredFailure,
) authoring.Diagnostic {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("failed during %s", fc.Operation))
	if fc.Provenance != "" {
		sb.WriteString(fmt.Sprintf(" (provenance: %s)", fc.Provenance))
	}
	if toolMsg := sf.FailureToolMessage(); toolMsg != "" {
		sb.WriteString(fmt.Sprintf(": %s", toolMsg))
	}

	return authoring.Diagnostic{
		Severity: authoring.SeverityError,
		Code:     fc.DiagnosticCode,
		Path:     fc.Path,
		Pointer:  fc.Pointer,
		Message:  sb.String(),
		Hint:     hintForReason(sf.FailureReason()),
	}
}

// hintForReason returns an actionable suggested-fix string for a known
// delegate-tool reason code. Unknown or empty codes return a generic hint
// directing the user to check the tool configuration.
func hintForReason(reason string) string {
	switch reason {
	case "harness-unresolvable":
		return "check that --mosaic-root points to a valid MOSAIC repository root"
	case "":
		return "check tool configuration; the delegate tool reported no structured reason"
	default:
		return fmt.Sprintf("check tool configuration; reason code %q is not recognised by this version of mosaic-agent-test", reason)
	}
}
