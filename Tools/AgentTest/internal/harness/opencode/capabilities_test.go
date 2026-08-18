package opencode_test

// Tests for the adapter's identity and declared capabilities: the values
// this harness's interception layer can honestly claim, per
// ContractsDesign.md's "AgentTest: opencode.Adapter" section.
//
// SupportsDirectSubstitution is false: the pre-invocation hook can block a
// call or rewrite its arguments, but cannot fabricate a successful result —
// throwing surfaces to the subject as a refusal, not a response — and the
// post-invocation hook's output mutations are confirmed not to be honoured
// downstream. Every stubbed call therefore travels the prompt-rewrite path.
//
// The conformance suite's capability-honesty test (run separately, once the
// adapter's interception facet exists) drives a stubbed invocation and
// asserts the *observed* effect matches this declaration; the tests here
// only check the declaration itself is the one the design specifies.

import (
	"reflect"
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/opencode"
)

func TestID_ReturnsHarnessID(t *testing.T) {
	a := opencode.New(opencode.Options{})
	if got := a.ID(); got != opencode.HarnessID {
		t.Errorf("ID() = %q, want %q", got, opencode.HarnessID)
	}
}

// TestHarnessID_MatchesSharedCatalogIdentity asserts the adapter's own
// identity constant is identical to the shared catalog's, so the two
// cannot silently drift apart — the same reason the claudecode adapter
// keeps its own constant rather than depending on the catalog it does not
// otherwise need.
func TestHarnessID_MatchesSharedCatalogIdentity(t *testing.T) {
	if opencode.HarnessID != commonharness.HarnessIDOpenCode {
		t.Errorf("opencode.HarnessID = %q, want it to equal commonharness.HarnessIDOpenCode %q", opencode.HarnessID, commonharness.HarnessIDOpenCode)
	}
}

func TestCapabilities_DirectSubstitutionUnsupported(t *testing.T) {
	caps := opencode.Capabilities()
	if caps.SupportsDirectSubstitution {
		t.Errorf("Capabilities: SupportsDirectSubstitution = true, want false — the pre-invocation hook can rewrite or block a call but cannot fabricate a successful result")
	}
}

func TestCapabilities_PostInterceptionSupported(t *testing.T) {
	caps := opencode.Capabilities()
	if !caps.SupportsPostInterception {
		t.Errorf("Capabilities: SupportsPostInterception = false, want true — a post-invocation hook exists and observes the real result")
	}
}

// TestCapabilities_ReplyRecoveryUnsupported asserts opencode receives no
// echo-fidelity-recovery change (AC12.9): unlike claudecode, it has no
// completion-signal-driven reply recovery, so this stays false as an
// explicit regression guard rather than holding only by the field's
// zero-value default.
func TestCapabilities_ReplyRecoveryUnsupported(t *testing.T) {
	caps := opencode.Capabilities()
	if caps.SupportsReplyRecovery {
		t.Errorf("Capabilities: SupportsReplyRecovery = true, want false — opencode receives no echo-fidelity-recovery change in this stage")
	}
}

// TestCapabilities_CorrelationFieldIsPromptArgument asserts the declared
// correlation field is the dispatch tool's prompt argument, which is the
// field a planted token survives in because this harness echoes it to the
// post-invocation hook.
func TestCapabilities_CorrelationFieldIsPromptArgument(t *testing.T) {
	caps := opencode.Capabilities()
	if caps.CorrelationField != "args.prompt" {
		t.Errorf("Capabilities: CorrelationField = %q, want %q", caps.CorrelationField, "args.prompt")
	}
}

// TestCapabilities_RegistrationModelIsDirectoryDrop asserts the declared
// registration model: OpenCode discovers plugins by their presence in the
// plugins directory, so there is no shared configuration document to
// register them in.
func TestCapabilities_RegistrationModelIsDirectoryDrop(t *testing.T) {
	caps := opencode.Capabilities()
	if caps.RegistrationModel != domain.RegistrationDirectoryDrop {
		t.Errorf("Capabilities: RegistrationModel = %q, want %q", caps.RegistrationModel, domain.RegistrationDirectoryDrop)
	}
}

// TestCapabilities_BridgeKindIsInProcess asserts the declared bridge kind:
// the plugin loads inside the harness's own runtime and forwards to the
// interceptor binary as a child process, rather than the harness spawning a
// command per intercepted call.
func TestCapabilities_BridgeKindIsInProcess(t *testing.T) {
	caps := opencode.Capabilities()
	if caps.BridgeKind != domain.BridgeInProcess {
		t.Errorf("Capabilities: BridgeKind = %q, want %q", caps.BridgeKind, domain.BridgeInProcess)
	}
}

// TestCapabilities_IsConstantAcrossCalls asserts the declared value does
// not vary between calls: Capabilities() MUST be constant for the
// adapter's lifetime, per domain.HarnessAdapter's own contract.
func TestCapabilities_IsConstantAcrossCalls(t *testing.T) {
	first := opencode.Capabilities()
	second := opencode.Capabilities()
	if !reflect.DeepEqual(first, second) {
		t.Errorf("Capabilities: first call = %+v, second call = %+v; the declared value must be constant", first, second)
	}
}

// TestCapabilities_PackageFunctionAndMethodAgree asserts the package-level
// function and the method report the same declaration, so the declared
// values can be asserted without constructing an adapter.
func TestCapabilities_PackageFunctionAndMethodAgree(t *testing.T) {
	a := opencode.New(opencode.Options{})
	if !reflect.DeepEqual(a.Capabilities(), opencode.Capabilities()) {
		t.Errorf("Capabilities: method = %+v, package function = %+v; they must agree", a.Capabilities(), opencode.Capabilities())
	}
}
