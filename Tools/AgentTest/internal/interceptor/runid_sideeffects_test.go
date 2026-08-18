package interceptor_test

// Tests for run-ID placeholder expansion in stub side-effect paths, the
// post-expansion escape guard, the verbatim-response contract, and the
// best-effort-miss behaviors (failed run-state read and empty RunID).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/fake"
)

// TestRun_SideEffectPath_WithRunIDPlaceholder_WritesFileAtExpandedLocation
// asserts that a side-effect path containing the run-ID placeholder is
// written at the location where the placeholder is replaced by the actual
// run ID, not at the literal placeholder path.
func TestRun_SideEffectPath_WithRunIDPlaceholder_WritesFileAtExpandedLocation(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	const runID = "20260815T111456Z-6b68"
	registry := domain.StubRegistry{
		SchemaVersion: 1,
		TestID:        "stage4-run-id-expansion",
		OnUnmatched:   domain.UnmatchedHalt,
		Stubs: []domain.Stub{{
			Match:    domain.StubMatch{Identity: id, Invocation: 1},
			Response: json.RawMessage(`{"status_code":"SUCCESS"}`),
			SideEffects: []domain.FileEffect{
				{
					Path:    "Orchestration-" + domain.RunIDPlaceholder + "/Plan.md",
					Content: "plan content",
				},
			},
		}},
	}
	h := newHarness(t, baseState(), registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)
	h.Config.RunID = runID

	native := encodePre(t, id, "corr-1", "researcher#1", "do work")
	_, exitCode, _ := run(t, h.Config, domain.PhasePre, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}

	// The file must exist at the expanded path.
	wantPath := filepath.Join(h.SubjectDir, "Orchestration-"+runID, "Plan.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected side-effect file at expanded path %q to exist: %v", wantPath, err)
	}

	// The file must NOT exist at the literal placeholder path.
	literalPath := filepath.Join(h.SubjectDir, "Orchestration-"+domain.RunIDPlaceholder, "Plan.md")
	if _, err := os.Stat(literalPath); err == nil {
		t.Errorf("side-effect file must not exist at the literal placeholder path %q; it must only appear at the expanded location", literalPath)
	}
}

// TestRun_SideEffectPath_MultipleRunIDOccurrences_AllOccurrencesExpand
// asserts that every occurrence of the run-ID placeholder in a single
// path is expanded, not only the first occurrence.
func TestRun_SideEffectPath_MultipleRunIDOccurrences_AllOccurrencesExpand(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	const runID = "20260815T111456Z-6b68"
	// Path with the placeholder appearing twice — one in the directory name,
	// one in the file name.
	pathWithDoubleOccurrence := domain.RunIDPlaceholder + "/nested-" + domain.RunIDPlaceholder + ".md"
	registry := domain.StubRegistry{
		SchemaVersion: 1,
		TestID:        "stage4-multi-occurrence",
		OnUnmatched:   domain.UnmatchedHalt,
		Stubs: []domain.Stub{{
			Match:    domain.StubMatch{Identity: id, Invocation: 1},
			Response: json.RawMessage(`{"status_code":"SUCCESS"}`),
			SideEffects: []domain.FileEffect{
				{Path: pathWithDoubleOccurrence, Content: "double expansion"},
			},
		}},
	}
	h := newHarness(t, baseState(), registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)
	h.Config.RunID = runID

	native := encodePre(t, id, "corr-2", "researcher#1", "do work")
	_, exitCode, _ := run(t, h.Config, domain.PhasePre, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}

	// Both occurrences must have expanded: dir and filename each contain the
	// actual run ID, not the placeholder literal.
	wantPath := filepath.Join(h.SubjectDir, runID, "nested-"+runID+".md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected file at %q — both placeholder occurrences must expand to %q: %v", wantPath, runID, err)
	}
}

// TestRun_SideEffectPath_WithoutRunIDPlaceholder_WrittenUnchanged asserts
// that a path containing no placeholder reaches the filesystem byte-identical
// to what the stub declared — the expansion leaves placeholder-free paths
// untouched.
func TestRun_SideEffectPath_WithoutRunIDPlaceholder_WrittenUnchanged(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	const declaredPath = "Orchestration-static/Plan.md"
	registry := domain.StubRegistry{
		SchemaVersion: 1,
		TestID:        "stage4-no-placeholder",
		OnUnmatched:   domain.UnmatchedHalt,
		Stubs: []domain.Stub{{
			Match:    domain.StubMatch{Identity: id, Invocation: 1},
			Response: json.RawMessage(`{"status_code":"SUCCESS"}`),
			SideEffects: []domain.FileEffect{
				{Path: declaredPath, Content: "static path content"},
			},
		}},
	}
	h := newHarness(t, baseState(), registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)
	h.Config.RunID = "20260815T111456Z-6b68"

	native := encodePre(t, id, "corr-3", "researcher#1", "do work")
	_, exitCode, _ := run(t, h.Config, domain.PhasePre, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}

	// The file must appear at the declared path, unmodified.
	wantPath := filepath.Join(h.SubjectDir, "Orchestration-static", "Plan.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected side-effect file at the unchanged declared path %q: %v", wantPath, err)
	}
}

