package transform_test

// body_preservation_test.go tests the core purity invariant: instruction body prose is
// never altered. For every generic agent in the repository, the bytes between the
// frontmatter and end of file must be identical to the source after transformation, except
// for regions the transformation is explicitly permitted to change (injection content).
//
// This is the mechanised form of AC8.1: asserted over real agents, not a sample.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// TestBodyPreservation_AllGenericAgentsBodyUnchanged asserts that for every .md file under
// Catalog/Subagents/, the body structure is byte-identical before and after transformation,
// with tool-managed managed region region content excluded from comparison.
//
// managed region regions (CommunicationProtocol, AvailableWorkflows, InfrastructureAgents,
// HarnessConstraints, etc.) are unconditionally regenerated on every transform: their
// content is expected to change. The invariant being asserted is that everything else —
// section names, boundary tag lines, plain prose, and user-owned project-injection region regions —
// is never altered. Clearing DEPLOYED region content in both source and output before
// comparison isolates exactly this structural invariant.
func TestBodyPreservation_AllGenericAgentsBodyUnchanged(t *testing.T) {
	mod := newFixtureModule(t)
	model := fixtureModel()
	agentsDir := genericAgentsDir(t)
	paths := collectGenericAgentPaths(t, agentsDir)

	for _, p := range paths {
		p := p // capture for subtest
		// Use the path relative to Catalog/Subagents/ as the subtest name for readability.
		rel, _ := filepath.Rel(agentsDir, p)
		t.Run(rel, func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read %s: %v", p, err)
			}

			agentKey := strings.TrimSuffix(filepath.Base(p), ".md")
			// Determine role from agent key: only the orchestrator gets RoleOrchestrator.
			role := domain.RoleWorker
			if agentKey == "orchestrator" {
				role = domain.RoleOrchestrator
			}
			req := transform.Request{
				Source:   src,
				Kind:     domain.ArtifactAgent,
				Key:      agentKey,
				Module:   mod,
				Model:    model,
				Scope:    domain.ScopeProject,
				Role:     role,
				Protocol: fixtureProtocol("1.9"),
			}

			result, err := transform.Apply(req)
			if err != nil {
				t.Fatalf("Apply(%s): %v", rel, err)
			}

			// Compare body structure with managed region content cleared in both operands.
			// DEPLOYED region content is legitimately different before and after transform;
			// the structural bytes surrounding those regions must remain identical.
			srcBody := bodyWithDeployedContentCleared(t, src, rel+" (source)")
			outBody := bodyWithDeployedContentCleared(t, result.Output, rel+" (output)")

			if !bytes.Equal(srcBody, outBody) {
				t.Errorf("body structure changed after transformation of %s\n"+
					"source body (%d bytes, first 200):\n%q\n"+
					"output body (%d bytes, first 200):\n%q",
					rel,
					len(srcBody), truncateBytes(srcBody, 200),
					len(outBody), truncateBytes(outBody, 200),
				)
			}
		})
	}
}

// bodyWithDeployedContentCleared parses src, clears every managed region region's inner
// content, and returns the resulting body bytes (everything after the frontmatter delimiter).
// The boundary tag lines themselves are preserved; only the bytes between them are erased.
// This lets the caller compare body structure while tolerating expected changes to
// tool-managed region content across a transform.
func bodyWithDeployedContentCleared(t *testing.T, src []byte, label string) []byte {
	t.Helper()
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("docformat.Parse(%s): %v", label, err)
	}
	for _, node := range doc.Body().DeployedRegions() {
		if err := node.Clear(); err != nil {
			t.Fatalf("clear deployed region %q in %s: %v", node.Name(), label, err)
		}
	}
	_, body, err := docformat.SplitFrontmatter(doc.Bytes())
	if err != nil {
		t.Fatalf("SplitFrontmatter(%s): %v", label, err)
	}
	return body
}

// TestBodyPreservation_BodyLengthMatchesSource is a lighter check that can be run without
// the full repository present: it uses a synthesised in-memory agent whose body is a known
// byte sequence. A correct implementation must reproduce that sequence byte-for-byte.
func TestBodyPreservation_BodyLengthMatchesSource(t *testing.T) {
	const syntheticSource = `---
id: 1
version: 1.0.0
name: synthetic-agent
description: A synthetic agent for body-preservation testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal task
required_skills: []
---

<Identity type="core">
# Synthetic Agent

This body must arrive byte-for-byte in the output.
No character may be altered, added, or removed.

<HarnessConstraints type="managed">
</HarnessConstraints>

</Identity>
`

	mod := newFixtureModule(t)
	src := []byte(syntheticSource)

	req := transform.Request{
		Source: src,
		Kind:   domain.ArtifactAgent,
		Key:    "synthetic-agent",
		Module: mod,
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err) // RED: fails here with ErrNotImplemented
	}

	// Compare body structure with managed region content cleared in both operands.
	// Deployed region content is legitimately different before and after transform;
	// the structural bytes surrounding those regions must remain identical.
	srcBody := bodyWithDeployedContentCleared(t, src, "synthetic-agent (source)")
	outBody := bodyWithDeployedContentCleared(t, result.Output, "synthetic-agent (output)")

	if !bytes.Equal(srcBody, outBody) {
		t.Errorf("body bytes differ\ngot  %d bytes, want %d bytes\n"+
			"first differing position: %d\n"+
			"source (first 300):\n%q\n"+
			"output (first 300):\n%q",
			len(outBody), len(srcBody),
			firstDiff(srcBody, outBody),
			truncateBytes(srcBody, 300),
			truncateBytes(outBody, 300),
		)
	}
}

// firstDiff returns the byte offset of the first difference between a and b, or the
// length of the shorter slice when one is a prefix of the other. Returns 0 when both are
// empty or identical.
func firstDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
