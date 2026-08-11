package contract_test

// These tests validate the conformance suite itself (internal/harness/contract),
// not any real harness adapter. They are the suite's own proof that it does
// what it claims: a truthful double passes every obligation, and a
// deliberately dishonest double is caught by the capability-honesty check
// (AC9.3). Real adapters (internal/harness/fake, internal/harness/claudecode,
// internal/harness/opencode) are driven against Run in their own test files.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/contract"
)

// wireCall is the loyalAdapter's native pre/post payload shape.
type wireCall struct {
	Phase           string `json:"phase"`
	Tool            string `json:"tool"`
	Agent           string `json:"agent"`
	AgentInstanceID string `json:"agent_instance_id"`
	TaskDescription string `json:"task_description"`
	Token           string `json:"token"`
	Observed        string `json:"observed,omitempty"`
}

// wireReply is the loyalAdapter's native outcome-reply shape.
type wireReply struct {
	Kind   string          `json:"kind"`
	Body   json.RawMessage `json:"body,omitempty"`
	Prompt string          `json:"prompt,omitempty"`
	Token  string          `json:"token"`
}

// loyalAdapter is a small, fully-working domain.HarnessAdapter double used
// to prove the suite passes a truthful implementation end to end. It writes
// one marker file per Provision call and removes it on Deprovision, and
// truthfully reports whichever effect its capabilities declare.
type loyalAdapter struct {
	caps domain.HarnessCapabilities

	// rewritingScope, when true, makes InspectScopes report one
	// non-isolatable external scope that registers a hook rewriting the
	// intercepted call's input, exercising ScopeFinding.RewritesInput.
	rewritingScope bool
}

func newLoyalAdapter(caps domain.HarnessCapabilities) func(t *testing.T, dir string) domain.HarnessAdapter {
	return func(t *testing.T, dir string) domain.HarnessAdapter {
		return &loyalAdapter{caps: caps}
	}
}

// newLoyalAdapterWithRewritingScope is identical to newLoyalAdapter except
// InspectScopes additionally reports a non-isolatable scope with
// RewritesInput set, so the whole suite (including
// ConfigScopeInspection's RewritesInput/Detail invariant) is exercised
// against a truthful double that actually has such a scope.
func newLoyalAdapterWithRewritingScope(caps domain.HarnessCapabilities) func(t *testing.T, dir string) domain.HarnessAdapter {
	return func(t *testing.T, dir string) domain.HarnessAdapter {
		return &loyalAdapter{caps: caps, rewritingScope: true}
	}
}

func (a *loyalAdapter) ID() string { return "loyal" }

func (a *loyalAdapter) Capabilities() domain.HarnessCapabilities { return a.caps }

func (a *loyalAdapter) ConfigScopes() []domain.ConfigScope {
	scopes := []domain.ConfigScope{
		{Name: "sandbox", InSandbox: true, Isolatable: false},
		{Name: "user", InSandbox: false, Isolatable: true},
	}
	if a.rewritingScope {
		scopes = append(scopes, domain.ConfigScope{Name: "enterprise", InSandbox: false, Isolatable: false})
	}
	return scopes
}

func (a *loyalAdapter) InspectScopes(ctx context.Context) ([]domain.ScopeFinding, error) {
	findings := []domain.ScopeFinding{
		{
			Scope:                    domain.ConfigScope{Name: "user", InSandbox: false, Isolatable: true},
			Neutralized:              true,
			PreservesSubjectFunction: true,
		},
	}
	if a.rewritingScope {
		findings = append(findings, domain.ScopeFinding{
			Scope:         domain.ConfigScope{Name: "enterprise", InSandbox: false, Isolatable: false},
			RewritesInput: true,
			Neutralized:   false,
			Detail:        "the enterprise scope registers a hook that rewrites the intercepted call's input and cannot be neutralized",
		})
	}
	return findings, nil
}