// TestRun_SideEffectPath_EscapesSubjectDirAfterRunIDExpansion_RefusedButInterceptorStillReplies
// asserts that a path that escapes the subject directory only after run-ID
// expansion is refused — the escape guard operates on the post-expansion
// path, not the pre-expansion literal. It also asserts the interceptor
// still emits a valid reply, because a non-zero exit or a missing reply
// from this process damages the run it is measuring.
func TestRun_SideEffectPath_EscapesSubjectDirAfterRunIDExpansion_RefusedButInterceptorStillReplies(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	// The run ID injects path-traversal segments. After joining with the path
	// template, the result escapes the subject directory.
	const escapingRunID = "../../escaped-run"
	registry := domain.StubRegistry{
		SchemaVersion: 1,
		TestID:        "stage4-escape-after-expansion",
		OnUnmatched:   domain.UnmatchedHalt,
		Stubs: []domain.Stub{{
			Match:    domain.StubMatch{Identity: id, Invocation: 1},
			Response: json.RawMessage(`{"status_code":"SUCCESS"}`),
			SideEffects: []domain.FileEffect{
				{
					// Before expansion: "Orchestration-{run_id}/file.md" — looks safe.
					// After expansion:  "Orchestration-../../escaped-run/file.md" — escapes.
					Path:    "Orchestration-" + domain.RunIDPlaceholder + "/file.md",
					Content: "must not be written outside the subject directory",
				},
			},
		}},
	}
	h := newHarness(t, baseState(), registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)
	h.Config.RunID = escapingRunID

	native := encodePre(t, id, "corr-4", "researcher#1", "do work")
	reply, exitCode, _ := run(t, h.Config, domain.PhasePre, native)

	// The interceptor must always return exit code 0. A non-zero exit is
	// interpreted by the harness as a failed hook, which damages the run.
	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0 — the interceptor must always reply regardless of side-effect failure", exitCode)
	}

	// The reply must be parseable — a missing or malformed reply damages the
	// harness's view of the run.
	if len(reply) == 0 {
		t.Fatal("Run produced no reply; a valid native reply is required even when a side effect is refused")
	}
	var replyObj map[string]interface{}
	if err := json.Unmarshal(reply, &replyObj); err != nil {
		t.Fatalf("reply is not valid JSON: %v (reply: %s)", err, reply)
	}

	// The file must not have been written outside the subject directory.
	parent := filepath.Dir(h.SubjectDir)
	escapedFile := filepath.Join(parent, "escaped-run", "file.md")
	if _, err := os.Stat(escapedFile); err == nil {
		t.Errorf("escape guard must refuse the post-expansion effect — file must not exist at %q", escapedFile)
	}
}

// TestRun_StubResponseContent_IsReturnedVerbatim_WithNoRunIDSubstitution
// asserts that a stub's response body is returned to the harness exactly as
// declared, even when the body contains the run-ID placeholder string.
// Response content belongs to the subject's communication protocol; expanding
// it would corrupt the output the test is measuring and violate AC4.5.
func TestRun_StubResponseContent_IsReturnedVerbatim_WithNoRunIDSubstitution(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	const runID = "20260815T111456Z-6b68"
	// The response body contains the placeholder string at a realistic
	// location an orchestrator might reference in its reply.
	responseWithPlaceholder := json.RawMessage(
		`{"artifact_path":"Orchestration-` + domain.RunIDPlaceholder + `/Design.md"}`,
	)
	registry := domain.StubRegistry{
		SchemaVersion: 1,
		TestID:        "stage4-verbatim-response",
		OnUnmatched:   domain.UnmatchedHalt,
		Stubs: []domain.Stub{{
			Match:    domain.StubMatch{Identity: id, Invocation: 1},
			Response: responseWithPlaceholder,
		}},
	}
	h := newHarness(t, baseState(), registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)
	h.Config.RunID = runID

	native := encodePre(t, id, "corr-5", "researcher#1", "do work")
	reply, exitCode, _ := run(t, h.Config, domain.PhasePre, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}

	got := decodeReply(t, reply)
	// The body must be byte-identical to the declared stub response.
	// No substitution — not even partial — must have been applied.
	wantBody := string(responseWithPlaceholder)
	if string(got.Body) != wantBody {
		t.Errorf("reply Body = %s, want verbatim stub response %s — run-ID substitution must not be applied to response content", got.Body, wantBody)
	}
}

