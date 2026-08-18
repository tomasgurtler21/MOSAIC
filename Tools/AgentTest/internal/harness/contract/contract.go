// Package contract is the reusable adapter conformance suite for
// domain.HarnessAdapter. It is written once and driven against an adapter
// supplied through Config — never copied per adapter — so this suite is the
// executable specification of the port: any obligation an adapter must meet
// is asserted here, and any port method this suite cannot exercise is
// either unnecessary or under-specified.
//
// Every implementation of domain.HarnessAdapter, including the scripted
// internal/harness/fake adapter, is required to pass Run unchanged.
package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
)

// Config is what an adapter package supplies to run the suite against
// itself.
type Config struct {
	// Name identifies the adapter in test output.
	Name string

	// New constructs a fresh adapter bound to a fresh sandbox under dir.
	// Called once per subtest so no state leaks between obligations.
	New func(t *testing.T, dir string) domain.HarnessAdapter

	// NativePre and NativePost synthesise native interception payloads for a
	// given collaborator identity and correlation token, in the adapter's
	// own wire shape. This is the only place a test may speak the harness's
	// native language.
	NativePre  func(id domain.CollaboratorIdentity, msg domain.TaskMessage, token string) []byte
	NativePost func(id domain.CollaboratorIdentity, token string, observed string) []byte

	// NativeCompletion synthesises a native completion-signal payload (see
	// domain.PhaseCompletion) carrying observed as what the collaborator
	// actually said. It exists so the capability-honesty check can drive a
	// real completion payload through TranslateCall(PhaseCompletion, ...)
	// rather than accepting the declaration on faith: an adapter whose
	// post-invocation point fires at launch could otherwise declare
	// SupportsReplyRecovery honestly in letter while the suite never
	// actually exercised the recovery path, exactly the gap that made
	// SupportsPostInterception's own declaration true in letter and false in
	// spirit before this mechanism existed.
	//
	// Nil for an adapter that declares SupportsReplyRecovery: false — it
	// makes no reply-recovery claim for this check to hold honest, so the
	// check is skipped for it rather than failing on an absent seam.
	NativeCompletion func(observed string) []byte

	// Observe interprets what the adapter emitted back to its harness and
	// reports the effect actually achieved. This is what makes the
	// capability-honesty test possible: the suite compares the observed
	// effect against Capabilities(), so an adapter claiming direct
	// substitution while only achieving prompt rewriting fails its own
	// contract rather than silently producing unfaithful stubs.
	Observe func(t *testing.T, native []byte) ObservedEffect

	// Subject describes a subject the adapter can produce a spawn plan for.
	Subject domain.SubjectUnderTest

	// CompetingRewriteRequest, if set, builds a domain.ProvisionRequest that
	// this adapter recognises as containing more than one entry that would
	// rewrite the intercepted call's input — however that condition is
	// expressed for this adapter (e.g. more than one stub collaborator
	// registering a rewriting hook). Provision on the returned request MUST
	// return an error, per HarnessAdapter.Provision's doc comment. A Config
	// that leaves this nil documents that this adapter has not yet wired a
	// seam for simulating the condition; the corresponding subtest is
	// skipped (never silently passed) with the gap named in the skip
	// reason.
	CompetingRewriteRequest func(sb domain.Sandbox, subject domain.SubjectUnderTest) domain.ProvisionRequest
}

// ObservedEffect is what a Config.Observe call reports about a native
// payload the adapter emitted.
type ObservedEffect struct {
	Kind             domain.OutcomeKind
	SubstitutedBody  string // the response the harness would record, if any
	RewrittenPrompt  string // the prompt the collaborator would receive, if any
	CorrelationToken string
}

