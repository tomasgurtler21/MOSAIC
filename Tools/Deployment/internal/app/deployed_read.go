package app

// deployed_read.go is the home of the deployed-artifact read funnel.
//
// This file is the ONLY place in the application layer permitted to call a
// translator's Decode. Every site that must decode deployed agent bytes routes
// through one of the three layers here.
//
// # Layer summary
//
//   - Layer 1 (decodeDeployedBytes): bytes in hand, no filesystem, no plan item.
//   - Layer 2 (readDeployedArtifact): reads a file from disk, no plan item.
//   - Layer 3 (readDeployedPlanItem): knows about plan items, calls Layer 2.
//
// Skills and hooks pass through undecoded at every layer; only agent artifacts
// are decoded, and always through the translator named by the source harness
// descriptor's AgentFormatID.
//
// # Reporting channel
//
// DeployedRead.Report carries the translator's decode report. Every consumer of
// the funnel MUST surface it; see the Decode report surfacing table in
// ContractsDesign.md for which consumer surfaces it where. The helpers
// mergeDecodeReport and decodeReportNotices perform the translation from
// agentformat vocabulary to the run's reporting vocabulary.
//
// # Read-site inventory
//
// This block inventories every application-layer call site that reads deployed
// agent bytes or branches on a file extension, and records the stage that
// converts each site to funnel-routed reads. Scope: internal/app.
//
// CONVERTED BY THIS STAGE (Stage 7):
//   - readDeployedFile in summary.go: absorbed into readDeployedArtifact (Layer 2).
//     Both callers (the deployedReader closures in update.go and workflow_update.go)
//     now route through readDeployedPlanItem (Layer 3).
//
// CONVERTED BY STAGE 8 (extension-driven read for deployed-state probes, index, source models):
//   - probeDeployedArtifact in deployedstate.go: now routes through readDeployedArtifact
//     (Layer 2). Signature extended with kind and src; callers pass pp.Ref.Kind and
//     module.Descriptor() so the correct decoder is selected per harness.
//   - deployed_agent_index.go: now uses decodeDeployedBytes (Layer 1) via the declared
//     extension. Files not matching the harness-declared extension are skipped.
//   - transform_source_models.go (ReadSourceModel, IndexSourceModels): ReadSourceModel
//     decodes through decodeDeployedBytes before frontmatter parse; IndexSourceModels
//     stores canonical bytes in SourceFile.Content for the retarget batch loop.
//   - promote_service.go (Promote): source is read via readDeployedArtifact (Layer 2);
//     all downstream promote logic receives canonical bytes.
//   - transform_service_impl.go (retarget loop): no change to the loop itself; it already
//     consumed sf.Content canonically. The pre-pass (IndexSourceModels) now stores
//     canonical bytes, so the loop implicitly benefits.
//
// CONVERTED BY STAGE 9 (extension-driven harness-only discovery):
//   - harness_only_discovery.go (scanHarnessOnlyAgents): converted to readDeployedArtifact
//     (Layer 2). The scan now accepts a HarnessDescriptor, filters by the declared agent
//     extension, strips the declared extension in agentKeyFromFileName, and derives the
//     orchestrator exclusion set via mosaicOrchestratorFileNames. Decode report notices are
//     returned as a second value for the caller to surface. Convert count: 1 site.
//
// CONVERTED BY STAGE 10 (harness-only refresh encode):
//   - workspace_agent_scan.go: converted in Stage 8 (extension-driven scan and funnel
//     decode). Harness-only eligibility now runs on canonical bytes.
//   - harness_only_refresh.go: receives decoded canonical bytes from the funnel via
//     buildContent (Stage 7); region and marker inspection operates on the canonical form.
//     The encode step added in Stage 10 converts the refreshed canonical bytes back into
//     the harness's own format (identity for Markdown; Codex TOML for the Codex harness).
//     DeployedRaw (the funnel's raw on-disk bytes) is threaded as the prior-bytes channel
//     so user-owned keys survive. Hashing and change detection remain on encoded bytes.
//     Convert count: 15 read sites in harness_only_refresh.go, all operating on canonical
//     bytes received from the funnel. No pending application-layer entries remain.
//
// SKILLS-ROOT INDEPENDENCE AUDIT (Stage 8, I8.5):
//   None of the sites converted in Stage 8 derive the skills path from the agents path.
//   The Codex harness declares agents at ".codex/agents" and skills at ".agents/skills"
//   (different parent directories). Every site that resolves target paths calls
//   module.TargetPath(domain.TargetPathRequest{...}), which reads from the descriptor's
//   Paths.Agents and Paths.Skills independently. The deployedAgentIndex, workspace scan,
//   and probe sites all receive agentsDir from module.Descriptor().Paths.Agents.Project
//   and never touch the skills path. Audit: clean.
//
// OUT OF SCOPE (not deployed agent reads in the funnel's sense):
//   - internal/harness/injectionfile/deployed.go: parses deployed harness injection
//     files (YAML/Markdown injection content, not agent files). This is not a deployed
//     agent read and is legitimately outside the funnel's scope.
//   - internal/registry/descriptoronly.go: reads harness descriptor files, not deployed
//     agent files.
//   - .md and extension literals outside internal/app: no .agent.md or format-specific
//     extension literals were found in deployed-agent read paths outside internal/app
//     after inspection of the two known candidates above.
//
// Count after Stage 8: 2 pending sites in internal/app (harness_only_discovery.go,
// harness_only_refresh.go); 2 confirmed out-of-scope sites outside internal/app.
//
// GENERIC-SOURCE CATEGORISATION (Stage 11):
//   After the conversion stages the remaining docformat.Parse and docformat.SplitFrontmatter
//   calls in internal/app operate on canonical bytes (already decoded through the funnel).
//   These are categorised as generic-source reads and migrated to ParseGenericSource in
//   parse_generic_source.go. The named operation is the second permitted caller of the
//   raw-bytes docformat entry points alongside this file (deployed_read.go).
//
//   GENERIC-SOURCE (migrated to ParseGenericSource in parse_generic_source.go):
//     - bundle_conformance.go: rawBody bytes from deployedBodies map (canonical after
//       funnel decoding upstream; read to check role and bundle region content).
//     - deployedstate.go (probeDeployedArtifact): canonical bytes from dr.Canonical
//       (funnel-decoded) — reads version stamps, model ID, injection version.
//     - deployedstate.go (extractDeployedInjectionVersion): canonical bytes passed in
//       from probeDeployedArtifact.
//     - deployedstate.go (extractDeployedWorkflows): canonical bytes passed in from
//       probeDeployedArtifact.
//     - deployed_agent_index.go: dr.Canonical after decodeDeployedBytes — reads numeric ID.
//     - harness_only_discovery.go: src (canonical bytes from readDeployedArtifact) —
//       validates harness-only eligibility.
//     - harness_only_refresh.go: req.Deployed (canonical bytes received from the funnel
//       via buildContent in the Update flow) — parses regions for refresh.
//     - promote.go (eligibleHarnessOnly): src from readDeployedArtifact (canonical).
//     - promote.go (buildGenericAgent): in.Source (canonical bytes from promote path).
//     - promote_service.go: src from the promote service (canonical after funnel decode).
//     - protocol_version.go: canonical bytes from probeDeployedArtifact chain.
//     - render_service_impl.go: srcBytes from catalog or path — always a generic-form
//       MOSAIC source file (Markdown+YAML), not a deployed file in native format.
//     - retarget.go (matchHarnessTarget): src from retarget path (canonical).
//     - retarget.go (retargetItem): in.Source (canonical).
//     - transform_source_models.go (ReadSourceModel): dr.Canonical after decodeDeployedBytes
//       — reads model key from canonical form. NOTE: ReadSourceModel is categorised
//       FUNNEL-ROUTED because its primary read (the raw deployed bytes via content
//       parameter) goes through decodeDeployedBytes before the parse. The subsequent
//       ParseGenericSource call is on canonical bytes only, which is safe. Migrating the
//       canonical-bytes parse to ParseGenericSource does not exempt the original deployed
//       read from the guard; that read is already funnel-routed.
//     - workspace_agent_scan.go: canonical bytes from decodeDeployedBytes in the scan.
//
//   FUNNEL-ROUTED (reads through decodeDeployedBytes/readDeployedArtifact/readDeployedPlanItem):
//     - All sites listed in CONVERTED BY STAGE 7, 8, 9, 10 above.
//     - transform_source_models.go (ReadSourceModel, IndexSourceModels): primary deployed
//       read is through decodeDeployedBytes — FUNNEL-ROUTED. The secondary canonical-bytes
//       parse is generic-source.
//
//   bodyStartPos (SplitFrontmatter call) confirmation:
//     - The SplitFrontmatter call in bodyStartPos is on currentBytes = doc.Bytes(), where
//       doc was parsed from req.Deployed (canonical bytes from the funnel). By the time
//       insertTopLevelDeployedRegion is called, currentBytes carries only canonical
//       Markdown+YAML bytes. VERDICT: canonical bytes confirmed. bodyStartPos has been
//       moved to parse_generic_source.go where it is a permitted caller.
//
// Count after Stage 11: 0 pending application-layer sites. Every site is either
// funnel-routed or generic-source, and both categories are enforced by the read-boundary
// guard rule in tools/importcheck/main.go.