// TestRun_RunStateReadFails_WithRunIDPlaceholderStub_InterceptorStillReplies
// asserts that a failed run-state read does not prevent the interceptor from
// emitting a valid reply to the harness. This pins the best-effort semantics
// the design requires: a state-read failure (absent or corrupt document) must
// leave the interceptor operational, even when a registered stub declares a
// side-effect path containing the run-ID placeholder.
//
// The run-state document is intentionally never written (no Initialize), so
// the Update call fails as it would when the driver's setup phase never ran or
// the state file was lost. Config.RunID is left at its zero value (empty
// string), matching the state the real binary entry point reaches when the
// run-state read does not succeed.
func TestRun_RunStateReadFails_WithRunIDPlaceholderStub_InterceptorStillReplies(t *testing.T) {
	subjectDir, controlDir := newSandboxDirs(t)
	clock := newFakeClock()
	adapter := fake.New(fake.Options{Capabilities: domain.HarnessCapabilities{SupportsDirectSubstitution: true}})

	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	registry := domain.StubRegistry{
		SchemaVersion: 1,
		TestID:        "stage4-state-read-fail",
		OnUnmatched:   domain.UnmatchedHalt,
		Stubs: []domain.Stub{{
			Match:    domain.StubMatch{Identity: id, Invocation: 1},
			Response: json.RawMessage(`{"status_code":"SUCCESS"}`),
			SideEffects: []domain.FileEffect{{
				Path:    "Orchestration-" + domain.RunIDPlaceholder + "/Plan.md",
				Content: "unreachable — state-read failure prevents this from executing",
			}},
		}},
	}
	cfg := bareConfig(t, controlDir, subjectDir, clock, registry, adapter)
	// No store.Initialize: the run-state document is absent, so the Update
	// call will fail. This simulates a failed run-state read — the condition
	// AC4.7 covers. Config.RunID is the zero string, matching the
	// best-effort-miss state: a real binary entry point that could not read
	// the state file populates RunID as empty rather than crashing.

	native := encodePre(t, id, "corr-1", "researcher#1", "do work")
	reply, exitCode, _ := run(t, cfg, domain.PhasePre, native)

	// The interceptor must return exit code 0 even when the state read fails.
	// A non-zero exit is interpreted by the harness as a failed hook, which
	// would damage the very run the interceptor is measuring.
	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0 — a failed run-state read must not crash the interceptor or leave the harness without a reply", exitCode)
	}

	// A valid JSON reply must have been emitted. An absent or malformed reply
	// damages the harness's view of the run.
	if len(reply) == 0 {
		t.Fatal("Run produced no reply; a valid native reply is required even when the run-state read fails")
	}
	var replyObj map[string]interface{}
	if err := json.Unmarshal(reply, &replyObj); err != nil {
		t.Fatalf("reply is not valid JSON: %v (reply: %s)", err, reply)
	}
}

// TestRun_SideEffectPath_EmptyRunID_PlaceholderExpandsToEmptyString asserts
// that an empty RunID in Config does not cause the interceptor to fail the
// call or panic. A {run_id} placeholder in the side-effect path must expand
// to the empty string rather than triggering a guard that refuses or crashes.
//
// This is the behavioral consequence of the best-effort-miss path: an
// interceptor process that could not read the run-state document populates
// Config.RunID as empty, expansion runs against that empty value, and the
// resulting path (with an empty segment where {run_id} was) is what the
// escape guard and file-write see. The interceptor must still reply correctly.
// The exact file location is secondary; the primary contract is exit code 0
// and a parseable reply.
func TestRun_SideEffectPath_EmptyRunID_PlaceholderExpandsToEmptyString(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	registry := domain.StubRegistry{
		SchemaVersion: 1,
		TestID:        "stage4-empty-runid-expansion",
		OnUnmatched:   domain.UnmatchedHalt,
		Stubs: []domain.Stub{{
			Match:    domain.StubMatch{Identity: id, Invocation: 1},
			Response: json.RawMessage(`{"status_code":"SUCCESS"}`),
			SideEffects: []domain.FileEffect{{
				// With RunID = "", this expands to "Orchestration-/Plan.md".
				// The interceptor must not treat an empty expansion as an error.
				Path:    "Orchestration-" + domain.RunIDPlaceholder + "/Plan.md",
				Content: "empty run-id expansion content",
			}},
		}},
	}
	h := newHarness(t, baseState(), registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)
	h.Config.RunID = "" // explicitly empty — the best-effort-miss state

	native := encodePre(t, id, "corr-1", "researcher#1", "do work")
	reply, exitCode, _ := run(t, h.Config, domain.PhasePre, native)

	// Exit code must be 0 even when RunID is empty. An empty RunID is a legal
	// state the interceptor must tolerate, not a reason to crash or refuse.
	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0 — an empty RunID must not cause the interceptor to fail the call", exitCode)
	}

	// A valid JSON reply must be present.
	if len(reply) == 0 {
		t.Fatal("Run produced no reply; a valid native reply is required even when RunID is empty")
	}
	var replyObj map[string]interface{}
	if err := json.Unmarshal(reply, &replyObj); err != nil {
		t.Fatalf("reply is not valid JSON: %v (reply: %s)", err, reply)
	}
}
