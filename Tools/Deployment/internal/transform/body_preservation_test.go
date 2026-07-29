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
// Agents/Generic/, the body bytes (everything after the closing frontmatter delimiter) are
// byte-identical to the source after a transformation with the fixture harness module.
//
// Agents with injection regions: the full body is compared. The fixture harness has no
// injection content (injections: []), so injection regions remain empty, matching the
// source.
func TestBodyPreservation_AllGenericAgentsBodyUnchanged(t *testing.T) {
	mod := newFixtureModule(t)
	model := fixtureModel()
	agentsDir := genericAgentsDir(t)
	paths := collectGenericAgentPaths(t, agentsDir)

	for _, p := range paths {
		p := p // capture for subtest
		// Use the path relative to Agents/Generic/ as the subtest name for readability.
		rel, _ := filepath.Rel(agentsDir, p)
		t.Run(rel, func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read %s: %v", p, err)
			}

			agentKey := strings.TrimSuffix(filepath.Base(p), ".md")
			req := transform.Request{
				Source: src,
				Kind:   domain.ArtifactAgent,
				Key:    agentKey,
				Module: mod,
				Model:  model,
				Scope:  domain.ScopeProject,
			}

			result, err := transform.Apply(req)
			if err != nil {
				// RED phase: Apply returns ErrNotImplemented, causing every subtest to fail here.
				t.Fatalf("Apply(%s): %v", rel, err)
			}

			// Extract body bytes from source and output using the same low-level split so the
			// comparison is purely about content, independent of frontmatter serialisation.
			_, srcBody, err := docformat.SplitFrontmatter(src)
			if err != nil {
				t.Fatalf("SplitFrontmatter (source, %s): %v", rel, err)
			}
			_, outBody, err := docformat.SplitFrontmatter(result.Output)
			if err != nil {
				t.Fatalf("SplitFrontmatter (output, %s): %v", rel, err)
			}

			if !bytes.Equal(srcBody, outBody) {
				t.Errorf("body bytes changed after transformation of %s\n"+
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

[[SECTION:Identity]]
# Synthetic Agent

This body must arrive byte-for-byte in the output.
No character may be altered, added, or removed.

[[INJECTION:HarnessConstraints]]
[[/INJECTION:HarnessConstraints]]

[[/SECTION:Identity]]
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

	_, srcBody, err := docformat.SplitFrontmatter(src)
	if err != nil {
		t.Fatalf("SplitFrontmatter (source): %v", err)
	}
	_, outBody, err := docformat.SplitFrontmatter(result.Output)
	if err != nil {
		t.Fatalf("SplitFrontmatter (output): %v", err)
	}

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
