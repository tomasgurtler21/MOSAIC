package app

// harnessonlyrefreshscope_test.go verifies the behaviour of askHarnessOnlyRefreshScope
// and RefreshScope.Regions().
//
// This file is in package app (not package app_test) because askHarnessOnlyRefreshScope
// is a package-private method on *service that cannot be called from the black-box test
// package. The interaction is injected through Deps.Interaction so the tests remain
// boundary-clean: no filesystem access, no real Interaction calls.
//
// Verified invariants:
//
// Presented options (T5.1):
//   - The "protocol-only" per-agent option is offered with no group label.
//   - The "all-deployed" per-agent option is offered with no group label.
//   - The "all:protocol-only" apply-to-all option is offered with group "Apply to all".
//   - The "all:all-deployed" apply-to-all option is offered with group "Apply to all".
//   - Exactly four options are presented — no section-structure reformat option is added.
//   - The question uses the domain.QHarnessOnlyRefreshScope QuestionID.
//   - The question Subject is the agent's TargetPath.
//   - No option carries an ID outside the four expected IDs.
//
// Answer handling (T5.2):
//   - A per-agent "protocol-only" answer returns (RefreshProtocolOnly, applyToAll=false).
//   - A per-agent "all-deployed" answer returns (RefreshAllDeployed, applyToAll=false).
//   - An "all:protocol-only" compound answer returns (RefreshProtocolOnly, applyToAll=true).
//   - An "all:all-deployed" compound answer returns (RefreshAllDeployed, applyToAll=true).
//
// Non-answered outcomes (T5.3):
//   - SkippedOne → (RefreshProtocolOnly, false): the narrow default, not the broad scope.
//   - SkippedAll → (RefreshProtocolOnly, false): same narrow default.
//   - Cancelled  → (RefreshProtocolOnly, false): same narrow default.
//   - Transport error → (RefreshProtocolOnly, false): same narrow default.
//
// Vocabulary-backed region resolution (T5.4):
//   - RefreshProtocolOnly.Regions() returns exactly []string{"CommunicationProtocol"}.
//   - RefreshAllDeployed.Regions() content matches docformat.CanonicalDeployed exactly.
//   - RefreshAllDeployed.Regions() returns a fresh slice each call (mutation-independent).
//   - An unrecognised RefreshScope value defaults to the narrow scope.
//   - The zero value of RefreshScope defaults to the narrow scope.

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Test double: scopePromptCapture
// ---------------------------------------------------------------------------

// scopePromptCall records one SelectOne invocation for option-shape assertions.
type scopePromptCall struct {
	Question domain.ChoiceQuestion
}

// scopePromptCapture is a minimal domain.Interaction fake for the scope-prompt tests.
// It records every SelectOne call so the presented options can be inspected, and returns
// a scripted answer. All other interaction methods are no-ops that return safe defaults.
type scopePromptCapture struct {
	// answer is returned by SelectOne when err is nil.
	answer domain.ChoiceAnswer
	// err is returned as the SelectOne error. When non-nil, answer is ignored.
	err   error
	calls []scopePromptCall
}

func (f *scopePromptCapture) SelectOne(_ context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	f.calls = append(f.calls, scopePromptCall{Question: q})
	if f.err != nil {
		return domain.ChoiceAnswer{}, f.err
	}
	return f.answer, nil
}
func (f *scopePromptCapture) SelectMany(_ context.Context, _ domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	return domain.MultiChoiceAnswer{Status: domain.SkippedOne}, nil
}
func (f *scopePromptCapture) AskText(_ context.Context, _ domain.TextQuestion) (domain.TextAnswer, error) {
	return domain.TextAnswer{Status: domain.SkippedOne}, nil
}
func (f *scopePromptCapture) Confirm(_ context.Context, _ domain.Question) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}
func (f *scopePromptCapture) Notify(_ context.Context, _ domain.Notice)          {}
func (f *scopePromptCapture) Progress(_ context.Context, _ domain.ProgressEvent) {}
func (f *scopePromptCapture) Review(_ context.Context, _ domain.Plan) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}

// newScopePromptService constructs a *service with only the Interaction dependency set.
// askHarnessOnlyRefreshScope only calls s.deps.Interaction.SelectOne, so all other deps
// are left nil; this is intentional and must not cause panics in the function under test.
func newScopePromptService(capture *scopePromptCapture) *service {
	return &service{deps: Deps{Interaction: capture}}
}

