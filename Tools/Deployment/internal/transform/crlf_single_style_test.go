package transform_test

// crlf_single_style_test.go covers the single line-ending style guarantee of the transform:
// a CRLF source yields CRLF throughout, an LF source yields LF throughout, CRLF and LF outputs
// are equal after normalisation, and repeated transforms are byte-identical.
//
// The CRLF tests are isolated per insertion path (one subtest per region kind, per preserved
// content kind and per frontmatter addition) so a failure names the path that emits LF. All
// content providers (protocol, bundle, workflow, infrastructure, harness) supply LF content,
// as the loaders do; the origin of the content must not matter.

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

const miniHeaderLF = `---
id: 79
version: 1.0.0
name: mini-agent
description: Agent with one region
role: subagent
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

`

// miniDeployedHeaderLF is a deployed-file frontmatter matching miniHeaderLF after transform.
const miniDeployedHeaderLF = `---
id: 79
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent with one region
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

`

func miniSourceLF(body string) string { return miniHeaderLF + body }

const infraRegionLF = "<Identity type=\"core\">\n## Identity\n\n<InfrastructureAgents type=\"managed\">\n</InfrastructureAgents>\n\n</Identity>\n"

const workflowRegionLF = "<Identity type=\"core\">\n## Identity\n\n<AvailableWorkflows type=\"managed\">\n</AvailableWorkflows>\n\n</Identity>\n"

const projectRegionLF = "<Identity type=\"core\">\n## Identity\n\n<IdentityExtension type=\"project\">\n</IdentityExtension>\n\n</Identity>\n"

// regionPath is one insertion path exercised with a CRLF source.
type regionPath struct {
	name     string
	body     string // source body (after the frontmatter), LF
	deployed string // deployed file (LF); empty means create
	tweak    func(*transform.Request)
}

func createPaths() []regionPath {
	return []regionPath{
		{name: "harness region", body: "<HarnessConstraints type=\"managed\">\n</HarnessConstraints>\n"},
		{name: "protocol region", body: "<CommunicationProtocol type=\"managed\">\n</CommunicationProtocol>\n"},
		{name: "bundle region", body: "<Identity type=\"core\">\n## Identity\n\n<AuthorityHierarchy type=\"managed\">\n</AuthorityHierarchy>\n\n</Identity>\n"},
		{name: "workflow region", body: workflowRegionLF},
		{name: "infrastructure region", body: infraRegionLF},
		{name: "infrastructure merge region", body: infraRegionLF, tweak: func(r *transform.Request) {
			r.InfrastructureMerge = transform.InfrastructureMergeEnsure
		}},
		{name: "project region with default content", body: "<Identity type=\"core\">\n## Identity\n\n<IdentityExtension type=\"project\">\nDefault one.\nDefault two.\n</IdentityExtension>\n\n</Identity>\n"},
		{name: "emptied project region", body: projectRegionLF},
		{name: "frontmatter additions and stamps", body: "# Plain agent\n\nSome prose.\n"},
	}
}

