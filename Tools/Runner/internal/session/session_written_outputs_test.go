package session_test

// Session-level tests for written-output detection and the HITL gate.
//
// Only the outputs an invocation actually created or modified are registered
// and gated. A declared output that is absent, or unchanged since before the
// invocation, is neither registered nor a gate miss. The gate runs for every
// response status. Each scenario runs on both dispatch paths: the auto path
// (engine-routed) and the consult path (consultant-routed).
//
// The tests use the filesystem detector and the file approval reader against a
// temporary workspace; MockAdapter ScriptedWrite stands in for the agent
// writing its declared files during the invocation.
//
// Deliberately not asserted here: which status is routed after a re-dispatch,
// and any behavior specific to a BLOCKED response's error code.

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"mosaic-common/interaction"
	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// stamped returns an artifact document carrying the given human_approved value.
// tag makes the content distinct between writes.
func stamped(approved, tag string) string {
	return "---\nhuman_approved: " + approved + "\n---\n# Artifact " + tag + "\n"
}

// recordingApprovalReader wraps the file approval reader and records every read.
type recordingApprovalReader struct {
	mu    sync.Mutex
	inner domain.ApprovalReader
	reads []approvalRead
}

type approvalRead struct {
	path     string
	approval domain.HumanApproval
}

func newRecordingApprovalReader() *recordingApprovalReader {
	return &recordingApprovalReader{inner: artifact.NewApprovalReader()}
}

func (r *recordingApprovalReader) ReadApproval(ctx context.Context, p string) domain.HumanApproval {
	a := r.inner.ReadApproval(ctx, p)
	r.mu.Lock()
	r.reads = append(r.reads, approvalRead{path: p, approval: a})
	r.mu.Unlock()
	return a
}

func (r *recordingApprovalReader) readsOf(p string) []domain.HumanApproval {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.HumanApproval
	for _, rd := range r.reads {
		if rd.path == p {
			out = append(out, rd.approval)
		}
	}
	return out
}

// dispatchPath names the on-disk (and dispatched) form of a run file.
func dispatchPath(rel string) string {
	return domain.RunScopedFolder(testRunID) + "/" + rel
}

