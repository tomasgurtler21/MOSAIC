package claudecode_test

// Tests derived from live payload captures against harness version 2.1.240.
// Fixture: testdata/live_payloads_claudecode_2.1.240.jsonl, derived from
// Tools/AgentTest/dist/hook-payload-capture-20260822-claudecode-2.1.240.jsonl
// by stripping personal filesystem paths and taking one representative entry
// per hook type.
//
// Four distinct hook events were observed:
//
//	PreToolUse    — fires at the pre-invocation interception point
//	PostToolUse   — fires at the post-invocation interception point
//	SubagentStart — fires after a dispatched collaborator is launched
//	SubagentStop  — fires when a dispatched collaborator finishes (the completion event)
//
// Key live observation, basis for this stage: SubagentStop carries NO
// tool_use_id. The field is absent from the payload entirely — not merely
// empty — on every observed firing against harness version 2.1.240, under
// both the default and a relocated configuration home. The only field that
// distinguishes one dispatch's SubagentStop from another's is agent_id.
//
// This contradicts the adapter's current documentation and capability
// declaration, which assert that SubagentStop carries tool_use_id. The tests
// below expose that mismatch.

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

// liveCaptureEntry is one line of the package-local fixture derived from the
// raw capture. Each entry represents one observed hook event type.
type liveCaptureEntry struct {
	HarnessVersion string          `json:"harness_version"`
	Hook           string          `json:"hook"`
	PayloadKeys    []string        `json:"payload_keys"`
	Payload        json.RawMessage `json:"payload"`
}

// loadLiveCapture reads every entry from the package-local fixture file.
func loadLiveCapture(t *testing.T) []liveCaptureEntry {
	t.Helper()
	f, err := os.Open("testdata/live_payloads_claudecode_2.1.240.jsonl")
	if err != nil {
		t.Fatalf("loadLiveCapture: open fixture: %v", err)
	}
	defer f.Close()

	var entries []liveCaptureEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e liveCaptureEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("loadLiveCapture: parse fixture line: %v", err)
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("loadLiveCapture: scan fixture: %v", err)
	}
	return entries
}

// liveCaptureEntryFor returns the entry for the named hook, failing the test
// if no such entry exists in the fixture.
func liveCaptureEntryFor(t *testing.T, entries []liveCaptureEntry, hook string) liveCaptureEntry {
	t.Helper()
	for _, e := range entries {
		if e.Hook == hook {
			return e
		}
	}
	t.Fatalf("live capture fixture has no entry for hook %q; available hooks: %v",
		hook, liveCaptureHooks(entries))
	return liveCaptureEntry{}
}

func liveCaptureHooks(entries []liveCaptureEntry) []string {
	hooks := make([]string, len(entries))
	for i, e := range entries {
		hooks[i] = e.Hook
	}
	return hooks
}