// Run drives the full conformance suite against the adapter cfg describes.
// Every adapter — including the scripted one — must pass it. The suite
// covers:
//   - spawn planning produces an executable plan for the configured subject,
//     and no process is started while planning it
//   - declared capabilities are constant and configuration scopes are
//     enumerated and inspected without error
//   - provisioning installs into the sandbox and deprovisioning removes it
//   - provisioning refuses rather than proceeds when the composed
//     configuration would contain more than one entry that rewrites the
//     intercepted call's input (where the adapter has wired a seam to
//     simulate that condition)
//   - translation converts a native payload to a normalized call and an
//     outcome back to native
//   - declared capabilities match observed effects (capability honesty)
//   - the correlation token survives the pre-to-post round trip, keys the
//     records together, and carries no test-revealing content
//   - translation round-trips preserve the outcome's meaning
//   - a malformed or unrecognised native payload is a handleable error,
//     never a panic
//   - deprovision removes every file provision installed
//   - the environment check answers for every adapter and is honest about
//     what it actually resolves (CheckEnvironment)
func Run(t *testing.T, cfg Config) {
	t.Helper()

	t.Run("SpawnPlan", func(t *testing.T) { testSpawnPlan(t, cfg) })
	t.Run("CapabilitiesDeclared", func(t *testing.T) { testCapabilitiesDeclared(t, cfg) })
	t.Run("ConfigScopeInspection", func(t *testing.T) { testConfigScopeInspection(t, cfg) })
	t.Run("ProvisionDeprovision", func(t *testing.T) { testProvisionDeprovision(t, cfg) })
	t.Run("ProvisionRefusesCompetingRewriteHooks", func(t *testing.T) { testProvisionRefusesCompetingRewriteHooks(t, cfg) })
	t.Run("ProvisionCoexistsWithPrerenderedFiles", func(t *testing.T) { testProvisionCoexistsWithPrerenderedFiles(t, cfg) })
	t.Run("TranslateBasics", func(t *testing.T) { testTranslateBasics(t, cfg) })
	t.Run("CapabilityHonesty", func(t *testing.T) { testCapabilityHonesty(t, cfg) })
	t.Run("Correlation", func(t *testing.T) { testCorrelation(t, cfg) })
	t.Run("TranslationRoundTrip", func(t *testing.T) { testTranslationRoundTrip(t, cfg) })
	t.Run("MalformedNativePayload", func(t *testing.T) { testMalformedNativePayload(t, cfg) })
	t.Run("Teardown", func(t *testing.T) { testTeardown(t, cfg) })
	t.Run("EnvironmentCheck", func(t *testing.T) { testEnvironmentCheck(t, cfg) })
}

// newSandbox builds a domain.Sandbox rooted at dir, creating the subject and
// control directories the adapter is allowed to write into.
func newSandbox(t *testing.T, dir string) domain.Sandbox {
	t.Helper()

	sb := domain.Sandbox{
		Key:        domain.RunKey{RunID: "contract-run", TestID: "contract-test", RunNumber: 1},
		Root:       dir,
		SubjectDir: filepath.Join(dir, "subject"),
		ControlDir: filepath.Join(dir, "control"),
	}
	if err := os.MkdirAll(sb.SubjectDir, 0o755); err != nil {
		t.Fatalf("contract: create subject dir: %v", err)
	}
	if err := os.MkdirAll(sb.ControlDir, 0o755); err != nil {
		t.Fatalf("contract: create control dir: %v", err)
	}
	return sb
}

// buildProvisionRequest assembles the minimal request every adapter needs to
// provision a fresh sandbox for the suite's purposes.
func buildProvisionRequest(sb domain.Sandbox, subject domain.SubjectUnderTest) domain.ProvisionRequest {
	return domain.ProvisionRequest{
		Sandbox:         sb,
		Subject:         subject,
		LoggerBundleDir: "",
		InterpreterCmd:  "",
		InterceptorPath: "interceptor",
		InterceptorArgs: []string{"intercept"},
	}
}

// provisionForTest provisions a fresh adapter and sandbox, registering
// cleanup so deprovision always runs even if the calling subtest fails
// partway through.
func provisionForTest(t *testing.T, cfg Config) (domain.HarnessAdapter, domain.Sandbox, domain.Provisioning) {
	t.Helper()

	dir := t.TempDir()
	adapter := cfg.New(t, dir)
	sb := newSandbox(t, dir)

	prov, err := adapter.Provision(testContext(), buildProvisionRequest(sb, cfg.Subject))
	if err != nil {
		t.Fatalf("contract: Provision: %v", err)
	}
	t.Cleanup(func() {
		_ = adapter.Deprovision(testContext(), prov)
	})

	return adapter, sb, prov
}

// mustMarshal is a small helper for building stub-response payloads in test
// bodies below.
func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("contract: marshal %#v: %v", v, err)
	}
	return b
}
