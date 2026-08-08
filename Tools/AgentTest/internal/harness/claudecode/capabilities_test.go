package claudecode_test

// Tests for capability declaration (T11.5). The values are a constraint
// derived from what this harness's interception points can do, not a
// choice: the pre-invocation point can rewrite a call's input but cannot
// fabricate a successful result, so every stubbed call travels the
// prompt-rewrite path and direct substitution is declared unsupported.
//
// The conformance suite's capability-honesty test — which drives a stubbed
// invocation through the adapter and asserts the *observed effect* matches
// this declaration, rather than reading the flag back — runs against this
// adapter via TestConformance in conformance_test.go. A test that merely
// reads the flag proves nothing on its own, which is why it is exercised
// there and only declaration correctness is asserted here.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

func TestCapabilities_DirectSubstitutionUnsupported(t *testing.T) {
	caps := claudecode.Capabilities()
	if caps.SupportsDirectSubstitution {
		t.Errorf("Capabilities: SupportsDirectSubstitution = true, want false — this harness's interception point cannot fabricate a successful result")
	}
}

func TestCapabilities_PostInterceptionSupported(t *testing.T) {
	caps := claudecode.Capabilities()
	if !caps.SupportsPostInterception {
		t.Errorf("Capabilities: SupportsPostInterception = false, want true — a post-invocation point exists and observes the real result")
	}
}

func TestCapabilities_CorrelationFieldDeclared(t *testing.T) {
	caps := claudecode.Capabilities()
	if caps.CorrelationField == "" {
		t.Errorf("Capabilities: CorrelationField is empty; the adapter must nominate a field")
	}
}

func TestCapabilities_RegistrationAndBridgeKind(t *testing.T) {
	caps := claudecode.Capabilities()
	if caps.RegistrationModel != domain.RegistrationSharedFile {
		t.Errorf("Capabilities: RegistrationModel = %q, want %q", caps.RegistrationModel, domain.RegistrationSharedFile)
	}
	if caps.BridgeKind != domain.BridgeSpawned {
		t.Errorf("Capabilities: BridgeKind = %q, want %q", caps.BridgeKind, domain.BridgeSpawned)
	}
}

// TestCapabilities_PackageFunctionAndMethodAgree asserts the package-level
// function and the method report the same declaration, since the method's
// doc comment states it is "a package-level function as well as a method"
// so the declared values can be asserted without constructing an adapter.
func TestCapabilities_PackageFunctionAndMethodAgree(t *testing.T) {
	a := claudecode.New(claudecode.Options{})
	if a.Capabilities() != claudecode.Capabilities() {
		t.Errorf("Capabilities: method = %+v, package function = %+v; they must agree", a.Capabilities(), claudecode.Capabilities())
	}
}