// jsonFieldNamesOf returns the JSON field names declared by the struct type of
// v. Fields with tag "-" and fields with no json tag at all are excluded.
// The omitempty suffix is stripped; only the field name is returned.
func jsonFieldNamesOf(v interface{}) []string {
	t := reflect.TypeOf(v)
	var names []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name == "" || name == "-" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// sortedCopy returns a sorted copy of ss, leaving the original unmodified.
func sortedCopy(ss []string) []string {
	cp := make([]string, len(ss))
	copy(cp, ss)
	sort.Strings(cp)
	return cp
}

// ---------------------------------------------------------------------------
// T3.2 — Fixture-driven decode tests
//
// For each captured hook event: assert that every field the adapter's payload
// struct declares is actually present in the live-captured payload. A struct
// field absent from the real harness payload cannot be read in production.
// ---------------------------------------------------------------------------

// TestLiveCapture_PreToolUse_PayloadStruct_AllFieldsPresentInCapture asserts
// that every JSON field declared by PreToolUsePayload is present in the
// live-captured PreToolUse payload. All fields are expected to be present:
// PreToolUse carries tool_use_id on every observed firing.
func TestLiveCapture_PreToolUse_PayloadStruct_AllFieldsPresentInCapture(t *testing.T) {
	entries := loadLiveCapture(t)
	entry := liveCaptureEntryFor(t, entries, "PreToolUse")

	payloadKeySet := make(map[string]bool)
	for _, k := range entry.PayloadKeys {
		payloadKeySet[k] = true
	}

	for _, jsonName := range jsonFieldNamesOf(claudecode.PreToolUsePayload{}) {
		if !payloadKeySet[jsonName] {
			t.Errorf(
				"PreToolUsePayload declares json field %q but it is absent from "+
					"live-captured PreToolUse payload (harness %s); "+
					"observed PreToolUse keys: %v",
				jsonName, entry.HarnessVersion, entry.PayloadKeys,
			)
		}
	}
}

// TestLiveCapture_PostToolUse_PayloadStruct_AllFieldsPresentInCapture asserts
// that every JSON field declared by PostToolUsePayload is present in the
// live-captured PostToolUse payload. PostToolUse carries tool_use_id, and
// all struct fields are expected to be present.
func TestLiveCapture_PostToolUse_PayloadStruct_AllFieldsPresentInCapture(t *testing.T) {
	entries := loadLiveCapture(t)
	entry := liveCaptureEntryFor(t, entries, "PostToolUse")

	payloadKeySet := make(map[string]bool)
	for _, k := range entry.PayloadKeys {
		payloadKeySet[k] = true
	}

	for _, jsonName := range jsonFieldNamesOf(claudecode.PostToolUsePayload{}) {
		if !payloadKeySet[jsonName] {
			t.Errorf(
				"PostToolUsePayload declares json field %q but it is absent from "+
					"live-captured PostToolUse payload (harness %s); "+
					"observed PostToolUse keys: %v",
				jsonName, entry.HarnessVersion, entry.PayloadKeys,
			)
		}
	}
}

// TestLiveCapture_SubagentStart_PayloadStruct_AllFieldsPresentInCapture asserts
// that every JSON field declared by AgentStartPayload is present in the
// live-captured SubagentStart payload. All fields are expected to be present:
// SubagentStart carries hook_event_name, session_id, agent_id, and agent_type
// on every observed firing.
//
// This test is in the TDD RED phase: it will fail to compile until
// AgentStartPayload is declared in native.go. Once the struct is declared with
// the correct fields, the build succeeds and this test passes. If the struct
// is declared with an additional field absent from the live fixture (for
// example, tool_use_id, which is absent from every observed SubagentStart
// payload), this test will catch the mismatch at runtime and name the
// offending field.
func TestLiveCapture_SubagentStart_PayloadStruct_AllFieldsPresentInCapture(t *testing.T) {
	entries := loadLiveCapture(t)
	entry := liveCaptureEntryFor(t, entries, "SubagentStart")

	payloadKeySet := make(map[string]bool)
	for _, k := range entry.PayloadKeys {
		payloadKeySet[k] = true
	}

	for _, jsonName := range jsonFieldNamesOf(claudecode.AgentStartPayload{}) {
		if !payloadKeySet[jsonName] {
			t.Errorf(
				"AgentStartPayload declares json field %q but it is absent from "+
					"live-captured SubagentStart payload (harness %s); "+
					"the adapter cannot read a field that the harness never sends; "+
					"observed SubagentStart keys: %v",
				jsonName, entry.HarnessVersion, entry.PayloadKeys,
			)
		}
	}
}

// TestLiveCapture_SubagentStop_CompletionPayloadStruct_AllFieldsPresentInCapture
// asserts that every JSON field declared by CompletionPayload is present in
// the live-captured SubagentStop payload.
//
// This test is in the TDD RED phase: it currently FAILS because CompletionPayload
// declares ToolUseID with json tag "tool_use_id,omitempty", but the live-captured
// SubagentStop payload does not carry tool_use_id on any observed firing against
// harness version 2.1.240. The field is absent from the payload entirely — not
// merely empty.
//
// The correct fix (I3.1) removes ToolUseID from CompletionPayload and updates
// its documentation to reflect that agent_id is the only dispatch identifier
// available on SubagentStop.
func TestLiveCapture_SubagentStop_CompletionPayloadStruct_AllFieldsPresentInCapture(t *testing.T) {
	entries := loadLiveCapture(t)
	entry := liveCaptureEntryFor(t, entries, "SubagentStop")

	payloadKeySet := make(map[string]bool)
	for _, k := range entry.PayloadKeys {
		payloadKeySet[k] = true
	}

	for _, jsonName := range jsonFieldNamesOf(claudecode.CompletionPayload{}) {
		if !payloadKeySet[jsonName] {
			t.Errorf(
				"CompletionPayload declares json field %q but it is absent from "+
					"live-captured SubagentStop payload (harness %s); "+
					"the adapter cannot read a field that the harness never sends; "+
					"observed SubagentStop keys: %v",
				jsonName, entry.HarnessVersion, entry.PayloadKeys,
			)
		}
	}
}

// TestLiveCapture_PreToolUse_DecodeSucceeds_ProducesDispatchIdentity decodes
// the live-captured PreToolUse payload through TranslateCall and asserts the
// normalized dispatch identity is populated.
func TestLiveCapture_PreToolUse_DecodeSucceeds_ProducesDispatchIdentity(t *testing.T) {
	entries := loadLiveCapture(t)
	entry := liveCaptureEntryFor(t, entries, "PreToolUse")

	a := claudecode.New(claudecode.Options{})
	call, err := a.TranslateCall(domain.PhasePre, entry.Payload)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePre, live PreToolUse payload): %v", err)
	}
	if call.Identity.ToolName != domain.DispatchToolName {
		t.Errorf("TranslateCall(PhasePre): Identity.ToolName = %q, want %q",
			call.Identity.ToolName, domain.DispatchToolName)
	}
	if call.CorrelationToken == "" {
		t.Errorf("TranslateCall(PhasePre): CorrelationToken is empty; " +
			"the live PreToolUse payload carries tool_use_id and the decoded " +
			"call must carry a non-empty correlation token")
	}
}

