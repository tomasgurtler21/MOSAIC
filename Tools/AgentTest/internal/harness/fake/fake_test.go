package fake_test

// These tests specify internal/harness/fake's own scripted behaviour: how
// it consumes Script entries per collaborator, how it reacts to an
// exhausted script or a malformed scripted payload, what it records for
// later assertion, and that both capability settings are selectable. The
// conformance obligations every adapter shares (including this one) are
// covered separately by driving internal/harness/contract.Run against this
// package in conformance_test.go.
//
// Native pre/post payload shape (this package's own wire format — no real
// harness ever sees it):
//
//	{"phase":"pre"|"post","tool":"...","agent":"...",
//	 "agent_instance_id":"...","task_description":"...","token":"..."}

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/fake"
)

type wireCall struct {
	Phase           string `json:"phase"`
	Tool            string `json:"tool"`
	Agent           string `json:"agent"`
	AgentInstanceID string `json:"agent_instance_id"`
	TaskDescription string `json:"task_description"`
	Token           string `json:"token"`
}

func nativePre(id domain.CollaboratorIdentity, msg domain.TaskMessage, token string) []byte {
	b, _ := json.Marshal(wireCall{
		Phase: "pre", Tool: id.ToolName, Agent: id.AgentIdentity,
		AgentInstanceID: msg.AgentInstanceID, TaskDescription: msg.TaskDescription, Token: token,
	})
	return b
}

func nativePost(id domain.CollaboratorIdentity, token string) []byte {
	b, _ := json.Marshal(wireCall{Phase: "post", Tool: id.ToolName, Agent: id.AgentIdentity, Token: token})
	return b
}

var workerID = domain.CollaboratorIdentity{ToolName: domain.DispatchToolName, AgentIdentity: "Worker"}
var workerMsg = domain.TaskMessage{AgentInstanceID: "Worker#1", TaskDescription: "do the work"}
var researcherID = domain.CollaboratorIdentity{ToolName: domain.DispatchToolName, AgentIdentity: "Researcher"}

// fakeSandbox builds a minimal domain.Sandbox rooted at dir, for tests that
// need to provision the fake adapter directly rather than through
// internal/harness/contract.
func fakeSandbox(t *testing.T, dir string) domain.Sandbox {
	t.Helper()
	return domain.Sandbox{
		Key:        domain.RunKey{RunID: "fake-run", TestID: "fake-test", RunNumber: 1},
		Root:       dir,
		SubjectDir: dir + "/subject",
		ControlDir: dir + "/control",
	}
}

func fakeSubject() domain.SubjectUnderTest {
	return domain.SubjectUnderTest{
		Identity:       "orchestrator",
		DefinitionPath: "agents/orchestrator.md",
		OpeningMessage: "begin the run",
		InvocationKind: "agent",
		Model:          "test-model",
	}
}

// subjectTurnWire is this package's own private wire shape for the script a
// scripted stand-in subject replays, carried in the SpawnPlan's Stdin so a
// whole-pipeline run can be driven with no LLM. Exactly one of Invoke or
// Result is set per entry, mirroring fake.SubjectTurn. CollaboratorIdentity
// already carries the right JSON tags ("tool"/"agent"), so it is reused
// directly here rather than duplicated.
type subjectTurnWire struct {
	Invoke *domain.CollaboratorIdentity `json:"invoke,omitempty"`
	Result *wireSubjectResultTurn       `json:"result,omitempty"`
}

type wireSubjectResultTurn struct {
	ProtocolMessage string `json:"protocol_message"`
	Disposition     string `json:"disposition"`
	ExitCode        int    `json:"exit_code"`
}

func TestTranslateCall_PhasePost_ScriptedTurnsConsumedInOrder(t *testing.T) {
	a := fake.New(fake.Options{
		Script: map[string][]fake.Turn{
			workerID.Key(): {
				{Body: "first response"},
				{Body: "second response"},
			},
		},
	})

	first, err := a.TranslateCall(domain.PhasePost, nativePost(workerID, "token-1"))
	if err != nil {
		t.Fatalf("first TranslateCall(PhasePost): %v", err)
	}
	if first.ObservedResponse != "first response" {
		t.Errorf("first ObservedResponse = %q, want %q", first.ObservedResponse, "first response")
	}

	second, err := a.TranslateCall(domain.PhasePost, nativePost(workerID, "token-2"))
	if err != nil {
		t.Fatalf("second TranslateCall(PhasePost): %v", err)
	}
	if second.ObservedResponse != "second response" {
		t.Errorf("second ObservedResponse = %q, want %q", second.ObservedResponse, "second response")
	}
}

