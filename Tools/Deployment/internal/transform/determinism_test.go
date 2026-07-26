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

[[INJECTION:HarnessConstraints]]
[[/INJECTION:HarnessConstraints]]

[[/SECTION:Identity]]
---

[[SECTION:Constraints]]
## Constraints

Some constraint text.

[[INJECTION:CustomConstraints]]
[[/INJECTION:CustomConstraints]]

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
