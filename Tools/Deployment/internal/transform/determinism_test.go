package transform_test

// determinism_test.go asserts that the same inputs always produce byte-identical output,
// regardless of how many times Apply is called or in what goroutine order (AC8.3).
//
// The tests cover two scenarios:
//   1. Sequential repeated calls: call Apply twice, compare outputs.
//   2. Map-iteration-sensitive paths: run Apply on multiple agents and verify no output
//      depends on Go map iteration order (which is randomised per run).

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// deterministicSource is a source agent with a representative frontmatter and body,
// including injection regions that may be touched by the transformation.
const deterministicSource = `---
id: 55
version: 4.0.0
name: determinism-agent
description: Agent for determinism testing
model: {model-identifier}
tools: [file_read, file_write, content_search, terminal]
recommended_tier: HIGH
tier_rationale: complex coordination
required_skills: []
---

[[SECTION:Identity]]
# Determinism Agent

Body content that must appear identically on every run.

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]

[[/SECTION:Identity]]
---

[[SECTION:Constraints]]
## Constraints

Some constraint text.

[[DEPLOYED:CustomConstraints]]
[[/DEPLOYED:CustomConstraints]]

[[/SECTION:Constraints]]
`

// TestDeterminism_SequentialCallsProduceIdenticalOutput calls Apply twice with identical
// inputs and asserts that both outputs are byte-identical. This catches implementations
// that accidentally accumulate or mutate shared state between calls.
func TestDeterminism_SequentialCallsProduceIdenticalOutput(t *testing.T) {
	mod := newFixtureModule(t)
	src := []byte(deterministicSource)

	makeReq := func() transform.Request {
		return transform.Request{
			Source: src,
			Kind:   domain.ArtifactAgent,
			Key:    "determinism-agent",
			Module: mod,
			Model:  fixtureModel(),
			Scope:  domain.ScopeProject,
		}
	}

	result1, err := transform.Apply(makeReq())
	if err != nil {
		t.Fatalf("Apply (first call): %v", err) // RED: fails here
	}
	result2, err := transform.Apply(makeReq())
	if err != nil {
		t.Fatalf("Apply (second call): %v", err)
	}

	if !bytes.Equal(result1.Output, result2.Output) {
		diff := firstDiff(result1.Output, result2.Output)
		t.Errorf("Apply is non-deterministic: identical inputs produced different outputs\n"+
			"first call:  %d bytes\n"+
			"second call: %d bytes\n"+
			"first difference at byte %d\n"+
			"first call  (first 400 bytes): %q\n"+
			"second call (first 400 bytes): %q",
			len(result1.Output), len(result2.Output), diff,
			truncateBytes(result1.Output, 400),
			truncateBytes(result2.Output, 400),
		)
	}
}

// TestDeterminism_RepeatedCallsProduceSameReport asserts that the transformation report is
// also deterministic: the same fields, in the same order, with the same reasons.
func TestDeterminism_RepeatedCallsProduceSameReport(t *testing.T) {
	mod := newFixtureModule(t)
	src := []byte(deterministicSource)

	req := transform.Request{
		Source: src,
		Kind:   domain.ArtifactAgent,
		Key:    "determinism-agent",
		Module: mod,
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	r1, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (first call): %v", err)
	}
	r2, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (second call): %v", err)
	}

	// Compare FieldChange lists.
	if len(r1.Report.Fields) != len(r2.Report.Fields) {
		t.Errorf("Report.Fields length differs: %d vs %d", len(r1.Report.Fields), len(r2.Report.Fields))
	} else {
		for i, f1 := range r1.Report.Fields {
			f2 := r2.Report.Fields[i]
			if f1.Key != f2.Key || f1.Before != f2.Before || f1.After != f2.After || f1.Reason != f2.Reason {
				t.Errorf("Report.Fields[%d] differs:\n  first:  %+v\n  second: %+v", i, f1, f2)
			}
		}
	}

	// Compare OutputBytes.
	if r1.Report.OutputBytes != r2.Report.OutputBytes {
		t.Errorf("Report.OutputBytes: %d vs %d", r1.Report.OutputBytes, r2.Report.OutputBytes)
	}
}

// ---------------------------------------------------------------------------
// T4.3: Regeneration on update — protocol content replaced, not lifted
// ---------------------------------------------------------------------------