// TestLiveCapture_PostToolUse_DecodeSucceeds_ProducesObservedResponse decodes
// the live-captured PostToolUse payload through TranslateCall and asserts the
// observed response is populated.
func TestLiveCapture_PostToolUse_DecodeSucceeds_ProducesObservedResponse(t *testing.T) {
	entries := loadLiveCapture(t)
	entry := liveCaptureEntryFor(t, entries, "PostToolUse")

	a := claudecode.New(claudecode.Options{})
	call, err := a.TranslateCall(domain.PhasePost, entry.Payload)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePost, live PostToolUse payload): %v", err)
	}
	if call.ObservedResponse == "" {
		t.Errorf("TranslateCall(PhasePost): ObservedResponse is empty; " +
			"the live PostToolUse payload carries tool_response and the decoded " +
			"call must carry a non-empty observed response")
	}
	if call.CorrelationToken == "" {
		t.Errorf("TranslateCall(PhasePost): CorrelationToken is empty; " +
			"the live PostToolUse payload carries tool_use_id and the decoded " +
			"call must carry a non-empty correlation token")
	}
}

// TestLiveCapture_SubagentStop_DecodeAsCompletion_ProducesObservedResponse
// decodes the live-captured SubagentStop payload through TranslateCall and
// asserts that the collaborator's reply (last_assistant_message) is captured
// as ObservedResponse, that AgentID is populated from agent_id, and that
// CorrelationToken is left empty — the complete translation contract for
// PhaseCompletion stated in the design.
//
// The AgentID assertion uses reflection because InterceptedCall.AgentID is
// added in a subsequent implementation stage. The test compiles today and
// fails at runtime with a clear message until the field is added and populated.
func TestLiveCapture_SubagentStop_DecodeAsCompletion_ProducesObservedResponse(t *testing.T) {
	entries := loadLiveCapture(t)
	entry := liveCaptureEntryFor(t, entries, "SubagentStop")

	a := claudecode.New(claudecode.Options{})
	call, err := a.TranslateCall(domain.PhaseCompletion, entry.Payload)
	if err != nil {
		t.Fatalf("TranslateCall(PhaseCompletion, live SubagentStop payload): %v", err)
	}
	if call.ObservedResponse == "" {
		t.Errorf("TranslateCall(PhaseCompletion): ObservedResponse is empty; " +
			"the live SubagentStop payload carries last_assistant_message and " +
			"the decoded call must capture it")
	}

	// AgentID check via reflection: InterceptedCall.AgentID is added in a
	// later implementation stage. Until then this test fails at runtime with a
	// clear message naming the missing field rather than a build failure.
	callVal := reflect.ValueOf(call)
	agentIDField := callVal.FieldByName("AgentID")
	if !agentIDField.IsValid() {
		t.Errorf(
			"TranslateCall(PhaseCompletion): InterceptedCall has no AgentID field; "+
				"the translation contract requires populating AgentID from agent_id "+
				"on the SubagentStop payload, which carries agent_id on every "+
				"observed firing (harness %s)",
			entry.HarnessVersion,
		)
	} else if agentIDField.String() == "" {
		t.Errorf(
			"TranslateCall(PhaseCompletion): AgentID is empty; "+
				"the live SubagentStop payload carries agent_id and the decoded call "+
				"must populate AgentID from it (harness %s); "+
				"observed SubagentStop keys: %v",
			entry.HarnessVersion, entry.PayloadKeys,
		)
	}

	if call.CorrelationToken != "" {
		t.Errorf(
			"TranslateCall(PhaseCompletion): CorrelationToken = %q, want empty; "+
				"SubagentStop carries no dispatch identifier (tool_use_id is absent "+
				"from every observed firing against harness %s), so the decoded "+
				"call must leave CorrelationToken empty",
			call.CorrelationToken, entry.HarnessVersion,
		)
	}
}