// scopePromptAgent returns a minimal HarnessOnlyAgent for use in scope-prompt tests.
func scopePromptAgent(targetPath string) HarnessOnlyAgent {
	return HarnessOnlyAgent{
		TargetPath:       targetPath,
		FileName:         "my-agent.agent.md",
		Key:              "my-agent",
		TransformVersion: "1.9",
		Role:             domain.RoleSubagent,
	}
}

// findScopeOption returns the first option in opts whose ID equals id, and a bool
// indicating whether it was found.
func findScopeOption(opts []domain.Option, id string) (domain.Option, bool) {
	for _, opt := range opts {
		if opt.ID == id {
			return opt, true
		}
	}
	return domain.Option{}, false
}

// assertSelectOneCalled is a helper that fails the test immediately when the fake's
// calls slice is empty, meaning SelectOne was never invoked.
func assertSelectOneCalled(t *testing.T, f *scopePromptCapture) {
	t.Helper()
	if len(f.calls) == 0 {
		t.Fatal("SelectOne was not called; askHarnessOnlyRefreshScope must consult " +
			"s.deps.Interaction.SelectOne to present the scope options")
	}
}

// ---------------------------------------------------------------------------
// T5.1 — Presented options
// ---------------------------------------------------------------------------

// TestAskHarnessOnlyRefreshScope_PresentsProtocolOnlyOption verifies that the narrow
// "CommunicationProtocol only" scope option is offered as a per-agent choice. Its Option.ID
// must be the bare scope value (no "all:" prefix) and it must carry no group label.
func TestAskHarnessOnlyRefreshScope_PresentsProtocolOnlyOption(t *testing.T) {
	capture := &scopePromptCapture{answer: domain.ChoiceAnswer{Status: domain.SkippedOne}}
	svc := newScopePromptService(capture)

	_, _ = svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	assertSelectOneCalled(t, capture)
	opts := capture.calls[0].Question.Options
	opt, ok := findScopeOption(opts, string(RefreshProtocolOnly))
	if !ok {
		t.Errorf("option with ID %q not found in presented options %v; "+
			"the narrow scope option must be offered as a per-agent choice with its bare scope ID",
			RefreshProtocolOnly, opts)
		return
	}
	if opt.Group != "" {
		t.Errorf("per-agent option %q has Group %q, want empty; "+
			"per-agent scope options must have no group label",
			opt.ID, opt.Group)
	}
}

// TestAskHarnessOnlyRefreshScope_PresentsAllDeployedOption verifies that the broad
// "all tool-managed DEPLOYED regions" scope option is offered as a per-agent choice.
// Its Option.ID must be the bare scope value with no group label.
func TestAskHarnessOnlyRefreshScope_PresentsAllDeployedOption(t *testing.T) {
	capture := &scopePromptCapture{answer: domain.ChoiceAnswer{Status: domain.SkippedOne}}
	svc := newScopePromptService(capture)

	_, _ = svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	assertSelectOneCalled(t, capture)
	opts := capture.calls[0].Question.Options
	opt, ok := findScopeOption(opts, string(RefreshAllDeployed))
	if !ok {
		t.Errorf("option with ID %q not found in presented options %v; "+
			"the broad scope option must be offered as a per-agent choice",
			RefreshAllDeployed, opts)
		return
	}
	if opt.Group != "" {
		t.Errorf("per-agent option %q has Group %q, want empty; "+
			"per-agent scope options must have no group label",
			opt.ID, opt.Group)
	}
}

// TestAskHarnessOnlyRefreshScope_PresentsApplyToAllProtocolOnlyOption verifies that the
// apply-to-all variant of the narrow scope is presented. Its Option.ID must carry the
// "all:" prefix (matching askLocalModification) and its Group must be "Apply to all".
func TestAskHarnessOnlyRefreshScope_PresentsApplyToAllProtocolOnlyOption(t *testing.T) {
	capture := &scopePromptCapture{answer: domain.ChoiceAnswer{Status: domain.SkippedOne}}
	svc := newScopePromptService(capture)

	_, _ = svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	assertSelectOneCalled(t, capture)
	opts := capture.calls[0].Question.Options
	compoundID := "all:" + string(RefreshProtocolOnly)
	opt, ok := findScopeOption(opts, compoundID)
	if !ok {
		t.Errorf("option with ID %q not found in presented options %v; "+
			"the apply-to-all variant of the narrow scope must carry the \"all:\" prefix, "+
			"matching the convention established by askLocalModification",
			compoundID, opts)
		return
	}
	if opt.Group != "Apply to all" {
		t.Errorf("apply-to-all option %q has Group %q, want %q; "+
			"apply-to-all scope options must use the \"Apply to all\" group label",
			opt.ID, opt.Group, "Apply to all")
	}
}