func TestTranslateCall_PhasePost_ExhaustedScript_ReturnsError(t *testing.T) {
	a := fake.New(fake.Options{
		Script: map[string][]fake.Turn{
			workerID.Key(): {{Body: "only response"}},
		},
	})

	if _, err := a.TranslateCall(domain.PhasePost, nativePost(workerID, "token-1")); err != nil {
		t.Fatalf("first TranslateCall(PhasePost): unexpected error: %v", err)
	}

	_, err := a.TranslateCall(domain.PhasePost, nativePost(workerID, "token-2"))
	if err == nil {
		t.Fatal("second TranslateCall(PhasePost): expected an error once the script is exhausted, got nil")
	}
}

// TestTranslateCall_PhasePost_NoScriptForIdentity_ReturnsErrorNotPanic verifies
// that when a non-nil script map is provided but has no entry for the called
// collaborator identity, TranslateCall returns an error rather than panicking.
// This covers the "forgot to add this collaborator to the script" mistake.
//
// A nil Script (no script at all) is different: it means post calls are
// unscripted and are allowed with an empty ObservedResponse. Tests that do not
// care about collaborator responses can pass nil to Options.Script.
func TestTranslateCall_PhasePost_NoScriptForIdentity_ReturnsErrorNotPanic(t *testing.T) {
	// Non-nil empty map: a script was provided but this collaborator has no
	// scripted turns. This is the "identity not in script" error case.
	a := fake.New(fake.Options{Script: map[string][]fake.Turn{}})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TranslateCall panicked for an identity with no script: %v", r)
		}
	}()

	_, err := a.TranslateCall(domain.PhasePost, nativePost(workerID, "token-1"))
	if err == nil {
		t.Fatal("expected an error for a collaborator identity with no scripted turns, got nil")
	}
}

func TestTranslateCall_PhasePost_ScriptedErr_ReturnsThatError(t *testing.T) {
	sentinel := errors.New("scripted collaborator failure")
	a := fake.New(fake.Options{
		Script: map[string][]fake.Turn{
			workerID.Key(): {{Err: sentinel}},
		},
	})

	_, err := a.TranslateCall(domain.PhasePost, nativePost(workerID, "token-1"))
	if !errors.Is(err, sentinel) {
		t.Errorf("TranslateCall(PhasePost) error = %v, want it to wrap %v", err, sentinel)
	}
}

func TestTranslateCall_PhasePost_MalformedScriptedRawPayload_ReturnsErrorNotPanic(t *testing.T) {
	a := fake.New(fake.Options{
		Script: map[string][]fake.Turn{
			workerID.Key(): {{Raw: []byte(`{not valid json`)}},
		},
	})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TranslateCall panicked on a malformed scripted Raw payload: %v", r)
		}
	}()

	_, err := a.TranslateCall(domain.PhasePost, nativePost(workerID, "token-1"))
	if err == nil {
		t.Fatal("expected an error for a malformed scripted Raw payload, got nil")
	}
}

func TestTranslateCall_PhasePre_MalformedNativePayload_ReturnsErrorNotPanic(t *testing.T) {
	a := fake.New(fake.Options{})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TranslateCall panicked on a malformed native payload: %v", r)
		}
	}()

	_, err := a.TranslateCall(domain.PhasePre, []byte(`not json {{{`))
	if err == nil {
		t.Fatal("expected an error for a malformed native pre-invocation payload, got nil")
	}
}

func TestTranslateOutcome_RecordsInvocation(t *testing.T) {
	a := fake.New(fake.Options{})

	call, err := a.TranslateCall(domain.PhasePre, nativePre(workerID, workerMsg, "token-1"))
	if err != nil {
		t.Fatalf("TranslateCall(PhasePre): %v", err)
	}

	outcome := domain.InterceptionOutcome{Kind: domain.OutcomePassthrough, CorrelationToken: "token-1"}
	if _, err := a.TranslateOutcome(outcome, call); err != nil {
		t.Fatalf("TranslateOutcome: %v", err)
	}

	got := a.Invocations()
	if len(got) != 1 {
		t.Fatalf("Invocations() has %d entries, want 1", len(got))
	}
	if got[0].Identity != workerID {
		t.Errorf("Invocations()[0].Identity = %+v, want %+v", got[0].Identity, workerID)
	}
	if got[0].Outcome.Kind != domain.OutcomePassthrough {
		t.Errorf("Invocations()[0].Outcome.Kind = %q, want %q", got[0].Outcome.Kind, domain.OutcomePassthrough)
	}
}