// writeRunFile writes content to a file in the run folder, creating directories.
func writeRunFile(t *testing.T, runFolder, rel, content string) {
	t.Helper()
	p := filepath.Join(runFolder, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

type dispatchPathKind string

const (
	pathAuto    dispatchPathKind = "auto"
	pathConsult dispatchPathKind = "consult"
)

// detectingRig is a session wired with the real detector and approval reader.
type detectingRig struct {
	ses       session.Session
	adapter   *harness.MockAdapter
	store     *memStore
	reader    *recordingApprovalReader
	consult   *scriptedRoutingConsultant
	orchPath  string
	runFolder string
}

// newDetectingRig builds a session for the fixture, in a fresh workspace that is
// also the working directory. The adapter writes relative to the workspace.
func newDetectingRig(t *testing.T, fixture string, agents ...string) *detectingRig {
	t.Helper()
	runFolder := chdirWorkspace(t)
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, fixture)
	for _, a := range agents {
		writeAgentFile(t, dir, a)
	}
	f := harness.NewMockAdapter()
	f.SetWriteRoot(filepath.Dir(runFolder))
	store := &memStore{}
	reader := newRecordingApprovalReader()
	consult := &scriptedRoutingConsultant{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Routing:   consult,
		Approvals: reader,
		Outputs:   artifact.NewOutputWriteDetector(),
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return &detectingRig{ses: ses, adapter: f, store: store, reader: reader, consult: consult,
		orchPath: orchPath, runFolder: runFolder}
}

func (r *detectingRig) config(kind dispatchPathKind, workflowID string) domain.RunConfig {
	mode := domain.ExecutionModeAuto
	if kind == pathConsult {
		mode = domain.ExecutionModeOrchestrated
	}
	return domain.RunConfig{
		OrchestratorFilePath: r.orchPath,
		WorkflowID:           domain.WorkflowID(workflowID),
		Task:                 "test task",
		IsNewRun:             true,
		RunFolder:            r.runFolder,
		RunSettings:          domain.RunSettings{Mode: mode},
	}
}

func (r *detectingRig) invocationsOf(agent string) int {
	n := 0
	for _, inv := range r.adapter.Invocations() {
		if inv.Agent.Identifier == agent {
			n++
		}
	}
	return n
}

// acceptedSteps returns the workflow steps applied for the agent that carry the
// step's final outcome (neither infrastructure rows nor rejected attempts).
func (r *detectingRig) acceptedSteps(agent string) []domain.CompletedStep {
	var out []domain.CompletedStep
	for _, s := range r.store.Applied {
		if s.IsInfrastructure || s.HITLRejected {
			continue
		}
		if strings.HasPrefix(s.AgentInstance, agent+"#") {
			out = append(out, s)
		}
	}
	return out
}

func resp(status domain.StatusCode, msg string) *domain.ProtocolResponse {
	return &domain.ProtocolResponse{StatusCode: status, StatusMessage: msg}
}

// ===== Linear workflow: single concrete output =====

type attempt struct {
	status  domain.StatusCode
	content string // "" = the agent writes nothing during this attempt
}

type linearScenario struct {
	name        string
	pre         string // content of design.md before the run; "" = absent
	attempts    []attempt
	wantCalls   int  // invocations of agent-a
	wantWritten bool // accepted step registers design.md
}

// linearScenarios use the written-outputs fixture: agent-a (HITL) declares design.md.
func linearScenarios() []linearScenario {
	const (
		S  = domain.StatusSUCCESS
		NA = domain.StatusCOMPLETED_NEEDS_ACTION
		PD = domain.StatusPARTIALLY_DONE
	)
	return []linearScenario{
		{name: "declared but never written, SUCCESS: accepted, nothing registered",
			attempts: []attempt{{S, ""}}, wantCalls: 1, wantWritten: false},
		{name: "declared but never written, COMPLETED_NEEDS_ACTION: accepted, nothing registered",
			attempts: []attempt{{NA, ""}}, wantCalls: 1, wantWritten: false},
		{name: "created and approved: accepted and registered",
			attempts: []attempt{{S, stamped("true", "v1")}}, wantCalls: 1, wantWritten: true},
		{name: "created and approved, PARTIALLY_DONE: accepted and registered",
			attempts: []attempt{{PD, stamped("true", "v1")}}, wantCalls: 1, wantWritten: true},
		{name: "created with false stamp, SUCCESS: re-dispatched",
			attempts: []attempt{{S, stamped("false", "v1")}, {S, stamped("true", "v2")}}, wantCalls: 2, wantWritten: true},
		{name: "created with false stamp, COMPLETED_NEEDS_ACTION: re-dispatched",
			attempts: []attempt{{NA, stamped("false", "v1")}, {S, stamped("true", "v2")}}, wantCalls: 2, wantWritten: true},
		{name: "created with false stamp, PARTIALLY_DONE: re-dispatched",
			attempts: []attempt{{PD, stamped("false", "v1")}, {S, stamped("true", "v2")}}, wantCalls: 2, wantWritten: true},
		{name: "modified pre-existing and approved: accepted and registered",
			pre: stamped("false", "old"), attempts: []attempt{{S, stamped("true", "new")}}, wantCalls: 1, wantWritten: true},
		{name: "modified pre-existing to a false stamp: re-dispatched",
			pre: stamped("true", "old"), attempts: []attempt{{S, stamped("false", "new")}, {S, stamped("true", "newer")}}, wantCalls: 2, wantWritten: true},
		{name: "unchanged pre-existing with stale false stamp, SUCCESS: ignored",
			pre: stamped("false", "stale"), attempts: []attempt{{S, ""}}, wantCalls: 1, wantWritten: false},
		{name: "unchanged pre-existing with stale false stamp, COMPLETED_NEEDS_ACTION: ignored",
			pre: stamped("false", "stale"), attempts: []attempt{{NA, ""}}, wantCalls: 1, wantWritten: false},
	}
}

func runLinearScenario(t *testing.T, kind dispatchPathKind, sc linearScenario) {
	rig := newDetectingRig(t, "written-outputs-orch.md", "agent-a", "agent-b")
	plan := dispatchPath("design.md")
	if sc.pre != "" {
		writeRunFile(t, rig.runFolder, "design.md", sc.pre)
	}
	attempts, extra := withMechanicalFollowUp(kind, sc.attempts)
	for _, a := range attempts {
		entry := harness.ScriptedEntry{Response: resp(a.status, "attempt")}
		if a.content != "" {
			entry.Writes = []harness.ScriptedWrite{{Path: plan, Content: a.content}}
		}
		rig.adapter.Queue("agent-a", entry)
	}
	rig.adapter.Queue("agent-b", harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "b done")})

	if kind == pathConsult {
		rig.consult.queueDispatch("agent-a", "do the work", 0)
		rig.consult.queueStop("done")
		rig.consult.queueStop("done")
		rig.consult.queueStop("done")
	} else {
		rig.consult.queueStop("done")
		rig.consult.queueStop("done")
		rig.consult.queueStop("done")
	}

	rig.ses.Start(context.Background(), rig.config(kind, "written-outputs")) //nolint:errcheck

	if got := rig.invocationsOf("agent-a"); got != sc.wantCalls+extra {
		t.Errorf("agent-a invocations: want %d, got %d", sc.wantCalls+extra, got)
	}
	acc := rig.acceptedSteps("agent-a")
	if len(acc) != 1+extra {
		t.Fatalf("accepted agent-a steps: want %d, got %d", 1+extra, len(acc))
	}
	var want []string
	if sc.wantWritten {
		want = []string{plan}
	}
	if !reflect.DeepEqual(nonNilStrings(acc[0].WrittenArtifacts), nonNilStrings(want)) {
		t.Errorf("registered outputs: want %v, got %v", want, acc[0].WrittenArtifacts)
	}
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func TestSession_WrittenOutputs_HITLGate_Linear(t *testing.T) {
	for _, kind := range []dispatchPathKind{pathAuto, pathConsult} {
		for _, sc := range linearScenarios() {
			t.Run(string(kind)+"/"+sc.name, func(t *testing.T) {
				runLinearScenario(t, kind, sc)
			})
		}
	}
}

// TestSession_WrittenOutputs_RedispatchThatLeavesWrittenFileFalse_Escalates
// verifies the baseline of the first attempt is reused for the re-dispatch: a
// file the first attempt wrote with a false stamp stays gated even when the
// re-dispatch does not touch it, so the step is not accepted.
func TestSession_WrittenOutputs_RedispatchThatLeavesWrittenFileFalse_Escalates(t *testing.T) {
	for _, kind := range []dispatchPathKind{pathAuto, pathConsult} {
		t.Run(string(kind), func(t *testing.T) {
			rig := newDetectingRig(t, "written-outputs-orch.md", "agent-a", "agent-b")
			rig.adapter.Queue("agent-a",
				harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "one"),
					Writes: []harness.ScriptedWrite{{Path: dispatchPath("design.md"), Content: stamped("false", "v1")}}},
				harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "two")},
			)
			if kind == pathConsult {
				rig.consult.queueDispatch("agent-a", "do the work", 0)
			}
			rig.consult.queueStop("done")
			rig.consult.queueStop("done")

			rig.ses.Start(context.Background(), rig.config(kind, "written-outputs")) //nolint:errcheck

			if got := rig.invocationsOf("agent-a"); got != 2 {
				t.Errorf("agent-a invocations: want 2 (original + one re-dispatch), got %d", got)
			}
			if acc := rig.acceptedSteps("agent-a"); len(acc) != 0 {
				t.Errorf("a step whose written output is still stamped false must not be accepted, got %d accepted", len(acc))
			}
		})
	}
}