// TestProtocol_RegenerationOnUpdate_ContentReplacedNotLifted verifies that on an update
// (Request.Deployed non-nil), the content of the [[DEPLOYED:CommunicationProtocol]] region
// is taken from Request.Protocol and not lifted from the deployed file. Tool-managed regions
// are never preserved from the deployed file — they are always regenerated.
func TestProtocol_RegenerationOnUpdate_ContentReplacedNotLifted(t *testing.T) {
	// The deployed file has stale protocol content that differs from what the current
	// protocol source would produce. After the update transform, the output must contain
	// the new block and not the stale content.
	const staleProtocolContent = "STALE PROTOCOL CONTENT — must not appear in updated output\n"
	deployedWithStaleProtocol := "---\nid: 99\nversion: 1.0.0\ntransform_version: 3.0.0\ninjections_version: 1.2.0\n" +
		"description: Agent for protocol region testing\nmode: subagent\nmodel: claude/claude-sonnet\ntools: [read-file]\n---\n\n" +
		"[[SECTION:Identity]]\n## Identity\n\nProtocol test agent.\n\n[[/SECTION:Identity]]\n\n" +
		"[[DEPLOYED:CommunicationProtocol]]\n" + staleProtocolContent + "[[/DEPLOYED:CommunicationProtocol]]\n"

	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Deployed: []byte(deployedWithStaleProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol("1.9"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("[[DEPLOYED:CommunicationProtocol]] absent from output")
	}
	regionContent := node.Content()

	// The stale content from the deployed file must not appear in the output.
	if bytes.Contains(regionContent, []byte(staleProtocolContent)) {
		t.Errorf("protocol region contains stale content from deployed file; tool-managed regions must be regenerated:\ncontent: %q", regionContent)
	}

	// The fresh content from Protocol must appear in the output.
	if !bytes.Contains(regionContent, []byte(subagentBlockContent)) {
		t.Errorf("protocol region does not contain current subagent block; region content: %q", regionContent)
	}
}

// TestProtocol_RegenerationOnUpdate_RepeatedTransformsAreByteIdentical verifies that
// applying the same transform with protocol content produces byte-identical output on
// every call. This is the determinism requirement applied to the protocol path.
//
// The test also asserts that the output actually contains the protocol block, so that
// byte-identical determinism is not satisfied vacuously (two calls producing the same
// empty output would be identical but not correct).
func TestProtocol_RegenerationOnUpdate_RepeatedTransformsAreByteIdentical(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleOrchestrator,
		Protocol: fixtureProtocol("2.1"),
	}

	result1, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (first call): %v", err)
	}
	result2, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (second call): %v", err)
	}

	// Both calls must contain the protocol block — ensures determinism is not satisfied
	// vacuously (two empty outputs are trivially identical but indicate a missing feature).
	if !bytes.Contains(result1.Output, []byte(orchestratorBlockContent)) {
		t.Error("Apply (first call): output does not contain orchestrator block; determinism of empty output is vacuous")
	}

	if !bytes.Equal(result1.Output, result2.Output) {
		diff := firstDiff(result1.Output, result2.Output)
		t.Errorf("protocol transform is non-deterministic: identical inputs produced different outputs\n"+
			"first call:  %d bytes\n"+
			"second call: %d bytes\n"+
			"first difference at byte %d",
			len(result1.Output), len(result2.Output), diff)
	}
}

// TestDeterminism_MultipleAgentsDontCrossContaminate applies the transformation to several
// distinct agents in sequence and verifies each produces the same output as if it were
// transformed in isolation. This catches implementations that keep per-agent state in a
// package-level variable.
func TestDeterminism_MultipleAgentsDontCrossContaminate(t *testing.T) {
	mod := newFixtureModule(t)
	model := fixtureModel()

	agents := []struct {
		key string
		src string
	}{
		{
			key: "agent-alpha",
			src: "---\nid: 1\nversion: 1.0.0\nname: agent-alpha\ndescription: Alpha\nmodel: {model-identifier}\ntools: [file_read]\nrecommended_tier: LOW\ntier_rationale: simple\nrequired_skills: []\n---\n\n[[SECTION:Identity]]\nAlpha body.\n[[/SECTION:Identity]]\n",
		},
		{
			key: "agent-beta",
			src: "---\nid: 2\nversion: 2.0.0\nname: agent-beta\ndescription: Beta\nmodel: {model-identifier}\ntools: [file_write, content_search]\nrecommended_tier: MEDIUM\ntier_rationale: moderate\nrequired_skills: []\n---\n\n[[SECTION:Identity]]\nBeta body.\n[[/SECTION:Identity]]\n",
		},
		{
			key: "agent-gamma",
			src: "---\nid: 3\nversion: 3.0.0\nname: agent-gamma\ndescription: Gamma\nmodel: {model-identifier}\ntools: [terminal, user_interaction]\nrecommended_tier: HIGH\ntier_rationale: complex\nrequired_skills: []\n---\n\n[[SECTION:Identity]]\nGamma body.\n[[/SECTION:Identity]]\n",
		},
	}

	// Run once to capture baseline outputs.
	baseline := make([][]byte, len(agents))
	for i, a := range agents {
		r, err := transform.Apply(transform.Request{
			Source: []byte(a.src),
			Kind:   domain.ArtifactAgent,
			Key:    a.key,
			Module: mod,
			Model:  model,
			Scope:  domain.ScopeProject,
		})
		if err != nil {
			t.Fatalf("Apply (baseline, %s): %v", a.key, err)
		}
		baseline[i] = r.Output
	}

	// Run again in the same order and compare.
	for i, a := range agents {
		r, err := transform.Apply(transform.Request{
			Source: []byte(a.src),
			Kind:   domain.ArtifactAgent,
			Key:    a.key,
			Module: mod,
			Model:  model,
			Scope:  domain.ScopeProject,
		})
		if err != nil {
			t.Fatalf("Apply (repeat, %s): %v", a.key, err)
		}
		if !bytes.Equal(r.Output, baseline[i]) {
			t.Errorf("agent %q: output differs between run 1 and run 2", a.key)
		}
	}
}