// ---------------------------------------------------------------------------
// T3.3 — Key set pinning
//
// One test per hook event pins the complete set of JSON keys observed in the
// live capture against harness 2.1.240. A future harness version that adds or
// removes a key fails the matching test rather than being silently absorbed.
// The fixture is labelled with its source version so a test failure identifies
// which version introduced the divergence.
// ---------------------------------------------------------------------------

// TestLiveCapture_PreToolUse_KeySet pins the complete key set observed on
// PreToolUse payloads against harness 2.1.240.
func TestLiveCapture_PreToolUse_KeySet(t *testing.T) {
	wantKeys := []string{
		"cwd",
		"effort",
		"hook_event_name",
		"permission_mode",
		"prompt_id",
		"session_id",
		"tool_input",
		"tool_name",
		"tool_use_id",
		"transcript_path",
	}

	entries := loadLiveCapture(t)
	entry := liveCaptureEntryFor(t, entries, "PreToolUse")

	got := sortedCopy(entry.PayloadKeys)
	want := sortedCopy(wantKeys)

	if !reflect.DeepEqual(got, want) {
		t.Errorf(
			"PreToolUse key set mismatch (harness %s):\n  got:  %v\n  want: %v\n"+
				"If the harness version changed, update the fixture and this pin.",
			entry.HarnessVersion, got, want,
		)
	}
}

// TestLiveCapture_PostToolUse_KeySet pins the complete key set observed on
// PostToolUse payloads against harness 2.1.240.
func TestLiveCapture_PostToolUse_KeySet(t *testing.T) {
	wantKeys := []string{
		"cwd",
		"duration_ms",
		"effort",
		"hook_event_name",
		"permission_mode",
		"prompt_id",
		"session_id",
		"tool_input",
		"tool_name",
		"tool_response",
		"tool_use_id",
		"transcript_path",
	}

	entries := loadLiveCapture(t)
	entry := liveCaptureEntryFor(t, entries, "PostToolUse")

	got := sortedCopy(entry.PayloadKeys)
	want := sortedCopy(wantKeys)

	if !reflect.DeepEqual(got, want) {
		t.Errorf(
			"PostToolUse key set mismatch (harness %s):\n  got:  %v\n  want: %v\n"+
				"If the harness version changed, update the fixture and this pin.",
			entry.HarnessVersion, got, want,
		)
	}
}

// TestLiveCapture_SubagentStart_KeySet pins the complete key set observed on
// SubagentStart payloads against harness 2.1.240. SubagentStart does NOT
// carry tool_use_id.
func TestLiveCapture_SubagentStart_KeySet(t *testing.T) {
	wantKeys := []string{
		"agent_id",
		"agent_type",
		"cwd",
		"hook_event_name",
		"prompt_id",
		"session_id",
		"transcript_path",
	}

	entries := loadLiveCapture(t)
	entry := liveCaptureEntryFor(t, entries, "SubagentStart")

	got := sortedCopy(entry.PayloadKeys)
	want := sortedCopy(wantKeys)

	if !reflect.DeepEqual(got, want) {
		t.Errorf(
			"SubagentStart key set mismatch (harness %s):\n  got:  %v\n  want: %v\n"+
				"If the harness version changed, update the fixture and this pin.",
			entry.HarnessVersion, got, want,
		)
	}
}