// TestSession_WrittenOutputs_UnchangedOutputIsNotRead verifies that approval is
// read only for written outputs: a stale false stamp on an unchanged file is
// never consulted.
func TestSession_WrittenOutputs_UnchangedOutputIsNotRead(t *testing.T) {
	for _, kind := range []dispatchPathKind{pathAuto, pathConsult} {
		t.Run(string(kind), func(t *testing.T) {
			rig := newDetectingRig(t, "written-outputs-orch.md", "agent-a", "agent-b")
			writeRunFile(t, rig.runFolder, "design.md", stamped("false", "stale"))
			rig.adapter.Queue("agent-a", harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "done")})
			rig.adapter.Queue("agent-b", harness.ScriptedEntry{Response: resp(domain.StatusSUCCESS, "done")})
			if kind == pathConsult {
				rig.consult.queueDispatch("agent-a", "do the work", 0)
			}
			rig.consult.queueStop("done")
			rig.consult.queueStop("done")

			rig.ses.Start(context.Background(), rig.config(kind, "written-outputs")) //nolint:errcheck

			if reads := rig.reader.readsOf(dispatchPath("design.md")); len(reads) != 0 {
				t.Errorf("unchanged design.md must not be read for approval, got %d read(s)", len(reads))
			}
		})
	}
}