// TestAskHarnessOnlyRefreshScope_PresentsApplyToAllAllDeployedOption verifies that the
// apply-to-all variant of the broad scope is presented with the "all:" prefix and the
// "Apply to all" group label.
func TestAskHarnessOnlyRefreshScope_PresentsApplyToAllAllDeployedOption(t *testing.T) {
	capture := &scopePromptCapture{answer: domain.ChoiceAnswer{Status: domain.SkippedOne}}
	svc := newScopePromptService(capture)

	_, _ = svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	assertSelectOneCalled(t, capture)
	opts := capture.calls[0].Question.Options
	compoundID := "all:" + string(RefreshAllDeployed)
	opt, ok := findScopeOption(opts, compoundID)
	if !ok {
		t.Errorf("option with ID %q not found in presented options %v; "+
			"the apply-to-all variant of the broad scope must carry the \"all:\" prefix",
			compoundID, opts)
		return
	}
	if opt.Group != "Apply to all" {
		t.Errorf("apply-to-all option %q has Group %q, want %q",
			opt.ID, opt.Group, "Apply to all")
	}
}

// TestAskHarnessOnlyRefreshScope_ExactlyFourOptions verifies that exactly four options
// are presented: two per-agent (protocol-only, all-deployed) and two apply-to-all
// variants. No additional scope — such as a section-structure reformat — may be offered.
func TestAskHarnessOnlyRefreshScope_ExactlyFourOptions(t *testing.T) {
	capture := &scopePromptCapture{answer: domain.ChoiceAnswer{Status: domain.SkippedOne}}
	svc := newScopePromptService(capture)

	_, _ = svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	assertSelectOneCalled(t, capture)
	opts := capture.calls[0].Question.Options
	if len(opts) != 4 {
		t.Errorf("askHarnessOnlyRefreshScope presented %d option(s), want exactly 4 "+
			"(protocol-only, all-deployed, all:protocol-only, all:all-deployed); "+
			"no section-structure reformat option may be offered — without a generic counterpart "+
			"there is nothing to align section structure against",
			len(opts))
	}
}

// TestAskHarnessOnlyRefreshScope_UsesQHarnessOnlyRefreshScopeID verifies that the
// question uses the domain.QHarnessOnlyRefreshScope QuestionID constant, not a hardcoded
// string literal or a QuestionID borrowed from another prompt.
func TestAskHarnessOnlyRefreshScope_UsesQHarnessOnlyRefreshScopeID(t *testing.T) {
	capture := &scopePromptCapture{answer: domain.ChoiceAnswer{Status: domain.SkippedOne}}
	svc := newScopePromptService(capture)

	_, _ = svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	assertSelectOneCalled(t, capture)
	gotID := capture.calls[0].Question.ID
	if gotID != domain.QHarnessOnlyRefreshScope {
		t.Errorf("question.ID = %q, want %q (domain.QHarnessOnlyRefreshScope); "+
			"the scope prompt must register and use its own QuestionID constant",
			gotID, domain.QHarnessOnlyRefreshScope)
	}
}

// TestAskHarnessOnlyRefreshScope_SubjectIsAgentTargetPath verifies that the question
// Subject is set to the agent's TargetPath so a frontend can identify which file is being
// asked about, matching the pattern set by askLocalModification.
func TestAskHarnessOnlyRefreshScope_SubjectIsAgentTargetPath(t *testing.T) {
	targetPath := ".mosaic/custom-handler.agent.md"
	capture := &scopePromptCapture{answer: domain.ChoiceAnswer{Status: domain.SkippedOne}}
	svc := newScopePromptService(capture)

	_, _ = svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent(targetPath))

	assertSelectOneCalled(t, capture)
	gotSubject := capture.calls[0].Question.Subject
	if gotSubject != targetPath {
		t.Errorf("question.Subject = %q, want %q (agent.TargetPath); "+
			"the scope prompt must identify which agent is being asked about via Subject, "+
			"so a frontend can render context for the user",
			gotSubject, targetPath)
	}
}

