package transform_test

// output_fidelity_fixtures_test.go holds the shared builders for the BOM and line-ending
// fidelity tests: a source that exercises every kind of region the transform writes, request
// builders, and byte-level assertions. In-memory sources are built in Go code because
// .gitattributes forces LF on checked-in files.

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

const utf8BOM = "\xEF\xBB\xBF"

// fullSourceLF declares one region of every kind the transform fills: harness, protocol,
// bundle, workflow, infrastructure, and project regions (empty and with default content).
const fullSourceLF = `---
id: 77
version: 2.4.1
name: fidelity-agent
description: Agent used for output fidelity testing
role: subagent
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Identity type="core">
## Identity

Fidelity agent.

<AuthorityHierarchy type="managed">
</AuthorityHierarchy>

<ClosingProcedure type="managed">
</ClosingProcedure>

<AvailableWorkflows type="managed">
</AvailableWorkflows>

<InfrastructureAgents type="managed">
</InfrastructureAgents>

<IdentityExtension type="project">
</IdentityExtension>

</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>

<Constraints type="core">
## Constraints

<HarnessConstraints type="managed">
</HarnessConstraints>

</Constraints>

<ErrorHandling type="core">
## Error Handling

<ErrorHandlingCommon type="managed">
</ErrorHandlingCommon>

</ErrorHandling>

<ExecutionPhilosophy type="core">
## Execution Philosophy

<ExecutionPhilosophyCommon type="managed">
</ExecutionPhilosophyCommon>

<ContextLimits type="project">
Default context limit line one.
Default context limit line two.
</ContextLimits>

</ExecutionPhilosophy>
`

// plainSourceLF has frontmatter and prose but no regions, so only frontmatter additions
// and stamps are written by the transform.
const plainSourceLF = `---
id: 78
version: 1.3.0
name: plain-agent
description: Agent without regions
role: subagent
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

# Plain agent

Some prose.
`

// noFrontmatterSourceLF has no frontmatter at all; the transform must synthesise one.
const noFrontmatterSourceLF = "# Bare agent\n\nSome prose.\n"

// crlfBytes returns s with every LF rewritten to CRLF.
func crlfBytes(s string) []byte {
	return []byte(strings.ReplaceAll(s, "\n", "\r\n"))
}

// withBOM returns b prefixed with the UTF-8 BOM.
func withBOM(b []byte) []byte {
	return append([]byte(utf8BOM), b...)
}

// normaliseEOL rewrites every CRLF in b to LF.
func normaliseEOL(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}

// fidelityRequest builds a request for the given source with providers that supply LF
// content, as the harness and catalog loaders do. deployed may be nil (create).
func fidelityRequest(t *testing.T, src, deployed []byte) transform.Request {
	t.Helper()
	return transform.Request{
		Source:   src,
		Kind:     domain.ArtifactAgent,
		Key:      "fidelity-agent",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleSubagent,
		Deployed: deployed,
		Protocol: fixtureProtocol("1.9"),
		Bundle:   fixtureBundle("1.0.0"),
		Workflows: []transform.WorkflowBlock{
			{ID: "quick-fix", Block: []byte(quickFixBlock)},
		},
		InfrastructureAgents: []transform.InfrastructureBlock{
			newInfraBlock("orchestration-review", "1.0.0"),
		},
		InjectionsVersion: "1.2.0",
	}
}

// mustApply runs the transform and fails the test on error.
func mustApply(t *testing.T, req transform.Request) []byte {
	t.Helper()
	res, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return res.Output
}

// firstBareLF returns the offset of the first LF not preceded by CR, or -1.
func firstBareLF(b []byte) int {
	for i, c := range b {
		if c == '\n' && (i == 0 || b[i-1] != '\r') {
			return i
		}
	}
	return -1
}

// lineAround returns the line containing offset off, for diagnostics.
func lineAround(b []byte, off int) string {
	start := bytes.LastIndexByte(b[:off], '\n') + 1
	end := bytes.IndexByte(b[off:], '\n')
	if end < 0 {
		return string(b[start:])
	}
	return string(b[start : off+end])
}

// assertNoBareLF fails when out contains an LF that is not part of a CRLF pair.
func assertNoBareLF(t *testing.T, out []byte) {
	t.Helper()
	if off := firstBareLF(out); off >= 0 {
		t.Errorf("output contains a bare LF at offset %d (line %q); a CRLF source must produce CRLF throughout",
			off, lineAround(out, off))
	}
}

// assertNoCR fails when out contains any carriage return.
func assertNoCR(t *testing.T, out []byte) {
	t.Helper()
	if off := bytes.IndexByte(out, '\r'); off >= 0 {
		t.Errorf("output contains a CR at offset %d (line %q); an LF source must produce LF throughout",
			off, lineAround(out, off))
	}
}

// filledDeployed returns a deployed file produced from the LF source on create, with the
// given project-region body written into the ContextLimits region using eol terminators.
func filledDeployed(t *testing.T, regionBody, eol string) []byte {
	t.Helper()
	out := string(mustApply(t, fidelityRequest(t, []byte(fullSourceLF), nil)))
	const marker = "Default context limit line one.\nDefault context limit line two.\n"
	if !strings.Contains(out, marker) {
		t.Fatalf("fixture deployed output lacks the default project content; output:\n%s", out)
	}
	out = strings.Replace(out, marker, strings.ReplaceAll(regionBody, "\n", eol), 1)
	if eol == "\r\n" {
		out = strings.ReplaceAll(strings.ReplaceAll(out, "\r\n", "\n"), "\n", "\r\n")
	}
	return []byte(out)
}