// TestSession_WrittenOutputs_ReadFailuresStayDistinct verifies that the gate
// treats each read failure of a written output as non-compliant and reads it
// with its own distinct result (no frontmatter, malformed, field absent).
func TestSession_WrittenOutputs_ReadFailuresStayDistinct(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    domain.HumanApproval
	}{
		{"no frontmatter", "# Just a heading\n", domain.ApprovalNoFrontmatter},
		{"malformed value", "---\nhuman_approved: maybe\n---\n# Body\n", domain.ApprovalMalformed},
		{"field absent", "---\ntitle: something\n---\n# Body\n", domain.ApprovalAbsent},
		{"stamped false", stamped("false", "v1"), domain.ApprovalFalse},
	}
	for _, kind := range []dispatchPathKind{pathAuto, pathConsult} {
		for _, tc := range cases {
			t.Run(string(kind)+"/"+tc.name, func(t *testing.T) {
				rig := newDetectingRig(t, "written-outputs-orch.md", "agent-a", "agent-b")
				plan := dispatchPath("design.md")
				rig.adapter.Queue("agent-a",
					harness.ScriptedEntry{Response: resp(domain.StatusCOMPLETED_NEEDS_ACTION, "one"),
						Writes: []harness.ScriptedWrite{{Path: plan, Content: tc.content}}},
					harness.ScriptedEntry{Response: resp(domain.StatusCOMPLETED_NEEDS_ACTION, "two"),
						Writes: []harness.ScriptedWrite{{Path: plan, Content: tc.content + "\n<!-- again -->\n"}}},
				)
				if kind == pathConsult {
					rig.consult.queueDispatch("agent-a", "do the work", 0)
				}
				rig.consult.queueStop("done")
				rig.consult.queueStop("done")

				rig.ses.Start(context.Background(), rig.config(kind, "written-outputs")) //nolint:errcheck

				if got := rig.invocationsOf("agent-a"); got != 2 {
					t.Errorf("agent-a invocations: want 2 (one re-dispatch, then escalation), got %d", got)
				}
				reads := rig.reader.readsOf(plan)
				if len(reads) == 0 {
					t.Fatalf("design.md must be read for approval")
				}
				if reads[0] != tc.want {
					t.Errorf("first approval read of design.md: want %v, got %v", tc.want, reads[0])
				}
			})
		}
	}
}

// ===== Wildcard outputs =====

type globScenario struct {
	name      string
	pre       map[string]string // stage files present before the run (Plan.md excluded)
	plannerWr map[string]string // stage files the planner writes
	attempts  []map[string]string
	wantCalls int
	wantOut   []string // stage files registered by the accepted agent-a step, sorted
}