// TestAskHarnessOnlyRefreshScope_NoUnexpectedOptionIDs verifies that every presented
// option ID is one of the four expected values. No section-structure reformat option,
// no legacy option ID, and no misspelled variant may appear.
func TestAskHarnessOnlyRefreshScope_NoUnexpectedOptionIDs(t *testing.T) {
	allowedIDs := map[string]bool{
		string(RefreshProtocolOnly):            true,
		string(RefreshAllDeployed):             true,
		"all:" + string(RefreshProtocolOnly):   true,
		"all:" + string(RefreshAllDeployed):    true,
	}
	capture := &scopePromptCapture{answer: domain.ChoiceAnswer{Status: domain.SkippedOne}}
	svc := newScopePromptService(capture)

	_, _ = svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	assertSelectOneCalled(t, capture)
	opts := capture.calls[0].Question.Options
	for _, opt := range opts {
		if !allowedIDs[opt.ID] {
			t.Errorf("unexpected option ID %q presented; only the four expected scope option IDs "+
				"are permitted — no section-structure reformat or other extra scope may be offered",
				opt.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// T5.2 — Answer handling
// ---------------------------------------------------------------------------

// TestAskHarnessOnlyRefreshScope_PerAgentProtocolOnly_ReturnsScopeWithApplyToAllFalse
// verifies that when the user selects the bare "protocol-only" option, the function returns
// RefreshProtocolOnly and applyToAll = false. The "all:" prefix is absent, so this is a
// per-agent decision that does not latch for remaining agents.
func TestAskHarnessOnlyRefreshScope_PerAgentProtocolOnly_ReturnsScopeWithApplyToAllFalse(t *testing.T) {
	capture := &scopePromptCapture{
		answer: domain.ChoiceAnswer{
			Status:   domain.Answered,
			OptionID: string(RefreshProtocolOnly),
		},
	}
	svc := newScopePromptService(capture)

	scope, applyToAll := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if scope != RefreshProtocolOnly {
		t.Errorf("scope = %q, want %q; a per-agent answer with the bare scope ID must return "+
			"the corresponding RefreshScope value",
			scope, RefreshProtocolOnly)
	}
	if applyToAll {
		t.Error("applyToAll = true, want false; a per-agent answer (no \"all:\" prefix) must " +
			"return applyToAll false — only the caller's explicit apply-to-all selection latches " +
			"the decision for remaining agents")
	}
}

// TestAskHarnessOnlyRefreshScope_PerAgentAllDeployed_ReturnsScopeWithApplyToAllFalse
// verifies that a bare "all-deployed" answer returns RefreshAllDeployed with applyToAll = false.
func TestAskHarnessOnlyRefreshScope_PerAgentAllDeployed_ReturnsScopeWithApplyToAllFalse(t *testing.T) {
	capture := &scopePromptCapture{
		answer: domain.ChoiceAnswer{
			Status:   domain.Answered,
			OptionID: string(RefreshAllDeployed),
		},
	}
	svc := newScopePromptService(capture)

	scope, applyToAll := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if scope != RefreshAllDeployed {
		t.Errorf("scope = %q, want %q; a per-agent \"all-deployed\" answer must return RefreshAllDeployed",
			scope, RefreshAllDeployed)
	}
	if applyToAll {
		t.Error("applyToAll = true, want false; a per-agent answer must return applyToAll false")
	}
}

// TestAskHarnessOnlyRefreshScope_ApplyToAllProtocolOnly_ReturnsTrueFlag verifies that
// the "all:protocol-only" compound option strips the prefix, returns RefreshProtocolOnly,
// and returns applyToAll = true so the caller can latch this decision for remaining agents.
func TestAskHarnessOnlyRefreshScope_ApplyToAllProtocolOnly_ReturnsTrueFlag(t *testing.T) {
	capture := &scopePromptCapture{
		answer: domain.ChoiceAnswer{
			Status:   domain.Answered,
			OptionID: "all:" + string(RefreshProtocolOnly),
		},
	}
	svc := newScopePromptService(capture)

	scope, applyToAll := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if scope != RefreshProtocolOnly {
		t.Errorf("scope = %q, want %q; the \"all:\" prefix must be stripped to recover the "+
			"underlying RefreshScope value",
			scope, RefreshProtocolOnly)
	}
	if !applyToAll {
		t.Error("applyToAll = false, want true; an answer with the \"all:\" prefix must return " +
			"applyToAll true so the caller can latch this decision for all remaining harness-only agents")
	}
}

// TestAskHarnessOnlyRefreshScope_ApplyToAllAllDeployed_ReturnsTrueFlag verifies that the
// "all:all-deployed" compound option returns RefreshAllDeployed with applyToAll = true.
func TestAskHarnessOnlyRefreshScope_ApplyToAllAllDeployed_ReturnsTrueFlag(t *testing.T) {
	capture := &scopePromptCapture{
		answer: domain.ChoiceAnswer{
			Status:   domain.Answered,
			OptionID: "all:" + string(RefreshAllDeployed),
		},
	}
	svc := newScopePromptService(capture)

	scope, applyToAll := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if scope != RefreshAllDeployed {
		t.Errorf("scope = %q, want %q; \"all:all-deployed\" must recover RefreshAllDeployed",
			scope, RefreshAllDeployed)
	}
	if !applyToAll {
		t.Error("applyToAll = false, want true; \"all:all-deployed\" must return applyToAll true")
	}
}

// ---------------------------------------------------------------------------
// T5.3 — Non-answered outcomes
// ---------------------------------------------------------------------------

// TestAskHarnessOnlyRefreshScope_SkippedOne_ReturnsProtocolOnlyDefault verifies that a
// SkippedOne outcome — the user skipped this particular question — returns the narrow
// default (RefreshProtocolOnly, false). The broader scope must never be selected without
// an explicit answer.
func TestAskHarnessOnlyRefreshScope_SkippedOne_ReturnsProtocolOnlyDefault(t *testing.T) {
	capture := &scopePromptCapture{
		answer: domain.ChoiceAnswer{Status: domain.SkippedOne},
	}
	svc := newScopePromptService(capture)

	scope, applyToAll := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if scope != RefreshProtocolOnly {
		t.Errorf("scope = %q on SkippedOne, want %q; "+
			"a skipped prompt must return the narrow default, never the broader scope",
			scope, RefreshProtocolOnly)
	}
	if applyToAll {
		t.Error("applyToAll = true on SkippedOne, want false; " +
			"a skipped prompt must not latch the apply-to-all flag for remaining agents")
	}
}

// TestAskHarnessOnlyRefreshScope_SkippedAll_ReturnsProtocolOnlyDefault verifies that a
// SkippedAll outcome also defaults to the narrow scope without escalating.
func TestAskHarnessOnlyRefreshScope_SkippedAll_ReturnsProtocolOnlyDefault(t *testing.T) {
	capture := &scopePromptCapture{
		answer: domain.ChoiceAnswer{Status: domain.SkippedAll},
	}
	svc := newScopePromptService(capture)

	scope, applyToAll := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if scope != RefreshProtocolOnly {
		t.Errorf("scope = %q on SkippedAll, want %q; "+
			"a SkippedAll outcome must also default to the narrow scope",
			scope, RefreshProtocolOnly)
	}
	if applyToAll {
		t.Error("applyToAll = true on SkippedAll, want false")
	}
}

// TestAskHarnessOnlyRefreshScope_Cancelled_ReturnsProtocolOnlyDefault verifies that a
// Cancelled outcome returns the narrow default. Cancellation is a non-answer and must
// never be interpreted as consent to the broader scope.
func TestAskHarnessOnlyRefreshScope_Cancelled_ReturnsProtocolOnlyDefault(t *testing.T) {
	capture := &scopePromptCapture{
		answer: domain.ChoiceAnswer{Status: domain.Cancelled},
	}
	svc := newScopePromptService(capture)

	scope, applyToAll := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if scope != RefreshProtocolOnly {
		t.Errorf("scope = %q on Cancelled, want %q; "+
			"a Cancelled outcome must default to the narrow scope, not the broader one",
			scope, RefreshProtocolOnly)
	}
	if applyToAll {
		t.Error("applyToAll = true on Cancelled, want false")
	}
}

// TestAskHarnessOnlyRefreshScope_TransportError_ReturnsProtocolOnlyDefault verifies that
// when SelectOne returns a non-nil transport error, the function returns the narrow default.
// A transport failure must never silently escalate scope.
func TestAskHarnessOnlyRefreshScope_TransportError_ReturnsProtocolOnlyDefault(t *testing.T) {
	capture := &scopePromptCapture{
		err: errors.New("transport failure: connection reset by peer"),
	}
	svc := newScopePromptService(capture)

	scope, applyToAll := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if scope != RefreshProtocolOnly {
		t.Errorf("scope = %q on transport error, want %q; "+
			"a SelectOne transport error must return the narrow default scope",
			scope, RefreshProtocolOnly)
	}
	if applyToAll {
		t.Error("applyToAll = true on transport error, want false")
	}
}

// ---------------------------------------------------------------------------
// T5.4 — Vocabulary-backed region resolution
// ---------------------------------------------------------------------------

// TestRefreshScope_ProtocolOnly_RegionsReturnsProtocolOnlySlice verifies that
// RefreshProtocolOnly.Regions() returns exactly []string{"CommunicationProtocol"}.
// This is the narrow scope and the safe default.
func TestRefreshScope_ProtocolOnly_RegionsReturnsProtocolOnlySlice(t *testing.T) {
	got := RefreshProtocolOnly.Regions()
	want := []string{"CommunicationProtocol"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RefreshProtocolOnly.Regions() = %v, want %v; "+
			"the narrow scope must cover only CommunicationProtocol",
			got, want)
	}
}

// TestRefreshScope_AllDeployed_RegionsMatchCanonicalDeployed verifies that
// RefreshAllDeployed.Regions() content is equal to docformat.CanonicalDeployed —
// the shared vocabulary — rather than a hardcoded list that could drift.
// This is the core assertion for AC5.5.
func TestRefreshScope_AllDeployed_RegionsMatchCanonicalDeployed(t *testing.T) {
	got := RefreshAllDeployed.Regions()
	if !reflect.DeepEqual(got, docformat.CanonicalDeployed) {
		t.Errorf("RefreshAllDeployed.Regions() = %v; want the same content as "+
			"docformat.CanonicalDeployed = %v; the all-deployed scope must be derived from "+
			"the shared vocabulary (docformat.CanonicalDeployed), not from a duplicated literal list "+
			"that could drift from Tools/OldAgentsTransform/boundary_constants.py:CANONICAL_DEPLOYED",
			got, docformat.CanonicalDeployed)
	}
}

// TestRefreshScope_AllDeployed_RegionsReturnsFreshSlice verifies that successive calls to
// Regions() return independent slices: mutating the result of one call must not affect a
// subsequent call's result or the canonical vocabulary backing it.
func TestRefreshScope_AllDeployed_RegionsReturnsFreshSlice(t *testing.T) {
	first := RefreshAllDeployed.Regions()
	// Mutate the first result (no-op when nil or empty, but safe to attempt).
	for i := range first {
		first[i] = "mutated"
	}
	// A second call must still match the canonical vocabulary.
	second := RefreshAllDeployed.Regions()
	if !reflect.DeepEqual(second, docformat.CanonicalDeployed) {
		t.Errorf("after mutating the slice returned by a first call, "+
			"RefreshAllDeployed.Regions() = %v; want %v; "+
			"Regions() must return a freshly allocated slice so callers cannot corrupt the vocabulary",
			second, docformat.CanonicalDeployed)
	}
}

// TestRefreshScope_UnrecognisedValue_DefaultsToProtocolOnly verifies that any RefreshScope
// value that is neither RefreshProtocolOnly nor RefreshAllDeployed yields the narrow scope.
// Widening the scope always requires an explicit, recognised value.
func TestRefreshScope_UnrecognisedValue_DefaultsToProtocolOnly(t *testing.T) {
	unrecognised := RefreshScope("unknown-scope-value")
	got := unrecognised.Regions()
	want := []string{"CommunicationProtocol"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RefreshScope(%q).Regions() = %v, want %v; "+
			"an unrecognised scope value must default to the narrow CommunicationProtocol scope, "+
			"never to the broader all-deployed scope",
			unrecognised, got, want)
	}
}

// TestRefreshScope_ZeroValue_DefaultsToProtocolOnly verifies that the zero value of
// RefreshScope (the empty string) also yields the narrow scope. A caller that forgets to
// set a scope must not silently receive a broader refresh than intended.
func TestRefreshScope_ZeroValue_DefaultsToProtocolOnly(t *testing.T) {
	var zero RefreshScope
	got := zero.Regions()
	want := []string{"CommunicationProtocol"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RefreshScope(\"\").Regions() = %v, want %v; "+
			"the zero value must default to the narrow scope",
			got, want)
	}
}
