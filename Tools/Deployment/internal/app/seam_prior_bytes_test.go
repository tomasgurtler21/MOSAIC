package app

// seam_prior_bytes_test.go contains the prior-bytes contract tests (T7.2).
//
// These tests assert the observable outcomes at the seam boundary, not the call shapes:
// an error return, a named decode failure, or a per-artifact failure record.
//
// # Cases
//
//   (a) OpUpdate with no prior deployed bytes fails loudly with ErrMissingPriorBytes
//       rather than silently deploying with user keys dropped.
//
//   (b) An update where the decoded canonical bytes are present but the raw prior bytes
//       were not threaded through the DeployedRaw field fails the same way. This is the
//       wiring regression the second field exists to catch; it is invisible on Markdown
//       harnesses where canonical == raw, so this case targets Codex specifically.
//
//   (c) A deployed file that exists on disk but cannot be read fails that artifact only
//       (the content callback returns an error). The rest of the run is unaffected. This
//       is new behaviour: today the nil-on-error reader silently treats unreadable files
//       as creates. I7.2a is what makes the case expressible.
//
//   (d) A deployed file that reads successfully but fails to decode (malformed TOML)
//       fails that artifact with ErrMalformedDeployed, distinguishable from both the
//       unreadable-file outcome and the absent-file outcome.
//
//   (e) An artifact context with Op: OpUnspecified on a Codex artifact fails with
//       ErrUnspecifiedOperation. Codex is the format under test because the Markdown
//       translator accepts any Op value.
//
//   (f) An artifact context with Op: OpCreate carrying non-nil prior bytes on a Codex
//       artifact fails with ErrUnspecifiedOperation.
//
//   (g) A plan item with PlanAction("") (the zero value) yields a non-nil error from the
//       Layer 3 action mapping. This is the safety net against an unrecognised PlanAction.
//
// # Translator registration
//
// Cases (a)-(f) use the Codex TOML translator. The blank import below registers it
// through the agentformat/all wiring package.  A direct blank import of
// internal/agentformat/codextoml is forbidden.

import (
	"errors"
	"testing"
	"time"

	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"

	_ "mosaic-deploy/internal/agentformat/all"
)

// ---------------------------------------------------------------------------
// Codex stub module used by the prior-bytes contract cases
// ---------------------------------------------------------------------------

// priorBytesCodexModule is a minimal domain.HarnessModule whose descriptor declares
// the Codex TOML format ID, so that transform.Apply resolves and calls the Codex
// translator. All other module methods return safe zero values or stubs.
type priorBytesCodexModule struct{}

func (priorBytesCodexModule) Ref() domain.HarnessRef {
	return domain.HarnessRef{ID: "codex-test", Tier: domain.TierBuiltin, Usable: true}
}
func (priorBytesCodexModule) Descriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		ID:            "codex-test",
		AgentFormatID: "codex-toml",
	}
}
func (priorBytesCodexModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
	}
	return domain.ToolResult{Resolutions: resolutions}, nil
}
func (priorBytesCodexModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (priorBytesCodexModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return ".codex/agents/" + req.Key + ".toml", nil
}
func (priorBytesCodexModule) Injection(_ domain.InjectionRequest) (string, bool) { return "", false }
func (priorBytesCodexModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "test"}, nil
}
func (priorBytesCodexModule) Close() error { return nil }

// codexAgentSource is a minimal canonical Markdown document with YAML frontmatter that
// the Codex translator can encode without error on the create path (non-empty body,
// AgentKey-matching name, description present).
const codexAgentSource = `---
name: my-codex-agent
description: A test Codex agent for prior-bytes contract tests.
---

This is the developer_instructions body. It is non-empty so ErrEmptyBody is not triggered.
`

// codexAgentKey is the agent key used in the prior-bytes contract tests. It matches
// the name field in codexAgentSource so that EntryOverriddenName is not emitted.
const codexAgentKey = "my-codex-agent"