// TestLiveCapture_SubagentStop_KeySet pins the complete key set observed on
// SubagentStop payloads against harness 2.1.240. SubagentStop does NOT carry
// tool_use_id. agent_id is the only field that identifies which dispatch this
// event belongs to.
func TestLiveCapture_SubagentStop_KeySet(t *testing.T) {
	wantKeys := []string{
		"agent_id",
		"agent_transcript_path",
		"agent_type",
		"background_tasks",
		"cwd",
		"effort",
		"hook_event_name",
		"last_assistant_message",
		"permission_mode",
		"prompt_id",
		"session_crons",
		"session_id",
		"stop_hook_active",
		"transcript_path",
	}

	entries := loadLiveCapture(t)
	entry := liveCaptureEntryFor(t, entries, "SubagentStop")

	got := sortedCopy(entry.PayloadKeys)
	want := sortedCopy(wantKeys)

	if !reflect.DeepEqual(got, want) {
		t.Errorf(
			"SubagentStop key set mismatch (harness %s):\n  got:  %v\n  want: %v\n"+
				"If the harness version changed, update the fixture and this pin.",
			entry.HarnessVersion, got, want,
		)
	}
}

// ---------------------------------------------------------------------------
// T3.4 — Capability-to-payload alignment
//
// The adapter declares how it correlates each interception phase. The
// declared field must be present in the live-captured payload for the event
// that carries it. A declared field absent from the real payload means the
// correlation mechanism cannot work in production.
// ---------------------------------------------------------------------------

// TestLiveCapture_PreAndPost_CorrelationField_PresentInCapture asserts that
// the adapter's declared CorrelationField is present in both the PreToolUse
// and PostToolUse payloads, which is expected and correct for harness 2.1.240.
func TestLiveCapture_PreAndPost_CorrelationField_PresentInCapture(t *testing.T) {
	entries := loadLiveCapture(t)
	caps := claudecode.Capabilities()

	for _, hook := range []string{"PreToolUse", "PostToolUse"} {
		entry := liveCaptureEntryFor(t, entries, hook)
		keySet := make(map[string]bool)
		for _, k := range entry.PayloadKeys {
			keySet[k] = true
		}
		if !keySet[caps.CorrelationField] {
			t.Errorf(
				"%s: Capabilities().CorrelationField = %q is absent from "+
					"live-captured payload (harness %s); observed keys: %v",
				hook, caps.CorrelationField, entry.HarnessVersion, entry.PayloadKeys,
			)
		}
	}
}

// TestLiveCapture_CompletionEvent_CompletionCorrelationField_DeclaredAndPresent
// asserts that HarnessCapabilities declares a CompletionCorrelationField and
// that the declared field is present in the live-captured SubagentStop payload.
//
// This test is in the TDD RED phase: it currently FAILS for two reasons.
//
//  1. HarnessCapabilities does not yet declare a CompletionCorrelationField.
//     The adapter incorrectly claims that CorrelationField ("tool_use_id")
//     is the correlation identifier on the completion event too, but
//     tool_use_id is absent from every observed SubagentStop payload.
//
//  2. Once CompletionCorrelationField is declared as "agent_id" (I3.3),
//     and agent_id IS present in the SubagentStop payload, this test will
//     pass.
//
// The reflection-based check is deliberate: it catches the absence of the
// field from the struct itself, not just a wrong value, so the failure
// message is unambiguous about what needs to be added.
func TestLiveCapture_CompletionEvent_CompletionCorrelationField_DeclaredAndPresent(t *testing.T) {
	entries := loadLiveCapture(t)
	entry := liveCaptureEntryFor(t, entries, "SubagentStop")

	caps := claudecode.Capabilities()
	capsVal := reflect.ValueOf(caps)
	fieldVal := capsVal.FieldByName("CompletionCorrelationField")

	if !fieldVal.IsValid() {
		t.Errorf(
			"HarnessCapabilities does not declare a CompletionCorrelationField; "+
				"the completion event (SubagentStop) does not carry %q "+
				"(the current CorrelationField), so a separate completion "+
				"correlation field naming agent_id must be declared",
			caps.CorrelationField,
		)
		return
	}

	completionField := fieldVal.String()
	if completionField == "" {
		t.Errorf(
			"Capabilities().CompletionCorrelationField is empty; "+
				"SubagentStop does not carry CorrelationField (%q), "+
				"so the completion correlation field must name the actual "+
				"identifier present on that event (agent_id)",
			caps.CorrelationField,
		)
		return
	}

	keySet := make(map[string]bool)
	for _, k := range entry.PayloadKeys {
		keySet[k] = true
	}

	if !keySet[completionField] {
		t.Errorf(
			"Capabilities().CompletionCorrelationField = %q is absent from "+
				"live-captured SubagentStop payload (harness %s); "+
				"observed SubagentStop keys: %v",
			completionField, entry.HarnessVersion, entry.PayloadKeys,
		)
	}
}