// Provision writes the loyal adapter's marker file, but first honours the
// port's MUST-fail obligation: it refuses when the composed configuration
// would contain more than one entry that rewrites the intercepted call's
// input. For this test double the condition is encoded as more than one
// element in InterceptorArgs — the seam loyalCompetingRewriteRequest triggers
// by passing two args instead of one. Each real adapter uses its own
// registration-model-specific check instead.
func (a *loyalAdapter) Provision(ctx context.Context, req domain.ProvisionRequest) (domain.Provisioning, error) {
	if len(req.InterceptorArgs) > 1 {
		return domain.Provisioning{}, fmt.Errorf("loyal: %d entries in the composed configuration would rewrite the intercepted call's input; refusing rather than proceeding", len(req.InterceptorArgs))
	}

	markerDir := filepath.Join(req.Sandbox.SubjectDir, "loyal-adapter")
	if err := os.MkdirAll(markerDir, 0o755); err != nil {
		return domain.Provisioning{}, fmt.Errorf("loyal: mkdir: %w", err)
	}
	markerFile := filepath.Join(markerDir, "installed.json")
	if err := os.WriteFile(markerFile, []byte(`{"installed":true}`), 0o644); err != nil {
		return domain.Provisioning{}, fmt.Errorf("loyal: write marker: %w", err)
	}
	return domain.Provisioning{
		Sandbox: req.Sandbox,
		Files:   []string{markerFile},
		Dirs:    []string{markerDir},
	}, nil
}

func (a *loyalAdapter) Deprovision(ctx context.Context, p domain.Provisioning) error {
	for _, f := range p.Files {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("loyal: remove %q: %w", f, err)
		}
	}
	for _, d := range p.Dirs {
		if err := os.Remove(d); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("loyal: remove dir %q: %w", d, err)
		}
	}
	return nil
}

func (a *loyalAdapter) SpawnPlan(ctx context.Context, subject domain.SubjectUnderTest, p domain.Provisioning) (domain.SpawnPlan, error) {
	return domain.SpawnPlan{
		Executable: "loyal-subject",
		Args:       []string{subject.OpeningMessage},
		WorkingDir: p.Sandbox.SubjectDir,
	}, nil
}

func (a *loyalAdapter) TranslateCall(phase domain.InterceptionPhase, native []byte) (domain.InterceptedCall, error) {
	var w wireCall
	if err := json.Unmarshal(native, &w); err != nil {
		return domain.InterceptedCall{}, fmt.Errorf("loyal: malformed native payload: %w", err)
	}
	if w.Phase == "" || w.Tool == "" || w.Token == "" {
		return domain.InterceptedCall{}, fmt.Errorf("loyal: unrecognised native payload: %+v", w)
	}
	return domain.InterceptedCall{
		Phase:            phase,
		Identity:         domain.CollaboratorIdentity{ToolName: w.Tool, AgentIdentity: w.Agent},
		Message:          domain.TaskMessage{AgentInstanceID: w.AgentInstanceID, TaskDescription: w.TaskDescription},
		CorrelationToken: w.Token,
		RawPayload:       native,
		Capabilities:     a.caps,
		ObservedResponse: w.Observed,
	}, nil
}

// CheckEnvironment implements domain.HarnessAdapter. The loyal double runs
// no interpreter and reports so honestly (InterpreterApplicable false, an
// empty InterpreterCmd), and finds no problem: it exists to prove the whole
// suite passes a truthful implementation end to end.
func (a *loyalAdapter) CheckEnvironment(ctx context.Context) (domain.EnvironmentReport, error) {
	return domain.EnvironmentReport{}, nil
}

func (a *loyalAdapter) TranslateOutcome(outcome domain.InterceptionOutcome, call domain.InterceptedCall) ([]byte, error) {
	reply := wireReply{Kind: string(outcome.Kind), Token: outcome.CorrelationToken}
	switch outcome.Kind {
	case domain.OutcomeSubstitute:
		reply.Body = outcome.StubResponse
	case domain.OutcomeRewritePrompt:
		reply.Prompt = outcome.RewrittenPrompt
	}
	return json.Marshal(reply)
}