import (
	"fmt"
	"os"
	"path/filepath"

	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// DeployedRead is the funnel's single result value. Raw and Canonical are returned
// together so that a caller cannot obtain one without the other and cannot accidentally
// thread only the decoded form.
//
// Outcomes:
//   - absent (a create):    Present false, ReadErr nil,  Canonical nil, DecodeErr nil, Report zero
//   - unreadable:           Present true,  ReadErr set,  Canonical nil, DecodeErr nil, Report zero
//   - corrupt (bad decode): Present true,  ReadErr nil,  Canonical nil, DecodeErr set, Report zero
//   - ok:                   Present true,  ReadErr nil,  Canonical set, DecodeErr nil, Report the translator's decode report
//
// The two error channels (ReadErr and DecodeErr) never both carry a failure.
type DeployedRead struct {
	// Raw is the bytes exactly as read from disk, in the harness's own format.
	// Nil when the file is absent or unreadable.
	Raw []byte

	// Canonical is the decoded canonical bytes (Markdown with YAML frontmatter).
	// Nil when DecodeErr is set, or when the file is absent or unreadable.
	// For Markdown harnesses, Canonical equals Raw (the identity decode).
	Canonical []byte

	// Present is false only when the file does not exist on disk.
	// It is NOT set to false for unreadable or undecodable files.
	Present bool

	// ReadErr is set when the file exists on disk but could not be read.
	// It is distinct from a missing file (Present false) and from a decode error.
	ReadErr error

	// DecodeErr is set when the file was read successfully but its bytes could not
	// be decoded. Callers classify with errors.Is(DecodeErr, agentformat.ErrMalformedDeployed).
	// Non-conflict plan items must fail the artifact on a non-nil DecodeErr;
	// conflict items resolved by overwrite must proceed (see ContractsDesign.md).
	DecodeErr error

	// Report is the DECODE report, exactly as the translator returned it.
	// It is always a value (never a pointer), and an empty Entries slice is the
	// normal case: for a Markdown harness it is always empty.
	// Set only on the ok outcome; absent, unreadable or undecodable files leave it zero.
	// Consumers MUST surface it; see "Decode report surfacing" in ContractsDesign.md.
	Report agentformat.Report
}

// decodeDeployedBytes is Layer 1 of the decode funnel.
//
// It takes raw bytes already in hand, the artifact kind, and the source harness
// descriptor. It returns a DeployedRead with both the raw bytes and the decoded
// canonical bytes (or a named decode error). No filesystem access; no plan item.
//
// This is the ONLY place in the application layer that calls a translator's Decode.
// It resolves the translator with agentformat.LookupString(src.AgentFormatID) and
// never with formatid.Parse followed by Lookup (CD-4).
//
// Skills and hooks pass through undecoded: their DeployedRead has Raw == Canonical == raw.
func decodeDeployedBytes(raw []byte, kind domain.ArtifactKind, src *domain.HarnessDescriptor) DeployedRead {
	if kind != domain.ArtifactAgent {
		// Skills and hooks are not decoded; Raw and Canonical are the same bytes.
		return DeployedRead{Raw: raw, Canonical: raw, Present: len(raw) > 0}
	}

	// Guard: a nil descriptor is treated as a zero-value descriptor (Markdown identity
	// harness). Both buildDeployedAgentIndex and scanWorkspaceAgents document that a nil
	// src is treated as Markdown; this guard enforces that at the decode boundary so
	// callers cannot panic by passing nil through to src.AgentFormatID.
	if src == nil {
		src = &domain.HarnessDescriptor{}
	}

	t, err := agentformat.LookupString(src.AgentFormatID)
	if err != nil {
		// A format ID that fails to resolve is a descriptor defect. Surface it as a
		// decode error with the format ID problem so the caller can fail the artifact.
		return DeployedRead{Raw: raw, Present: len(raw) > 0, DecodeErr: err}
	}

	canonical, report, err := t.Decode(raw, agentformat.ArtifactContext{
		Kind: kind,
	})
	if err != nil {
		return DeployedRead{Raw: raw, Present: len(raw) > 0, DecodeErr: err}
	}
	return DeployedRead{
		Raw:       raw,
		Canonical: canonical,
		Present:   true,
		Report:    report,
	}
}

// readDeployedArtifact is Layer 2 of the decode funnel.
//
// It reads a file at the given path, then delegates to decodeDeployedBytes (Layer 1).
// It adds the presence flag and a read error channel on top of Layer 1's result.
// No plan item; this is the entry point that Stages 8, 9 and 10 use for all deployed-
// agent reads that hold no plan item.
//
// Outcomes:
//   - File absent:     DeployedRead{Present: false}
//   - File unreadable: DeployedRead{Present: true, ReadErr: <error>}
//   - Decode failure:  DeployedRead{Present: true, Raw: <bytes>, DecodeErr: <error>}
//   - OK:              DeployedRead{Present: true, Raw: <bytes>, Canonical: <bytes>, Report: <report>}
func readDeployedArtifact(path string, kind domain.ArtifactKind, src *domain.HarnessDescriptor) DeployedRead {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DeployedRead{Present: false}
		}
		// File exists but could not be read: present, with a read error.
		return DeployedRead{Present: true, ReadErr: err}
	}
	// File was read; delegate decode to Layer 1.
	result := decodeDeployedBytes(data, kind, src)
	result.Present = true
	return result
}