// applyCodexWithOp is a helper that calls transform.Apply with the priorBytesCodexModule,
// the shared codexAgentSource, and the given Op and prior bytes fields.
func applyCodexWithOp(op agentformat.Operation, deployedCanonical, deployedRaw []byte) (transform.Result, error) {
	return transform.Apply(transform.Request{
		Source:      []byte(codexAgentSource),
		Kind:        domain.ArtifactAgent,
		Key:         codexAgentKey,
		Module:      priorBytesCodexModule{},
		Scope:       domain.ScopeProject,
		Deployed:    deployedCanonical,
		DeployedRaw: deployedRaw,
		Op:          op,
		Protocol:    domain.ProtocolContent{},
	})
}

// ---------------------------------------------------------------------------
// T7.2(a): OpUpdate with nil prior bytes fails with ErrMissingPriorBytes
// ---------------------------------------------------------------------------

// TestSeamPriorBytes_OpUpdate_NilBothBytes_FailsWithErrMissingPriorBytes verifies case (a):
// an update that supplies no prior deployed bytes at all (both Deployed and DeployedRaw
// are nil) fails with ErrMissingPriorBytes rather than silently deploying a file with
// user keys absent.
func TestSeamPriorBytes_OpUpdate_NilBothBytes_FailsWithErrMissingPriorBytes(t *testing.T) {
	_, err := applyCodexWithOp(agentformat.OpUpdate, nil, nil)
	if err == nil {
		t.Fatal("transform.Apply returned nil error; want ErrMissingPriorBytes for OpUpdate with nil prior bytes")
	}
	if !errors.Is(err, agentformat.ErrMissingPriorBytes) {
		t.Errorf("transform.Apply returned %v; want error wrapping ErrMissingPriorBytes", err)
	}
}

// ---------------------------------------------------------------------------
// T7.2(b): Decoded bytes present but raw prior bytes not threaded fails the same way
// ---------------------------------------------------------------------------

// TestSeamPriorBytes_OpUpdate_DeployedSetButRawNil_FailsWithErrMissingPriorBytes verifies
// case (b): an update where the decoded canonical bytes are present (Deployed set) but the
// raw prior bytes were not threaded through DeployedRaw fails with ErrMissingPriorBytes.
//
// This is the wiring regression the separate DeployedRaw field exists to catch. On Markdown
// harnesses the two forms are identical, so the bug is invisible there; Codex is the format
// under test precisely because it enforces the prior-bytes contract.
func TestSeamPriorBytes_OpUpdate_DeployedSetButRawNil_FailsWithErrMissingPriorBytes(t *testing.T) {
	// Decoded canonical bytes are present, but DeployedRaw is nil (raw channel not threaded).
	deployedCanonical := []byte(codexAgentSource)
	_, err := applyCodexWithOp(agentformat.OpUpdate, deployedCanonical, nil)
	if err == nil {
		t.Fatal("transform.Apply returned nil error; want ErrMissingPriorBytes when " +
			"Deployed is set but DeployedRaw (the raw prior-bytes channel) is nil")
	}
	if !errors.Is(err, agentformat.ErrMissingPriorBytes) {
		t.Errorf("transform.Apply returned %v; want error wrapping ErrMissingPriorBytes", err)
	}
}

// ---------------------------------------------------------------------------
// T7.2(c): Unreadable deployed file fails that artifact only via the content callback
// ---------------------------------------------------------------------------