func updatePaths() []regionPath {
	return []regionPath{
		{
			name: "project region preserved from LF deployed file",
			body: projectRegionLF,
			deployed: miniDeployedHeaderLF + "<Identity type=\"core\">\n## Identity\n\n<IdentityExtension type=\"project\">\n" +
				"User line one.\n\nUser line two.\n</IdentityExtension>\n\n</Identity>\n",
		},
		{
			name: "workflow region preserved from LF deployed file",
			body: workflowRegionLF,
			deployed: miniDeployedHeaderLF + "<Identity type=\"core\">\n## Identity\n\n<AvailableWorkflows type=\"managed\">\n" +
				"<Workflow type=\"managed\" name=\"quick-fix\" version=\"3.0\">\n## Quick Fix\n\n| Row | Phase |\n|-----|-------|\n| 1 | PLANNING |\n\n</Workflow>\n" +
				"</AvailableWorkflows>\n\n</Identity>\n",
			tweak: func(r *transform.Request) { r.Workflows = nil; r.PreserveDeployedWorkflows = true },
		},
		{
			name: "infrastructure region preserved from LF deployed file",
			body: infraRegionLF,
			deployed: miniDeployedHeaderLF + "<Identity type=\"core\">\n## Identity\n\n<InfrastructureAgents type=\"managed\">\n" +
				infraSection(newInfraBlock("kept-agent", "1.0.0")) + "</InfrastructureAgents>\n\n</Identity>\n",
			tweak: func(r *transform.Request) { r.InfrastructureAgents = nil },
		},
		{
			name: "infrastructure merge against LF deployed region",
			body: infraRegionLF,
			deployed: miniDeployedHeaderLF + "<Identity type=\"core\">\n## Identity\n\n<InfrastructureAgents type=\"managed\">\n" +
				infraSection(newInfraBlock("kept-agent", "1.0.0")) + "</InfrastructureAgents>\n\n</Identity>\n",
			tweak: func(r *transform.Request) { r.InfrastructureMerge = transform.InfrastructureMergeEnsure },
		},
		{
			name: "nested custom region re-emitted from LF deployed file",
			body: infraRegionLF,
			deployed: miniDeployedHeaderLF + "<Identity type=\"core\">\n## Identity\n\n<InfrastructureAgents type=\"managed\">\n" +
				infraSection(newInfraBlock("orchestration-review", "1.0.0")) + customRegionInInfra +
				"</InfrastructureAgents>\n\n</Identity>\n",
		},
		{
			name: "parked custom region from LF deployed file",
			body: "# Plain agent\n\nSome prose.\n",
			deployed: miniDeployedHeaderLF + "# Plain agent\n\nSome prose.\n\n<Orphaned type=\"custom\">\nOrphan one.\nOrphan two.\n</Orphaned>\n",
		},
		{
			name:     "frontmatter updated against LF deployed file",
			body:     "# Plain agent\n\nSome prose.\n",
			deployed: miniDeployedHeaderLF + "# Plain agent\n\nSome prose.\n",
		},
	}
}

func (p regionPath) request(t *testing.T, src []byte) transform.Request {
	t.Helper()
	var deployed []byte
	if p.deployed != "" {
		deployed = []byte(p.deployed)
	}
	req := fidelityRequest(t, src, deployed)
	if p.tweak != nil {
		p.tweak(&req)
	}
	return req
}

// TestApply_CRLFSource_PerInsertionPath_NoBareLF isolates each content insertion path with a
// CRLF source and asserts the output of that path contains no bare LF.
func TestApply_CRLFSource_PerInsertionPath_NoBareLF(t *testing.T) {
	for _, p := range append(createPaths(), updatePaths()...) {
		t.Run(p.name, func(t *testing.T) {
			out := mustApply(t, p.request(t, crlfBytes(miniSourceLF(p.body))))
			assertNoBareLF(t, out)
		})
	}
}

// TestApply_LFSource_PerInsertionPath_NoCR isolates each insertion path with an LF source and
// a deployed file in CRLF, and asserts the output contains no carriage return.
func TestApply_LFSource_PerInsertionPath_NoCR(t *testing.T) {
	for _, p := range append(createPaths(), updatePaths()...) {
		t.Run(p.name, func(t *testing.T) {
			if p.deployed != "" {
				p.deployed = string(crlfBytes(p.deployed))
			}
			out := mustApply(t, p.request(t, []byte(miniSourceLF(p.body))))
			assertNoCR(t, out)
		})
	}
}

// TestApply_CRLFSource_NoFrontmatter_SynthesisedFrontmatterIsCRLF verifies that frontmatter
// added to a source that had none follows the source's CRLF style.
func TestApply_CRLFSource_NoFrontmatter_SynthesisedFrontmatterIsCRLF(t *testing.T) {
	out := mustApply(t, fidelityRequest(t, crlfBytes(noFrontmatterSourceLF), nil))
	assertNoBareLF(t, out)
}