// readDeployedPlanItem is Layer 3 of the decode funnel.
//
// It resolves the target path and artifact kind from the plan item, calls
// readDeployedArtifact (Layer 2), and maps PlanItem.Action to an agentformat.Operation.
// This is the only layer that knows about plan items; nothing below it may take one.
//
// The action mapping is total over the four defined PlanAction values:
//
//	ActionCreate     ("create")           -> OpCreate
//	ActionUpdate     ("update")           -> OpUpdate
//	ActionUnchanged  ("unchanged")        -> OpUpdate
//	ActionConflict   ("locally-modified") -> OpUpdate
//
// An unrecognised PlanAction value (including the zero value PlanAction("")) yields a
// non-nil error. This is the safety net against a new action being added to the domain
// without updating the funnel.
//
// The third return is plan-item-level only. It is non-nil exactly when the plan item
// cannot be turned into a read at all (an unrecognised Action). Every outcome of
// actually touching the file (absent, unreadable, undecodable, ok) lands inside
// DeployedRead and leaves this error nil. The two channels never both carry a failure.
func readDeployedPlanItem(workspace string, desc *domain.HarnessDescriptor, item domain.PlanItem) (DeployedRead, agentformat.Operation, error) {
	// Map PlanItem.Action to agentformat.Operation.
	// This switch must be exhaustive: a new PlanAction value must be added here.
	var op agentformat.Operation
	switch item.Action {
	case domain.ActionCreate:
		op = agentformat.OpCreate
	case domain.ActionUpdate, domain.ActionUnchanged, domain.ActionConflict:
		op = agentformat.OpUpdate
	default:
		return DeployedRead{}, agentformat.OpUnspecified,
			fmt.Errorf("readDeployedPlanItem: unrecognised PlanAction %q for item %q", item.Action, item.TargetPath)
	}

	path := filepath.Join(workspace, item.TargetPath)
	read := readDeployedArtifact(path, item.Ref.Kind, desc)
	return read, op, nil
}