// TestSeamPriorBytes_ReadErr_FailsArtifactViaCallback verifies case (c): when the
// deployedReader returns a DeployedRead with ReadErr set, buildContent propagates that
// error as the content callback's return value. This models an unreadable deployed file.
//
// This is new behaviour: the old nil-on-error reader was indistinguishable from an absent
// file. I7.2a makes the two outcomes distinguishable; the test asserts the error channel
// now reaches the callback's caller.
func TestSeamPriorBytes_ReadErr_FailsArtifactViaCallback(t *testing.T) {
	agentSrc := []byte("---\nid: 1\nversion: 1.0\nname: test-runner\ndescription: desc\nmodel: m\ntools: []\nrecommended_tier: LOW\ntier_rationale: t\nrequired_skills: []\n---\n\nAgent body.\n")
	const agentSourcePath = "agent-src.md"
	const testAgentKey = "test-runner"

	cat := &seamReplayCatalog{sources: map[string][]byte{agentSourcePath: agentSrc}}

	// Load protocol from frozen catalog so the content path does not fail on a nil protocol.
	frozenRoot := seamReplayFrozenCatalogRoot(t)
	protocol, err := catalog.FileProtocolLoader{}.LoadProtocol(frozenRoot)
	if err != nil {
		t.Fatalf("load protocol: %v", err)
	}

	svc := &service{deps: Deps{
		Catalog: cat,
		Todo:    seamReplayTodo{},
		Now:     func() time.Time { return time.Now() },
	}}

	agentByKey := map[string]domain.Agent{
		testAgentKey: {Key: testAgentKey, SourcePath: agentSourcePath, Role: domain.RoleWorker},
	}

	// deployedReader returns a ReadErr to simulate an unreadable file.
	readErr := errors.New("permission denied: test-induced read error")
	deployedReader := func(item domain.PlanItem) (DeployedRead, agentformat.Operation, error) {
		return DeployedRead{Present: true, ReadErr: readErr}, agentformat.OpUpdate, nil
	}

	// Use the stub harness module (Markdown format, empty AgentFormatID) so the test
	// exercises the error channel without needing the Codex translator.
	contentFn := svc.buildContent(
		&seamReplayStubModule{}, agentByKey, nil,
		nil, nil,
		nil, nil,
		domain.ScopeProject,
		deployedReader,
		"",
		protocol,
		domain.BundleContent{},
		nil,
		nil, // ownedKeyDiffSink
	)

	item := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: testAgentKey},
		TargetPath: testAgentKey + ".md",
		Action:     domain.ActionUpdate,
	}
	_, cbErr := contentFn(item)
	if cbErr == nil {
		t.Fatal("buildContent closure returned nil error; want the ReadErr to propagate through the callback")
	}
	if !errors.Is(cbErr, readErr) {
		t.Errorf("callback error is %v; want it to wrap the injected ReadErr (%v)", cbErr, readErr)
	}
}

// ---------------------------------------------------------------------------
// T7.2(d): Malformed TOML decode fails with ErrMalformedDeployed
// ---------------------------------------------------------------------------

// TestSeamPriorBytes_MalformedTOML_DecodeErrWrapsErrMalformedDeployed verifies case (d):
// when a deployed file reads successfully but its bytes are malformed TOML, Layer 1 of the
// decode funnel returns a DecodeErr wrapping ErrMalformedDeployed.
//
// This outcome is distinct from both "file absent" (Present: false) and "file unreadable"
// (ReadErr set). The test calls decodeDeployedBytes directly to isolate Layer 1.
func TestSeamPriorBytes_MalformedTOML_DecodeErrWrapsErrMalformedDeployed(t *testing.T) {
	desc := &domain.HarnessDescriptor{AgentFormatID: "codex-toml"}
	malformedTOML := []byte("this is not valid TOML !!! [broken")

	read := decodeDeployedBytes(malformedTOML, domain.ArtifactAgent, desc)

	if read.DecodeErr == nil {
		t.Fatal("decodeDeployedBytes returned nil DecodeErr for malformed TOML; want a decode error")
	}
	if !errors.Is(read.DecodeErr, agentformat.ErrMalformedDeployed) {
		t.Errorf("DecodeErr is %v; want it to wrap ErrMalformedDeployed", read.DecodeErr)
	}
	// Raw bytes are set even on decode failure (the read succeeded).
	if len(read.Raw) == 0 {
		t.Error("Raw bytes are empty on a decode failure; they should carry the bytes that were read")
	}
	// Canonical bytes must be nil on decode failure.
	if read.Canonical != nil {
		t.Error("Canonical bytes are non-nil on a decode failure; they must be nil")
	}
}

// ---------------------------------------------------------------------------
// T7.2(e): OpUnspecified on Codex fails with ErrUnspecifiedOperation
// ---------------------------------------------------------------------------

// TestSeamPriorBytes_Codex_OpUnspecified_FailsWithErrUnspecifiedOperation verifies case (e):
// a Codex artifact context with Op: OpUnspecified fails with ErrUnspecifiedOperation.
// OpUnspecified is the zero value and is the value supplied by the ~470 existing call sites
// that pre-date the Op threading; they are safe on Markdown because the Markdown translator
// ignores Op, but Codex rejects it. This is the seam-level regression that confirms the
// translator's raise propagates correctly through the pipeline.
func TestSeamPriorBytes_Codex_OpUnspecified_FailsWithErrUnspecifiedOperation(t *testing.T) {
	_, err := applyCodexWithOp(agentformat.OpUnspecified, nil, nil)
	if err == nil {
		t.Fatal("transform.Apply returned nil error; want ErrUnspecifiedOperation for OpUnspecified on Codex")
	}
	if !errors.Is(err, agentformat.ErrUnspecifiedOperation) {
		t.Errorf("transform.Apply returned %v; want error wrapping ErrUnspecifiedOperation", err)
	}
}