func TestRemainingScript_ReportsUnconsumedTurns(t *testing.T) {
	a := fake.New(fake.Options{
		Script: map[string][]fake.Turn{
			workerID.Key(): {{Body: "a"}, {Body: "b"}},
		},
	})

	if got := a.RemainingScript(); got != 2 {
		t.Fatalf("RemainingScript() before consumption = %d, want 2", got)
	}

	if _, err := a.TranslateCall(domain.PhasePost, nativePost(workerID, "token-1")); err != nil {
		t.Fatalf("TranslateCall(PhasePost): %v", err)
	}

	if got := a.RemainingScript(); got != 1 {
		t.Errorf("RemainingScript() after consuming one turn = %d, want 1", got)
	}
}

func TestCapabilities_SubstitutionSetting_IsSelectable(t *testing.T) {
	a := fake.New(fake.Options{Capabilities: domain.HarnessCapabilities{
		SupportsDirectSubstitution: true,
		CorrelationField:           "token",
	}})

	if got := a.Capabilities(); !got.SupportsDirectSubstitution {
		t.Errorf("Capabilities().SupportsDirectSubstitution = false, want true")
	}
}

func TestCapabilities_RewriteOnlySetting_IsSelectable(t *testing.T) {
	a := fake.New(fake.Options{Capabilities: domain.HarnessCapabilities{
		SupportsDirectSubstitution: false,
		CorrelationField:           "token",
	}})

	if got := a.Capabilities(); got.SupportsDirectSubstitution {
		t.Errorf("Capabilities().SupportsDirectSubstitution = true, want false")
	}
}

func TestProvisionDeprovision_RemovesEverythingItInstalled(t *testing.T) {
	dir := t.TempDir()
	a := fake.New(fake.Options{})

	sandbox := domain.Sandbox{
		Key:        domain.RunKey{RunID: "fake-run", TestID: "fake-test", RunNumber: 1},
		Root:       dir,
		SubjectDir: dir + "/subject",
		ControlDir: dir + "/control",
	}

	prov, err := a.Provision(context.Background(), domain.ProvisionRequest{Sandbox: sandbox})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(prov.Files) == 0 && len(prov.Dirs) == 0 {
		t.Fatal("Provision recorded nothing installed")
	}

	if err := a.Deprovision(context.Background(), prov); err != nil {
		t.Fatalf("Deprovision: %v", err)
	}
}

func TestTranslateCall_PhasePost_MalformedNativePayload_ReturnsErrorNotPanic(t *testing.T) {
	// Distinct from TestTranslateCall_PhasePost_MalformedScriptedRawPayload_ReturnsErrorNotPanic:
	// that test exercises a bad *scripted* Raw turn (a bad script entry).
	// This one exercises a bad *native wire* payload arriving at PhasePost,
	// which — per fake.go's own doc comment — is the more complex decoding
	// path because it also consumes the per-identity Script queue.
	a := fake.New(fake.Options{
		Script: map[string][]fake.Turn{
			workerID.Key(): {{Body: "irrelevant"}},
		},
	})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TranslateCall panicked on a malformed native post-invocation payload: %v", r)
		}
	}()

	_, err := a.TranslateCall(domain.PhasePost, []byte(`not json {{{`))
	if err == nil {
		t.Fatal("expected an error for a malformed native post-invocation payload, got nil")
	}
}

// TestTranslateCall_PhasePost_ScriptConsumptionIsIndependentPerIdentity
// exercises the reason Script is keyed per collaborator identity: two
// distinct identities' queues are consumed independently, interleaved, and
// exhaustion of one must not affect the other.
func TestTranslateCall_PhasePost_ScriptConsumptionIsIndependentPerIdentity(t *testing.T) {
	a := fake.New(fake.Options{
		Script: map[string][]fake.Turn{
			workerID.Key():     {{Body: "worker first"}, {Body: "worker second"}},
			researcherID.Key(): {{Body: "researcher first"}},
		},
	})

	wCall1, err := a.TranslateCall(domain.PhasePost, nativePost(workerID, "w1"))
	if err != nil {
		t.Fatalf("worker turn 1: unexpected error: %v", err)
	}
	if wCall1.ObservedResponse != "worker first" {
		t.Errorf("worker turn 1 ObservedResponse = %q, want %q", wCall1.ObservedResponse, "worker first")
	}

	rCall1, err := a.TranslateCall(domain.PhasePost, nativePost(researcherID, "r1"))
	if err != nil {
		t.Fatalf("researcher turn 1: unexpected error: %v", err)
	}
	if rCall1.ObservedResponse != "researcher first" {
		t.Errorf("researcher turn 1 ObservedResponse = %q, want %q", rCall1.ObservedResponse, "researcher first")
	}

	wCall2, err := a.TranslateCall(domain.PhasePost, nativePost(workerID, "w2"))
	if err != nil {
		t.Fatalf("worker turn 2: unexpected error: %v", err)
	}
	if wCall2.ObservedResponse != "worker second" {
		t.Errorf("worker turn 2 ObservedResponse = %q, want %q", wCall2.ObservedResponse, "worker second")
	}

	if _, err := a.TranslateCall(domain.PhasePost, nativePost(researcherID, "r2")); err == nil {
		t.Error("researcher turn 2: expected an error once the researcher's own script is exhausted, got nil")
	}
	if _, err := a.TranslateCall(domain.PhasePost, nativePost(workerID, "w3")); err == nil {
		t.Error("worker turn 3: expected an error once the worker's own script is exhausted, got nil")
	}
}