const globPlanMD = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | First stage | - | FALSE |
| 2 | Stage Two | Second stage | 1 | FALSE |
`

func globScenarios() []globScenario {
	s1, s2 := "Stage-1/Plan.md", "Stage-2/Plan.md"
	return []globScenario{
		{name: "every written match is registered",
			plannerWr: map[string]string{s1: stamped("true", "p1"), s2: stamped("true", "p2")},
			attempts:  []map[string]string{{s1: stamped("true", "a1"), s2: stamped("true", "a2")}},
			wantCalls: 1, wantOut: []string{s1, s2}},
		{name: "only the written match is registered, the unchanged one is ignored",
			plannerWr: map[string]string{s1: stamped("true", "p1"), s2: stamped("true", "p2")},
			attempts:  []map[string]string{{s2: stamped("true", "a2")}},
			wantCalls: 1, wantOut: []string{s2}},
		{name: "written match with false stamp is re-dispatched",
			plannerWr: map[string]string{s1: stamped("true", "p1"), s2: stamped("true", "p2")},
			attempts:  []map[string]string{{s2: stamped("false", "a2")}, {s2: stamped("true", "b2")}},
			wantCalls: 2, wantOut: []string{s2}},
		{name: "unchanged match with stale false stamp is ignored",
			plannerWr: map[string]string{s1: stamped("false", "stale"), s2: stamped("true", "p2")},
			attempts:  []map[string]string{{s2: stamped("true", "a2")}},
			wantCalls: 1, wantOut: []string{s2}},
		{name: "nothing written: accepted, nothing registered",
			plannerWr: map[string]string{s1: stamped("false", "stale"), s2: stamped("false", "stale")},
			attempts:  []map[string]string{{}},
			wantCalls: 1, wantOut: nil},
		{name: "match that appears during the dispatch is detected and gated",
			plannerWr: map[string]string{s1: stamped("true", "p1")},
			attempts:  []map[string]string{{s2: stamped("false", "a2")}, {s2: stamped("true", "b2")}},
			wantCalls: 2, wantOut: []string{s2}},
	}
}

func scriptedWrites(m map[string]string) []harness.ScriptedWrite {
	var paths []string
	for p := range m {
		paths = append(paths, p)
	}
	// deterministic order
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			if paths[j] < paths[i] {
				paths[i], paths[j] = paths[j], paths[i]
			}
		}
	}
	var out []harness.ScriptedWrite
	for _, p := range paths {
		out = append(out, harness.ScriptedWrite{Path: dispatchPath(p), Content: m[p]})
	}
	return out
}

func runGlobScenario(t *testing.T, kind dispatchPathKind, sc globScenario) {
	rig := newDetectingRig(t, "hitl-glob-staged-orch.md", "planner", "agent-a")
	writeRunFile(t, rig.runFolder, "Plan.md", globPlanMD)

	rig.adapter.Queue("planner", harness.ScriptedEntry{
		Response: resp(domain.StatusSUCCESS, "planned"), Writes: scriptedWrites(sc.plannerWr)})
	for _, a := range sc.attempts {
		status := domain.StatusSUCCESS
		rig.adapter.Queue("agent-a", harness.ScriptedEntry{
			Response: resp(status, "attempt"), Writes: scriptedWrites(a)})
	}

	if kind == pathConsult {
		globPaths := []string{"Stage-*/Plan.md"}
		rig.consult.queueDispatch("planner", "create the plan", 0)
		rig.consult.queueDispatchWithOutputs("agent-a", "do the work", 1, &globPaths)
	}
	rig.consult.queueStop("done")
	rig.consult.queueStop("done")

	rig.ses.Start(context.Background(), rig.config(kind, "hitl-glob-staged")) //nolint:errcheck

	if got := rig.invocationsOf("agent-a"); got != sc.wantCalls {
		t.Errorf("agent-a invocations: want %d, got %d", sc.wantCalls, got)
	}
	acc := rig.acceptedSteps("agent-a")
	if len(acc) != 1 {
		t.Fatalf("accepted agent-a steps: want 1, got %d", len(acc))
	}
	var want []string
	for _, w := range sc.wantOut {
		want = append(want, dispatchPath(w))
	}
	if !reflect.DeepEqual(nonNilStrings(acc[0].WrittenArtifacts), nonNilStrings(want)) {
		t.Errorf("registered outputs: want %v, got %v", want, acc[0].WrittenArtifacts)
	}
}

func TestSession_WrittenOutputs_HITLGate_Wildcard(t *testing.T) {
	for _, kind := range []dispatchPathKind{pathAuto, pathConsult} {
		for _, sc := range globScenarios() {
			t.Run(string(kind)+"/"+sc.name, func(t *testing.T) {
				runGlobScenario(t, kind, sc)
			})
		}
	}
}

// ensure the interaction import is used by the shared doubles in this package.
var _ interaction.Interaction = (*noopInteraction)(nil)