func loyalNativePre(id domain.CollaboratorIdentity, msg domain.TaskMessage, token string) []byte {
	b, _ := json.Marshal(wireCall{
		Phase: "pre", Tool: id.ToolName, Agent: id.AgentIdentity,
		AgentInstanceID: msg.AgentInstanceID, TaskDescription: msg.TaskDescription, Token: token,
	})
	return b
}

func loyalNativePost(id domain.CollaboratorIdentity, token string, observed string) []byte {
	b, _ := json.Marshal(wireCall{
		Phase: "post", Tool: id.ToolName, Agent: id.AgentIdentity, Token: token, Observed: observed,
	})
	return b
}

func loyalObserve(t *testing.T, native []byte) contract.ObservedEffect {
	t.Helper()
	var w wireReply
	if err := json.Unmarshal(native, &w); err != nil {
		t.Fatalf("loyalObserve: malformed reply: %v", err)
	}
	return contract.ObservedEffect{
		Kind:             domain.OutcomeKind(w.Kind),
		SubstitutedBody:  string(w.Body),
		RewrittenPrompt:  w.Prompt,
		CorrelationToken: w.Token,
	}
}

// loyalCompetingRewriteRequest builds a ProvisionRequest that loyalAdapter
// recognises as having competing input-rewrite hooks and refuses. The loyal
// double's trigger for this condition is more than one element in
// InterceptorArgs — two args stand in for two competing configurations in the
// loyal double's registration model. See loyalAdapter.Provision.
func loyalCompetingRewriteRequest(sb domain.Sandbox, subject domain.SubjectUnderTest) domain.ProvisionRequest {
	return domain.ProvisionRequest{
		Sandbox:         sb,
		Subject:         subject,
		InterceptorPath: "interceptor",
		// Two args: the loyal adapter interprets this as two contributions
		// that would rewrite the intercepted call's input and refuses.
		InterceptorArgs: []string{"intercept", "competing-rewriter"},
	}
}

func loyalSubject() domain.SubjectUnderTest {
	return domain.SubjectUnderTest{
		Identity:       "orchestrator",
		DefinitionPath: "agents/orchestrator.md",
		OpeningMessage: "begin the run",
		InvocationKind: "agent",
		Model:          "test-model",
	}
}

func TestRun_LoyalDouble_SubstitutionCapable_PassesWholeSuite(t *testing.T) {
	cfg := contract.Config{
		Name: "loyal-substitute",
		New: newLoyalAdapter(domain.HarnessCapabilities{
			SupportsDirectSubstitution: true,
			SupportsPostInterception:   true,
			CorrelationField:           "token",
		}),
		NativePre:               loyalNativePre,
		NativePost:              loyalNativePost,
		Observe:                 loyalObserve,
		Subject:                 loyalSubject(),
		CompetingRewriteRequest: loyalCompetingRewriteRequest,
	}

	contract.Run(t, cfg)
}

func TestRun_LoyalDouble_RewriteOnly_PassesWholeSuite(t *testing.T) {
	cfg := contract.Config{
		Name: "loyal-rewrite",
		New: newLoyalAdapter(domain.HarnessCapabilities{
			SupportsDirectSubstitution: false,
			SupportsPostInterception:   true,
			CorrelationField:           "token",
		}),
		NativePre:               loyalNativePre,
		NativePost:              loyalNativePost,
		Observe:                 loyalObserve,
		Subject:                 loyalSubject(),
		CompetingRewriteRequest: loyalCompetingRewriteRequest,
	}

	contract.Run(t, cfg)
}

// TestRun_LoyalDouble_RewritingScope_PassesWholeSuite drives the whole suite
// against a loyal double whose InspectScopes reports a real
// RewritesInput/Detail-carrying finding, so
// ConfigScopeInspection's RewritesInput invariant is exercised against a
// double that actually has such a scope, not just against doubles that
// never set the field.
func TestRun_LoyalDouble_RewritingScope_PassesWholeSuite(t *testing.T) {
	cfg := contract.Config{
		Name: "loyal-rewriting-scope",
		New: newLoyalAdapterWithRewritingScope(domain.HarnessCapabilities{
			SupportsDirectSubstitution: true,
			SupportsPostInterception:   true,
			CorrelationField:           "token",
		}),
		NativePre:  loyalNativePre,
		NativePost: loyalNativePost,
		Observe:    loyalObserve,
		Subject:    loyalSubject(),
	}

	contract.Run(t, cfg)
}