// ---------------------------------------------------------------------------
// T7.2(f): OpCreate with non-nil prior bytes on Codex fails with ErrUnspecifiedOperation
// ---------------------------------------------------------------------------

// TestSeamPriorBytes_Codex_OpCreate_WithPriorBytes_FailsWithErrUnspecifiedOperation
// verifies case (f): a Codex artifact context with Op: OpCreate carrying non-nil prior
// bytes fails with ErrUnspecifiedOperation. A create should carry no prior bytes; the
// presence of prior bytes on a create contradicts the operation and signals a wiring bug.
func TestSeamPriorBytes_Codex_OpCreate_WithPriorBytes_FailsWithErrUnspecifiedOperation(t *testing.T) {
	priorBytes := []byte("prior on-disk bytes that should not be here on a create")
	_, err := applyCodexWithOp(agentformat.OpCreate, nil, priorBytes)
	if err == nil {
		t.Fatal("transform.Apply returned nil error; want ErrUnspecifiedOperation for OpCreate with non-nil prior bytes")
	}
	if !errors.Is(err, agentformat.ErrUnspecifiedOperation) {
		t.Errorf("transform.Apply returned %v; want error wrapping ErrUnspecifiedOperation", err)
	}
}

// ---------------------------------------------------------------------------
// T7.2(g): PlanAction("") yields non-nil error from Layer 3
// ---------------------------------------------------------------------------

// TestSeamPriorBytes_ZeroPlanAction_Layer3ReturnsError verifies case (g): calling
// readDeployedPlanItem with a plan item whose Action is the zero value PlanAction("")
// returns a non-nil error.
//
// The Layer 3 action mapping is a total switch with a failing default. An unrecognised
// PlanAction value -- including the zero value -- must yield an error rather than a
// silent default, so a new action added to the domain without updating the funnel fails
// loudly rather than deploying with the wrong Op.
func TestSeamPriorBytes_ZeroPlanAction_Layer3ReturnsError(t *testing.T) {
	ws := t.TempDir()
	desc := &domain.HarnessDescriptor{} // Markdown; format is irrelevant for this case

	item := domain.PlanItem{
		Action:     domain.PlanAction(""), // the zero value: unrecognised
		TargetPath: "test-agent.md",
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-agent"},
	}

	_, _, err := readDeployedPlanItem(ws, desc, item)
	if err == nil {
		t.Fatal("readDeployedPlanItem returned nil error for PlanAction(\"\"); " +
			"the Layer 3 action mapping must return an error for unrecognised PlanAction values")
	}
}

// ---------------------------------------------------------------------------
// seamReplayStubModule: minimal domain.HarnessModule for the c-case Markdown test
// ---------------------------------------------------------------------------

// seamReplayStubModule satisfies domain.HarnessModule for tests that need a real
// module but do not exercise harness-specific behaviour (e.g. case (c) which tests
// the error channel, not the transform output).
type seamReplayStubModule struct{}

func (seamReplayStubModule) Ref() domain.HarnessRef {
	return domain.HarnessRef{ID: "replay-stub", Tier: domain.TierBuiltin, Usable: true}
}
func (seamReplayStubModule) Descriptor() *domain.HarnessDescriptor {
	// Empty AgentFormatID resolves to the Markdown identity translator.
	return &domain.HarnessDescriptor{ID: "replay-stub"}
}
func (seamReplayStubModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
	}
	return domain.ToolResult{Resolutions: resolutions}, nil
}
func (seamReplayStubModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (seamReplayStubModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + ".md", nil
}
func (seamReplayStubModule) Injection(_ domain.InjectionRequest) (string, bool) { return "", false }
func (seamReplayStubModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}
func (seamReplayStubModule) Close() error { return nil }