// TestSpawnPlan_SubjectTurns_EncodedInStdinForScriptedStandIn is the fake
// package's own specification of Options.SubjectTurns: this is the
// mechanism fake.go's doc comment says "lets a whole-pipeline run be driven
// through this adapter with no LLM", and it was entirely untested. The plan
// must name a scripted stand-in and carry the configured turns where that
// stand-in can read and replay them.
func TestSpawnPlan_SubjectTurns_EncodedInStdinForScriptedStandIn(t *testing.T) {
	turns := []fake.SubjectTurn{
		{Invoke: &workerID},
		{Result: &domain.SubjectResult{
			ProtocolMessage: `{"status_code":"SUCCESS"}`,
			Disposition:     domain.DispositionCompleted,
		}},
	}
	a := fake.New(fake.Options{SubjectTurns: turns})

	dir := t.TempDir()
	prov, err := a.Provision(context.Background(), domain.ProvisionRequest{Sandbox: fakeSandbox(t, dir)})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	plan, err := a.SpawnPlan(context.Background(), fakeSubject(), prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	if plan.Executable == "" {
		t.Fatal("SpawnPlan: Executable is empty; the plan must name a scripted stand-in, per this package's doc comment")
	}
	if len(plan.Stdin) == 0 {
		t.Fatal("SpawnPlan: Stdin is empty; the scripted stand-in has no script to replay Options.SubjectTurns from")
	}

	var decoded []subjectTurnWire
	if err := json.Unmarshal(plan.Stdin, &decoded); err != nil {
		t.Fatalf("SpawnPlan: Stdin does not decode as the scripted subject-turn script: %v", err)
	}
	if len(decoded) != len(turns) {
		t.Fatalf("SpawnPlan: Stdin script has %d turns, want %d", len(decoded), len(turns))
	}

	if decoded[0].Invoke == nil {
		t.Fatalf("SpawnPlan: turn 0 = %+v, want an Invoke turn for %+v", decoded[0], workerID)
	}
	if *decoded[0].Invoke != workerID {
		t.Errorf("SpawnPlan: turn 0 Invoke = %+v, want %+v", *decoded[0].Invoke, workerID)
	}

	if decoded[1].Result == nil {
		t.Fatalf("SpawnPlan: turn 1 = %+v, want a Result turn", decoded[1])
	}
	if decoded[1].Result.Disposition != string(domain.DispositionCompleted) {
		t.Errorf("SpawnPlan: turn 1 Result.Disposition = %q, want %q", decoded[1].Result.Disposition, domain.DispositionCompleted)
	}
	if decoded[1].Result.ProtocolMessage != `{"status_code":"SUCCESS"}` {
		t.Errorf("SpawnPlan: turn 1 Result.ProtocolMessage = %q, want %q", decoded[1].Result.ProtocolMessage, `{"status_code":"SUCCESS"}`)
	}
}

// TestSpawnPlan_NoSubjectTurns_StdinCarriesEmptyScript pins the edge case
// alongside the populated one above: an adapter configured with no
// SubjectTurns must still produce a decodable, empty script rather than an
// undecodable or nil Stdin.
func TestSpawnPlan_NoSubjectTurns_StdinCarriesEmptyScript(t *testing.T) {
	a := fake.New(fake.Options{})

	dir := t.TempDir()
	prov, err := a.Provision(context.Background(), domain.ProvisionRequest{Sandbox: fakeSandbox(t, dir)})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	plan, err := a.SpawnPlan(context.Background(), fakeSubject(), prov)
	if err != nil {
		t.Fatalf("SpawnPlan: %v", err)
	}

	var decoded []subjectTurnWire
	if err := json.Unmarshal(plan.Stdin, &decoded); err != nil {
		t.Fatalf("SpawnPlan: Stdin does not decode as the scripted subject-turn script: %v", err)
	}
	if len(decoded) != 0 {
		t.Errorf("SpawnPlan: Stdin script has %d turns for an empty Options.SubjectTurns, want 0", len(decoded))
	}
}