// dishonestAdapter declares direct substitution but its TranslateOutcome
// always emits a rewritten-prompt reply regardless of what it is asked for
// — the exact failure mode AC9.3 exists to catch: an adapter whose
// declared capability does not match its observed effect.
type dishonestAdapter struct {
	loyalAdapter
}

func (a *dishonestAdapter) TranslateOutcome(outcome domain.InterceptionOutcome, call domain.InterceptedCall) ([]byte, error) {
	reply := wireReply{
		Kind:   string(domain.OutcomeRewritePrompt),
		Prompt: "a prompt rewrite masquerading as a substitution",
		Token:  outcome.CorrelationToken,
	}
	return json.Marshal(reply)
}

func TestRun_DishonestCapabilityDouble_FailsCapabilityHonestyCheck(t *testing.T) {
	caps := domain.HarnessCapabilities{
		SupportsDirectSubstitution: true, // the lie: only rewriting is ever achieved
		SupportsPostInterception:   true,
		CorrelationField:           "token",
	}
	cfg := contract.Config{
		Name: "dishonest",
		New: func(t *testing.T, dir string) domain.HarnessAdapter {
			return &dishonestAdapter{loyalAdapter{caps: caps}}
		},
		NativePre:  loyalNativePre,
		NativePost: loyalNativePost,
		Observe:    loyalObserve,
		Subject:    loyalSubject(),
	}

	// Calling the check* functions directly, with no t.Run subtest, means a
	// failing verdict here cannot propagate up and mark this test's own
	// *testing.T failed as an unwanted side effect — see the "Chosen fix"
	// notes in Stage-9/PlanProgress.md for why the previous t.Run-based
	// structure made these two tests structurally unpassable.
	if err := contract.CheckCapabilityHonesty(t, cfg); err == nil {
		t.Error("expected the capability-honesty check to fail against a double whose declared capability does not match its observed effect, but it reported no error")
	}
	if err := contract.CheckTranslationRoundTrip(t, cfg); err == nil {
		t.Error("expected the translation-round-trip check to fail against a double whose declared capability does not match its observed effect, but it reported no error")
	}
}

// dishonestUnderclaimingAdapter declares only prompt rewriting but its
// TranslateOutcome always emits a substitution reply regardless of what it
// is asked for — the opposite direction of dishonesty from
// dishonestAdapter: under-claiming capability rather than over-claiming it.
// The port's capability-honesty guarantee must hold symmetrically.
type dishonestUnderclaimingAdapter struct {
	loyalAdapter
}

func (a *dishonestUnderclaimingAdapter) TranslateOutcome(outcome domain.InterceptionOutcome, call domain.InterceptedCall) ([]byte, error) {
	body, _ := json.Marshal(map[string]string{"status_code": "SUCCESS"})
	reply := wireReply{
		Kind:  string(domain.OutcomeSubstitute),
		Body:  body,
		Token: outcome.CorrelationToken,
	}
	return json.Marshal(reply)
}

func TestRun_DishonestUnderclaimingCapabilityDouble_FailsCapabilityHonestyCheck(t *testing.T) {
	caps := domain.HarnessCapabilities{
		SupportsDirectSubstitution: false, // the lie: direct substitution is achieved despite declaring only rewrite
		SupportsPostInterception:   true,
		CorrelationField:           "token",
	}
	cfg := contract.Config{
		Name: "dishonest-underclaiming",
		New: func(t *testing.T, dir string) domain.HarnessAdapter {
			return &dishonestUnderclaimingAdapter{loyalAdapter{caps: caps}}
		},
		NativePre:  loyalNativePre,
		NativePost: loyalNativePost,
		Observe:    loyalObserve,
		Subject:    loyalSubject(),
	}

	// Calling the check* functions directly, with no t.Run subtest, means a
	// failing verdict here cannot propagate up and mark this test's own
	// *testing.T failed as an unwanted side effect — see the "Chosen fix"
	// notes in Stage-9/PlanProgress.md for why the previous t.Run-based
	// structure made these two tests structurally unpassable.
	if err := contract.CheckCapabilityHonesty(t, cfg); err == nil {
		t.Error("expected the capability-honesty check to fail against a double that under-claims its capability (declares no direct substitution but actually achieves it), but it reported no error")
	}
	if err := contract.CheckTranslationRoundTrip(t, cfg); err == nil {
		t.Error("expected the translation-round-trip check to fail against a double that under-claims its capability (declares no direct substitution but actually achieves it), but it reported no error")
	}
}