// mergeDecodeReport appends dr's entries to dst using the entry-kind mapping table
// from the transform pipeline contract. It is additive: it never clears, re-orders
// or de-duplicates what dst already holds. Calling it twice for one artifact duplicates
// the entries, so each consumer calls it exactly once, at the named point.
//
// This is the single place the decode-side vocabulary is translated into the run's
// reporting vocabulary; a consumer that formats agentformat entries itself is a second
// rendering of the same facts and is forbidden.
func mergeDecodeReport(dst *transform.Report, dr agentformat.Report) {
	for _, entry := range dr.Entries {
		switch entry.Kind {
		case agentformat.EntryMissingInstructions:
			// decode-only: a deployed file carried no developer_instructions.
			// Not a FieldChange (the agent body is not a frontmatter key).
			// No existing gap kind fits exactly; surfaced as a manual-step gap so
			// it reaches the TODO output and is visible to the user.
			dst.Gaps = append(dst.Gaps, domain.Gap{
				Kind:   domain.GapManualStep,
				Detail: entry.Detail,
			})
		case agentformat.EntryDroppedForeignKey:
			dst.Fields = append(dst.Fields, transform.FieldChange{
				Key:    entry.Key,
				Before: entry.Detail,
				After:  "",
				Reason: entry.Reason,
			})
		case agentformat.EntryOverriddenName:
			dst.Fields = append(dst.Fields, transform.FieldChange{
				Key:    entry.Key,
				Before: entry.Detail,
				Reason: entry.Reason,
			})
		case agentformat.EntryAppliedFallback:
			dst.Fields = append(dst.Fields, transform.FieldChange{
				Key:    entry.Key,
				After:  entry.Detail,
				Reason: entry.Reason,
			})
		case agentformat.EntryStrippedContainer:
			dst.Fields = append(dst.Fields, transform.FieldChange{
				Key:    entry.Key,
				Before: entry.Detail,
				After:  "",
				Reason: entry.Reason,
			})
		case agentformat.EntryCarriedContainer:
			// Nothing: the key was re-emitted, so no field changed.
		case agentformat.EntryRefusedMarker:
			dst.Fields = append(dst.Fields, transform.FieldChange{
				Key:    entry.Key,
				After:  entry.Detail,
				Reason: entry.Reason,
			})
		case agentformat.EntryDroppedComment:
			// A free-standing comment that did not travel. Not a FieldChange.
			// No existing gap kind fits; leave for a later reporting stage.
		}
	}
}

// decodeReportNotices renders dr for a consumer that has no transform.Report to merge
// into. One notice string per entry, each naming the artifact key and the entry key.
// Same vocabulary as mergeDecodeReport, different sink.
func decodeReportNotices(agentKey string, dr agentformat.Report) []string {
	var notices []string
	for _, entry := range dr.Entries {
		switch entry.Kind {
		case agentformat.EntryMissingInstructions:
			notices = append(notices, fmt.Sprintf(
				"agent %q: deployed file carries no developer_instructions (body is empty)",
				agentKey,
			))
		case agentformat.EntryDroppedForeignKey:
			notices = append(notices, fmt.Sprintf(
				"agent %q: key %q was dropped during decode (%s)",
				agentKey, entry.Key, entry.Reason,
			))
		case agentformat.EntryCarriedContainer:
			// Nothing: the key was re-emitted.
		case agentformat.EntryDroppedComment:
			notices = append(notices, fmt.Sprintf(
				"agent %q: comment was dropped during decode: %s",
				agentKey, entry.Detail,
			))
		default:
			notices = append(notices, fmt.Sprintf(
				"agent %q: decode notice [%s] key=%q: %s",
				agentKey, entry.Kind, entry.Key, entry.Reason,
			))
		}
	}
	return notices
}