// TestApply_CRLFSource_FullOutput_NoBareLF checks the whole output of a source carrying every
// region kind, for create, update against an LF deployed file, and update against a CRLF one.
func TestApply_CRLFSource_FullOutput_NoBareLF(t *testing.T) {
	cases := []struct {
		name     string
		deployed []byte
	}{
		{"create", nil},
		{"update against LF deployed file", filledDeployed(t, "Kept.\nContext.\n", "\n")},
		{"update against CRLF deployed file", filledDeployed(t, "Kept.\nContext.\n", "\r\n")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertNoBareLF(t, mustApply(t, fidelityRequest(t, crlfBytes(fullSourceLF), tc.deployed)))
		})
	}
}

// TestApply_LFSource_FullOutput_NoCR checks that an LF source never yields CRLF, including
// when the deployed file is CRLF and when providers supply CRLF blocks.
func TestApply_LFSource_FullOutput_NoCR(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		assertNoCR(t, mustApply(t, fidelityRequest(t, []byte(fullSourceLF), nil)))
	})
	t.Run("update against CRLF deployed file", func(t *testing.T) {
		deployed := filledDeployed(t, "Kept.\nContext.\n", "\r\n")
		assertNoCR(t, mustApply(t, fidelityRequest(t, []byte(fullSourceLF), deployed)))
	})
	t.Run("CRLF provider blocks", func(t *testing.T) {
		req := fidelityRequest(t, []byte(fullSourceLF), nil)
		req.Protocol = fixtureProtocolCRLF("1.9")
		req.Bundle = crlfBundle(fixtureBundle("1.0.0"))
		req.Workflows = []transform.WorkflowBlock{{ID: "quick-fix", Block: crlfBytes(quickFixBlock)}}
		assertNoCR(t, mustApply(t, req))
	})
}

func crlfBundle(b domain.BundleContent) domain.BundleContent {
	blocks := make([]domain.BundleBlock, len(b.Blocks))
	for i, blk := range b.Blocks {
		blk.Content = bytes.ReplaceAll(blk.Content, []byte("\n"), []byte("\r\n"))
		blocks[i] = blk
	}
	b.Blocks = blocks
	return b
}

// TestApply_CRLFAndLFSources_EqualAfterNormalisation verifies that the CRLF catalog deploys
// the same content as the LF catalog once line endings are normalised.
func TestApply_CRLFAndLFSources_EqualAfterNormalisation(t *testing.T) {
	deployedLF := filledDeployed(t, "Kept.\nContext.\n", "\n")
	deployedCRLF := filledDeployed(t, "Kept.\nContext.\n", "\r\n")
	cases := []struct {
		name         string
		lf, crlf     []byte
		depLF, depCR []byte
	}{
		{"create", []byte(fullSourceLF), crlfBytes(fullSourceLF), nil, nil},
		{"update", []byte(fullSourceLF), crlfBytes(fullSourceLF), deployedLF, deployedCRLF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lfOut := mustApply(t, fidelityRequest(t, tc.lf, tc.depLF))
			crlfOut := mustApply(t, fidelityRequest(t, tc.crlf, tc.depCR))
			if !bytes.Equal(normaliseEOL(crlfOut), lfOut) {
				t.Errorf("CRLF output differs from LF output after normalisation\nCRLF (normalised): %q\nLF: %q",
					normaliseEOL(crlfOut), lfOut)
			}
		})
	}
}

// TestApply_RepeatedTransform_ByteIdentical verifies that the same inputs give byte-identical
// output on every call, in both line-ending styles, and that re-rendering the output against
// itself is stable.
func TestApply_RepeatedTransform_ByteIdentical(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  []byte
	}{
		{"LF", []byte(fullSourceLF)},
		{"CRLF", crlfBytes(fullSourceLF)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := mustApply(t, fidelityRequest(t, tc.src, nil))
			second := mustApply(t, fidelityRequest(t, tc.src, nil))
			if !bytes.Equal(first, second) {
				t.Error("two transforms of the same inputs differ")
			}
			again := mustApply(t, fidelityRequest(t, tc.src, first))
			if !bytes.Equal(first, again) {
				t.Errorf("re-rendering the output against itself changed it\nfirst: %q\nagain: %q", first, again)
			}
			if strings.Contains(tc.name, "CRLF") {
				assertNoBareLF(t, again)
			}
		})
	}
}