// brokenFunctionAdapter declares a scope neutralized while reporting that
// the isolation broke the subject's ability to run — the exact failure mode
// AC8.4 exists to catch: an adapter that isolates a scope at the cost of
// the subject's ability to run must fail the neutralization check, not
// pass it.
type brokenFunctionAdapter struct {
	loyalAdapter
}

func (a *brokenFunctionAdapter) InspectScopes(ctx context.Context) ([]domain.ScopeFinding, error) {
	return []domain.ScopeFinding{
		{
			Scope:                    domain.ConfigScope{Name: "user", InSandbox: false, Isolatable: true},
			Neutralized:              true,
			PreservesSubjectFunction: false,
			FunctionDetail:           "relocating the user scope moved authentication material out of reach; the subject can no longer log in",
		},
	}, nil
}

// TestRun_AdapterThatBreaksSubjectWhileClaimingNeutralization_FailsTheNeutralizationCheck
// pins AC8.4: an adapter that reports Neutralized=true for a scope while
// reporting PreservesSubjectFunction=false must fail the conformance
// suite's neutralization check.
func TestRun_AdapterThatBreaksSubjectWhileClaimingNeutralization_FailsTheNeutralizationCheck(t *testing.T) {
	caps := domain.HarnessCapabilities{
		SupportsDirectSubstitution: true,
		SupportsPostInterception:   true,
		CorrelationField:           "token",
	}
	cfg := contract.Config{
		Name: "broken-function",
		New: func(t *testing.T, dir string) domain.HarnessAdapter {
			return &brokenFunctionAdapter{loyalAdapter{caps: caps}}
		},
		NativePre:  loyalNativePre,
		NativePost: loyalNativePost,
		Observe:    loyalObserve,
		Subject:    loyalSubject(),
	}

	// Direct call, no t.Run subtest, for the same reason the dishonest
	// capability doubles above call the check* function directly: a failing
	// verdict here must not mark this test's own *testing.T failed as an
	// unwanted side effect.
	if err := contract.CheckPreservesSubjectFunction(t, cfg); err == nil {
		t.Error("expected the neutralization check to fail against a double that reports Neutralized=true while PreservesSubjectFunction=false, but it reported no error")
	}
}

// TestRun_AdapterThatOnlyInspectsCleanly_PreservesSubjectFunctionByDefault
// pins the companion positive case ContractsDesign.md states explicitly: an
// adapter that neither isolates nor breaks anything reports
// PreservesSubjectFunction=true by not interfering, so the neutralization
// check does not fire for a scope it never neutralized in the first place.
func TestRun_AdapterThatOnlyInspectsCleanly_PreservesSubjectFunctionByDefault(t *testing.T) {
	cfg := contract.Config{
		Name: "loyal-rewrite",
		New: newLoyalAdapter(domain.HarnessCapabilities{
			SupportsDirectSubstitution: false,
			SupportsPostInterception:   true,
			CorrelationField:           "token",
		}),
		NativePre:  loyalNativePre,
		NativePost: loyalNativePost,
		Observe:    loyalObserve,
		Subject:    loyalSubject(),
	}

	if err := contract.CheckPreservesSubjectFunction(t, cfg); err != nil {
		t.Errorf("neutralization check failed against a loyal double that honestly declares PreservesSubjectFunction=true for its neutralized scope: %v", err)
	}
}
