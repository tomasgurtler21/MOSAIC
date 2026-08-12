package transform_test

// injection_rename_test.go covers the injection rename map: a mechanism that forwards
// content stored in the deployed file under an injection's old name into the region the
// source declares under its new name, preventing silent content loss when a source
// [[INJECTION:]] region is renamed.
//
// Behaviour specified:
//
//   - Content stored in the deployed file under a renamed injection's old name lands in
//     the output's new region, byte-identically, at the source-declared position.
//
//   - A migrated region produces the RegionMigrated action. No orphan outcome and no
//     GapRemovedInjection gap are produced for the old name.
//
//   - When both the old and the new name are present in the deployed file, the new name's
//     content wins; the old name's content is emitted as a GapRemovedInjection so the
//     user can recover it. This precedence is deterministic and pinned.
//
//   - When only the new name is present in the deployed file, the rename map entry is a
//     no-op: the region is preserved normally (RegionPreserved), not RegionMigrated.
//
//   - An ambiguous rename table (duplicate Old, or a mapping chain) is a hard error at
//     execution time. The error wraps ErrRenameMapConflict and names the conflicting entries.
//     The transform produces no output when the table is conflicting.
//
//   - A name absent from the rename map is unaffected by rename logic.
//
//   - A genuinely removed injection — absent from both the source and the rename map —
//     still produces RegionOrphaned and GapRemovedInjection, unchanged.

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Shared fixture documents for injection rename tests.
//
// renameSource is a source document with one project injection under the new name
// "NewCapabilityDesc". The old name "CapabilityDesc" no longer appears in any source
// file; it exists only in already-deployed files.
// ---------------------------------------------------------------------------

const renameSource = `---
id: 800
version: 2.0.0
name: rename-test
description: Agent for injection rename testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: rename testing
required_skills: []
---

[[SECTION:Capabilities]]
# RenameTest Agent

[[INJECTION:NewCapabilityDesc]]
[[/INJECTION:NewCapabilityDesc]]
[[/SECTION:Capabilities]]
`

// renameDeployedOldName is the deployed predecessor of renameSource.
// It was deployed before the injection was renamed: the region is still stored as
// [[INJECTION:CapabilityDesc]]. With a rename entry {Old:"CapabilityDesc", New:"NewCapabilityDesc"},
// the content must reach the source-declared region NewCapabilityDesc in the output.
const renameDeployedOldName = `---
id: 800
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for injection rename testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Capabilities]]
# RenameTest Agent

[[INJECTION:CapabilityDesc]]
This is the user-authored capability description stored under the old injection name.
It must land in NewCapabilityDesc after rename resolution, byte-identically.
[[/INJECTION:CapabilityDesc]]
[[/SECTION:Capabilities]]
`

// renameDeployedBothNames is a deployed file that contains BOTH the old name
// "CapabilityDesc" and the new name "NewCapabilityDesc" with different content.
// Declaration order: old name first, then new name.
// The new name's content must win; the old name's content must appear in a GapRemovedInjection.
const renameDeployedBothNames = `---
id: 800
version: 1.5.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for injection rename precedence testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Capabilities]]
# RenameTest Agent

[[INJECTION:CapabilityDesc]]
OLD-NAME-CONTENT: content stored under the old injection name.
This must not win; it must appear only in the GapRemovedInjection fragment.
[[/INJECTION:CapabilityDesc]]

[[INJECTION:NewCapabilityDesc]]
NEW-NAME-CONTENT: content stored under the new injection name.
This is the winning content; it must appear in the output region.
[[/INJECTION:NewCapabilityDesc]]
[[/SECTION:Capabilities]]
`

// renameDeployedBothNamesReversed is a deployed file that contains BOTH the old name
// "CapabilityDesc" and the new name "NewCapabilityDesc" with different content, but with
// the declaration order reversed relative to renameDeployedBothNames: the new name is
// declared first, the old name second. Precedence must not depend on declaration order —
// the new name must still win regardless of which one appears first in the document.
const renameDeployedBothNamesReversed = `---
id: 800
version: 1.5.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for injection rename precedence testing (reversed declaration order)
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Capabilities]]
# RenameTest Agent

[[INJECTION:NewCapabilityDesc]]
NEW-NAME-CONTENT: content stored under the new injection name (declared first this time).
This is the winning content; it must appear in the output region regardless of order.
[[/INJECTION:NewCapabilityDesc]]

[[INJECTION:CapabilityDesc]]
OLD-NAME-CONTENT: content stored under the old injection name (declared second this time).
This must not win; it must appear only in the GapRemovedInjection fragment.
[[/INJECTION:CapabilityDesc]]
[[/SECTION:Capabilities]]
`

// renameDeployedNewNameOnly is a deployed file that contains ONLY the new name
// "NewCapabilityDesc". The rename entry is present in the table, but OldName is absent
// from the deployed file — there is nothing to migrate. The rename map must be a no-op.
const renameDeployedNewNameOnly = `---
id: 800
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for rename no-op testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Capabilities]]
# RenameTest Agent

[[INJECTION:NewCapabilityDesc]]
NOOP-CONTENT: content already stored under the new injection name.
The rename entry exists but the old name is absent; this must be a normal preserve.
[[/INJECTION:NewCapabilityDesc]]
[[/SECTION:Capabilities]]
`

// renameAndOrphanSource is a source with one injection (NewName) and no
// RemovedInjection. With a rename entry {Old:"OldName", New:"NewName"}, the deployed
// OldName content migrates to NewName. RemovedInjection has no rename and is absent
// from the source — it must still orphan as before.
const renameAndOrphanSource = `---
id: 801
version: 2.0.0
name: rename-orphan-test
description: Agent for rename-plus-orphan testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: rename orphan testing
required_skills: []
---

[[SECTION:Identity]]
# RenameOrphanTest Agent

[[INJECTION:NewName]]
[[/INJECTION:NewName]]
[[/SECTION:Identity]]
`

// renameAndOrphanDeployed is the deployed predecessor of renameAndOrphanSource.
// OldName carries user content and maps to NewName via the rename table.
// RemovedInjection carries user content and has no rename entry — it must orphan.
// UnmappedInjection is present in both the deployed file and the source (as NewName is
// unrelated to it), and also carries content; it is unaffected by rename logic.
const renameAndOrphanDeployed = `---
id: 801
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for rename-plus-orphan testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# RenameOrphanTest Agent

[[INJECTION:OldName]]
MIGRATE-ME: content under the old name, must reach NewName in the output.
[[/INJECTION:OldName]]
[[/SECTION:Identity]]

[[INJECTION:RemovedInjection]]
ORPHAN-ME: content under a removed injection with no rename entry.
This must produce RegionOrphaned and GapRemovedInjection, unchanged.
[[/INJECTION:RemovedInjection]]
`

// unmappedSource is a source where UnmappedInjection is present normally. No rename
// touches this name — the rename table is present but does not mention UnmappedInjection.
const unmappedSource = `---
id: 802
version: 2.0.0
name: unmapped-test
description: Agent for unmapped injection testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: unmapped testing
required_skills: []
---

[[SECTION:Identity]]
# UnmappedTest Agent

[[INJECTION:UnmappedInjection]]
[[/INJECTION:UnmappedInjection]]
[[/SECTION:Identity]]
`

// unmappedDeployed has UnmappedInjection with content. The rename table maps some other
// name; UnmappedInjection is untouched.
const unmappedDeployed = `---
id: 802
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for unmapped injection testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# UnmappedTest Agent

[[INJECTION:UnmappedInjection]]
UNMAPPED-CONTENT: content under an injection not in the rename table.
The rename table must not alter this region in any way.
[[/INJECTION:UnmappedInjection]]
[[/SECTION:Identity]]
`

// renameEntry returns a single-entry rename slice mapping Old to New.
func renameEntry(old, new string) []transform.RenameEntry {
	return []transform.RenameEntry{{Old: old, New: new}}
}

// ---------------------------------------------------------------------------
// Content migration: deployed content under the old name lands in the new region
// ---------------------------------------------------------------------------

// TestRename_OldNameContent_LandsInNewRegionByteIdentically asserts that content stored
// in the deployed file under a renamed injection's old name lands in the output's new
// region with byte-identical content, at the source-declared position.
func TestRename_OldNameContent_LandsInNewRegionByteIdentically(t *testing.T) {
	req := transform.Request{
		Source:           []byte(renameSource),
		Deployed:         []byte(renameDeployedOldName),
		Kind:             domain.ArtifactAgent,
		Key:              "rename-test",
		Module:           newFixtureModule(t),
		Model:            fixtureModel(),
		Scope:            domain.ScopeProject,
		InjectionRenames: renameEntry("CapabilityDesc", "NewCapabilityDesc"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The new region must appear in the output at the source-declared position.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	outNode, ok := outDoc.Body().Injection("NewCapabilityDesc")
	if !ok {
		t.Fatal("NewCapabilityDesc injection absent from output; content stored under the old name must land in the new region after rename resolution")
	}

	// Content must be byte-identical to what was stored under the old name in the deployed file.
	depDoc, err := docformat.Parse([]byte(renameDeployedOldName))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	depNode, depOK := depDoc.Body().Injection("CapabilityDesc")
	if !depOK {
		t.Fatal("CapabilityDesc not found in deployed fixture; fixture is malformed")
	}

	if !bytes.Equal(outNode.Content(), depNode.Content()) {
		t.Errorf("NewCapabilityDesc content is not byte-identical to deployed CapabilityDesc:\ndeployed:  %q\noutput:    %q",
			depNode.Content(), outNode.Content())
	}

	// The old name must not appear in the output (it was renamed, not orphaned).
	_, oldPresent := outDoc.Body().Injection("CapabilityDesc")
	if oldPresent {
		t.Error("old injection name CapabilityDesc must not appear in the output; content was migrated to NewCapabilityDesc")
	}
}

// TestRename_NewRegion_IsAtSourceDeclaredPosition asserts that the migration places the
// content at the position the source declares for the new injection name, not at some
// other position derived from the deployed file.
func TestRename_NewRegion_IsAtSourceDeclaredPosition(t *testing.T) {
	req := transform.Request{
		Source:           []byte(renameSource),
		Deployed:         []byte(renameDeployedOldName),
		Kind:             domain.ArtifactAgent,
		Key:              "rename-test",
		Module:           newFixtureModule(t),
		Model:            fixtureModel(),
		Scope:            domain.ScopeProject,
		InjectionRenames: renameEntry("CapabilityDesc", "NewCapabilityDesc"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	outNode, ok := outDoc.Body().Injection("NewCapabilityDesc")
	if !ok {
		t.Fatal("NewCapabilityDesc absent from output")
	}

	// Source declares NewCapabilityDesc inside the Capabilities section.
	// After migration the region must remain inside that section.
	parent := outNode.Parent()
	if parent == nil {
		t.Fatal("NewCapabilityDesc is at body top level in the output; " +
			"the source declares it inside Capabilities, so it must be nested there")
	}
	if parent.Kind() != docformat.NodeSection {
		t.Errorf("NewCapabilityDesc parent kind: want NodeSection, got %q", parent.Kind())
	}
	if parent.Name() != "Capabilities" {
		t.Errorf("NewCapabilityDesc parent section: want %q, got %q", "Capabilities", parent.Name())
	}
}

// ---------------------------------------------------------------------------
// Migration outcome: RegionMigrated reported; no orphan and no removal gap
// ---------------------------------------------------------------------------

// TestRename_MigratedRegion_ReportsMigratedAction asserts that a region filled through
// rename resolution reports the RegionMigrated action in RegionOutcome, not RegionPreserved.
func TestRename_MigratedRegion_ReportsMigratedAction(t *testing.T) {
	req := transform.Request{
		Source:           []byte(renameSource),
		Deployed:         []byte(renameDeployedOldName),
		Kind:             domain.ArtifactAgent,
		Key:              "rename-test",
		Module:           newFixtureModule(t),
		Model:            fixtureModel(),
		Scope:            domain.ScopeProject,
		InjectionRenames: renameEntry("CapabilityDesc", "NewCapabilityDesc"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "NewCapabilityDesc" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("RegionOutcome for NewCapabilityDesc absent from report; report: %v", result.Report.Regions)
	}
	if outcome.Action != transform.RegionMigrated {
		t.Errorf("NewCapabilityDesc action: want %q (RegionMigrated), got %q; "+
			"a region filled through rename resolution must be reported with the migration action",
			transform.RegionMigrated, outcome.Action)
	}
	if outcome.Action == transform.RegionPreserved {
		t.Errorf("NewCapabilityDesc action must not be RegionPreserved; "+
			"migration and normal preservation are distinct outcomes")
	}
}

// TestRename_MigratedRegion_NoOrphanOutcome asserts that no RegionOrphaned outcome is
// produced for either the old or the new injection name when rename resolution succeeds.
// The old name never surfaces as an orphan — it was resolved, not removed.
func TestRename_MigratedRegion_NoOrphanOutcome(t *testing.T) {
	req := transform.Request{
		Source:           []byte(renameSource),
		Deployed:         []byte(renameDeployedOldName),
		Kind:             domain.ArtifactAgent,
		Key:              "rename-test",
		Module:           newFixtureModule(t),
		Model:            fixtureModel(),
		Scope:            domain.ScopeProject,
		InjectionRenames: renameEntry("CapabilityDesc", "NewCapabilityDesc"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, o := range result.Report.Regions {
		if o.Action == transform.RegionOrphaned {
			t.Errorf("unexpected RegionOrphaned outcome for %q; "+
				"a renamed injection must not produce an orphan — rename resolution prevents orphaning",
				o.Name)
		}
	}
}

// TestRename_MigratedRegion_NoRemovedInjectionGap asserts that no GapRemovedInjection gap
// is produced for the old injection name when the content was successfully migrated to the
// new name. Emitting both a migration and a removal gap would double-count the same content.
func TestRename_MigratedRegion_NoRemovedInjectionGap(t *testing.T) {
	req := transform.Request{
		Source:           []byte(renameSource),
		Deployed:         []byte(renameDeployedOldName),
		Kind:             domain.ArtifactAgent,
		Key:              "rename-test",
		Module:           newFixtureModule(t),
		Model:            fixtureModel(),
		Scope:            domain.ScopeProject,
		InjectionRenames: renameEntry("CapabilityDesc", "NewCapabilityDesc"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapRemovedInjection && g.Subject == "CapabilityDesc" {
			t.Errorf("GapRemovedInjection must not be emitted for the old injection name CapabilityDesc "+
				"when content was migrated to NewCapabilityDesc; gap: %+v", g)
		}
	}
}

// ---------------------------------------------------------------------------
// Precedence when both old and new names are present in the deployed file
// ---------------------------------------------------------------------------

// TestRename_BothNamesPresent_NewNameContentWins asserts that when the deployed file
// contains both the old and the new injection name, the new name's content is used in
// the output. The precedence rule is: new name wins; old name is treated as superseded.
func TestRename_BothNamesPresent_NewNameContentWins(t *testing.T) {
	req := transform.Request{
		Source:           []byte(renameSource),
		Deployed:         []byte(renameDeployedBothNames),
		Kind:             domain.ArtifactAgent,
		Key:              "rename-test",
		Module:           newFixtureModule(t),
		Model:            fixtureModel(),
		Scope:            domain.ScopeProject,
		InjectionRenames: renameEntry("CapabilityDesc", "NewCapabilityDesc"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	outNode, ok := outDoc.Body().Injection("NewCapabilityDesc")
	if !ok {
		t.Fatal("NewCapabilityDesc absent from output")
	}

	// The output must carry the new-name content, not the old-name content.
	if !bytes.Contains(outNode.Content(), []byte("NEW-NAME-CONTENT")) {
		t.Errorf("NewCapabilityDesc output content must come from the new name's deployed entry; "+
			"got: %q", outNode.Content())
	}
	if bytes.Contains(outNode.Content(), []byte("OLD-NAME-CONTENT")) {
		t.Errorf("NewCapabilityDesc output content must not contain the old name's content; "+
			"the new name wins when both are present. Got: %q", outNode.Content())
	}

	// When both names are present, the new name is found directly in the deployed file and
	// wins via its own existing entry — this is RegionPreserved, not RegionMigrated. A
	// correct implementation must distinguish "new name already present" from "old name
	// migrated to new name"; they are different code paths.
	var newNameOutcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "NewCapabilityDesc" {
			newNameOutcome = &result.Report.Regions[i]
			break
		}
	}
	if newNameOutcome == nil {
		t.Fatal("RegionOutcome for NewCapabilityDesc absent from report; " +
			"the new name must always appear in the region report when it is in both source and deployed")
	}
	if newNameOutcome.Action == transform.RegionMigrated {
		t.Errorf("NewCapabilityDesc action must not be RegionMigrated when both old and new names are present; "+
			"the new name wins via its own existing entry, not via migration from the old name. Got: %q",
			newNameOutcome.Action)
	}
	if newNameOutcome.Action != transform.RegionPreserved {
		t.Errorf("NewCapabilityDesc action: want %q (RegionPreserved), got %q; "+
			"when the new name is already present in the deployed file, its content is preserved normally, "+
			"not migrated", transform.RegionPreserved, newNameOutcome.Action)
	}

	// The old name must not produce RegionOrphaned. When the rename map is consulted and both
	// names are present, the old name is recognized as superseded — it is consumed by the rename
	// resolution path and must not fall through to the orphan path. Without rename-aware
	// map-building, CapabilityDesc would be absent from the source and would orphan normally.
	for _, o := range result.Report.Regions {
		if o.Name == "CapabilityDesc" && o.Action == transform.RegionOrphaned {
			t.Errorf("CapabilityDesc must not produce RegionOrphaned when both old and new names are "+
				"present; the rename map recognizes CapabilityDesc as a superseded old key whose content "+
				"goes to GapRemovedInjection, not to the orphan path")
		}
	}
}

// TestRename_BothNamesPresent_OldNameContentInRemovalGap asserts that when both names
// are present in the deployed file and the new name wins, the old name's content is not
// silently discarded — it is emitted as a GapRemovedInjection so the user can recover it.
func TestRename_BothNamesPresent_OldNameContentInRemovalGap(t *testing.T) {
	req := transform.Request{
		Source:           []byte(renameSource),
		Deployed:         []byte(renameDeployedBothNames),
		Kind:             domain.ArtifactAgent,
		Key:              "rename-test",
		Module:           newFixtureModule(t),
		Model:            fixtureModel(),
		Scope:            domain.ScopeProject,
		InjectionRenames: renameEntry("CapabilityDesc", "NewCapabilityDesc"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var removalGap *domain.Gap
	for i := range result.Report.Gaps {
		if result.Report.Gaps[i].Kind == domain.GapRemovedInjection &&
			result.Report.Gaps[i].Subject == "CapabilityDesc" {
			removalGap = &result.Report.Gaps[i]
			break
		}
	}
	if removalGap == nil {
		t.Fatalf("GapRemovedInjection for CapabilityDesc must be emitted when both old and new names "+
			"are present in the deployed file (old-name content must not be silently discarded); "+
			"gaps: %v", result.Report.Gaps)
	}
	if !strings.Contains(removalGap.Fragment, "OLD-NAME-CONTENT") {
		t.Errorf("GapRemovedInjection.Fragment for CapabilityDesc must contain the old-name content "+
			"so the user can recover it; got: %q", removalGap.Fragment)
	}

	// NewCapabilityDesc must be reported as RegionPreserved, not RegionMigrated. The new name
	// was already present in the deployed file and wins via its own entry — it is preserved
	// normally. An implementation that incorrectly reports the new name as RegionMigrated when
	// both names are present would have misidentified which code path ran.
	var newNameOutcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "NewCapabilityDesc" {
			newNameOutcome = &result.Report.Regions[i]
			break
		}
	}
	if newNameOutcome == nil {
		t.Fatal("RegionOutcome for NewCapabilityDesc absent from report")
	}
	if newNameOutcome.Action != transform.RegionPreserved {
		t.Errorf("NewCapabilityDesc action: want %q (RegionPreserved), got %q; "+
			"when both names are present the new name's existing entry wins without migration",
			transform.RegionPreserved, newNameOutcome.Action)
	}

	// CapabilityDesc must not produce RegionOrphaned. The rename map marks it as a superseded
	// old key; the orphan path must not run for it. Without rename-aware map-building, the
	// existing logic would orphan CapabilityDesc (absent from source), which would make the
	// GapRemovedInjection assertion above pass coincidentally via the orphan gap rather than
	// via the rename-resolution gap.
	for _, o := range result.Report.Regions {
		if o.Name == "CapabilityDesc" && o.Action == transform.RegionOrphaned {
			t.Errorf("CapabilityDesc must not produce RegionOrphaned when both old and new names "+
				"are present; the rename map consumes it before the orphan path sees it")
		}
	}
}

// TestRename_BothNamesPresent_Deterministic asserts that calling Apply twice with the same
// inputs produces the same outcome when both the old and new names are present. The
// precedence rule must not depend on map iteration order or any other non-determinism.
func TestRename_BothNamesPresent_Deterministic(t *testing.T) {
	req := transform.Request{
		Source:           []byte(renameSource),
		Deployed:         []byte(renameDeployedBothNames),
		Kind:             domain.ArtifactAgent,
		Key:              "rename-test",
		Module:           newFixtureModule(t),
		Model:            fixtureModel(),
		Scope:            domain.ScopeProject,
		InjectionRenames: renameEntry("CapabilityDesc", "NewCapabilityDesc"),
	}

	r1, err1 := transform.Apply(req)
	r2, err2 := transform.Apply(req)

	if err1 != nil || err2 != nil {
		t.Fatalf("Apply errors: first=%v second=%v", err1, err2)
	}
	if !bytes.Equal(r1.Output, r2.Output) {
		t.Error("Apply with both old and new names present is non-deterministic: " +
			"two identical calls produced different output bytes")
	}
}

// TestRename_BothNamesPresent_ReversedOrder_NewNameStillWins asserts that the precedence
// rule ("new name wins") is independent of the declaration order in the deployed file. The
// renameDeployedBothNamesReversed fixture declares NewCapabilityDesc before CapabilityDesc,
// which is the opposite of renameDeployedBothNames. The outcome must be identical: the new
// name's content wins and the old name's content appears in a GapRemovedInjection.
//
// A naive last-write-wins implementation that builds the deployed region map by iterating
// document order would pass renameDeployedBothNames (old declared first → new overwrites it)
// but fail this test (new declared first → old overwrites it, wrong winner). This test pins
// the order-independence property explicitly, closing the risk flagged in Plan.md's Risks table.
func TestRename_BothNamesPresent_ReversedOrder_NewNameStillWins(t *testing.T) {
	req := transform.Request{
		Source:           []byte(renameSource),
		Deployed:         []byte(renameDeployedBothNamesReversed),
		Kind:             domain.ArtifactAgent,
		Key:              "rename-test",
		Module:           newFixtureModule(t),
		Model:            fixtureModel(),
		Scope:            domain.ScopeProject,
		InjectionRenames: renameEntry("CapabilityDesc", "NewCapabilityDesc"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	outNode, ok := outDoc.Body().Injection("NewCapabilityDesc")
	if !ok {
		t.Fatal("NewCapabilityDesc absent from output; the new name must win regardless of declaration order")
	}

	// The new name's content must win even though it was declared first in this fixture.
	if !bytes.Contains(outNode.Content(), []byte("NEW-NAME-CONTENT")) {
		t.Errorf("NewCapabilityDesc output content must come from the new name's entry even when it "+
			"is declared before the old name in the deployed file; got: %q", outNode.Content())
	}
	if bytes.Contains(outNode.Content(), []byte("OLD-NAME-CONTENT")) {
		t.Errorf("NewCapabilityDesc must not contain the old name's content even when the old name "+
			"is declared after the new name; a last-write-wins map building would incorrectly let "+
			"the old name overwrite the new name here. Got: %q", outNode.Content())
	}

	// The old name's content must appear in a GapRemovedInjection regardless of order.
	var removalGap *domain.Gap
	for i := range result.Report.Gaps {
		if result.Report.Gaps[i].Kind == domain.GapRemovedInjection &&
			result.Report.Gaps[i].Subject == "CapabilityDesc" {
			removalGap = &result.Report.Gaps[i]
			break
		}
	}
	if removalGap == nil {
		t.Fatalf("GapRemovedInjection for CapabilityDesc must be emitted in the reversed-order fixture; "+
			"the old name's content must not be silently discarded regardless of declaration order. "+
			"Gaps: %v", result.Report.Gaps)
	}
	if !strings.Contains(removalGap.Fragment, "OLD-NAME-CONTENT") {
		t.Errorf("GapRemovedInjection.Fragment for CapabilityDesc must contain the old-name content; "+
			"got: %q", removalGap.Fragment)
	}

	// CapabilityDesc must not produce RegionOrphaned — it is a superseded old key consumed by
	// the rename resolution path. Without rename-aware map-building, it would orphan (absent
	// from source), and the GapRemovedInjection assertion above would pass coincidentally via
	// the orphan gap rather than via the rename-resolution gap.
	for _, o := range result.Report.Regions {
		if o.Name == "CapabilityDesc" && o.Action == transform.RegionOrphaned {
			t.Errorf("CapabilityDesc must not produce RegionOrphaned in the reversed-order fixture; "+
				"rename resolution must recognize it as a superseded old key regardless of declaration order")
		}
	}
}

// ---------------------------------------------------------------------------
// No-op when only the new name is present in the deployed file
// ---------------------------------------------------------------------------

// TestRename_OnlyNewNamePresent_RenameIsNoOp asserts that when the deployed file contains
// only the new injection name (the rename already happened in a prior run), the rename
// table entry is a no-op. The region is preserved normally without invoking migration.
func TestRename_OnlyNewNamePresent_RenameIsNoOp(t *testing.T) {
	req := transform.Request{
		Source:           []byte(renameSource),
		Deployed:         []byte(renameDeployedNewNameOnly),
		Kind:             domain.ArtifactAgent,
		Key:              "rename-test",
		Module:           newFixtureModule(t),
		Model:            fixtureModel(),
		Scope:            domain.ScopeProject,
		InjectionRenames: renameEntry("CapabilityDesc", "NewCapabilityDesc"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	// The new name must appear in the output with the deployed content preserved.
	outNode, ok := outDoc.Body().Injection("NewCapabilityDesc")
	if !ok {
		t.Fatal("NewCapabilityDesc absent from output; the region must be preserved when the new name is in the deployed file")
	}
	if !bytes.Contains(outNode.Content(), []byte("NOOP-CONTENT")) {
		t.Errorf("NewCapabilityDesc content must be the deployed new-name content; got: %q", outNode.Content())
	}

	// When only the new name is present, the action is RegionPreserved, not RegionMigrated.
	// (The rename map is a no-op here: there is nothing to migrate.)
	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "NewCapabilityDesc" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatal("RegionOutcome for NewCapabilityDesc absent from report")
	}
	if outcome.Action == transform.RegionMigrated {
		t.Errorf("NewCapabilityDesc action must not be RegionMigrated when only the new name is present; "+
			"migration requires the old name to be found in the deployed file. Got: %q", outcome.Action)
	}
	if outcome.Action != transform.RegionPreserved {
		t.Errorf("NewCapabilityDesc action: want %q (RegionPreserved), got %q; "+
			"when the rename table entry has no matching old name in the deployed file, "+
			"the region is a normal preserve", transform.RegionPreserved, outcome.Action)
	}
}

// ---------------------------------------------------------------------------
// Conflict detection: ambiguous rename table is a hard error
// ---------------------------------------------------------------------------

// TestValidateRenames_DuplicateOld_ReturnsConflictError asserts that when the same Old
// name appears in two entries (mapping one source name to two destinations), ValidateRenames
// returns an error wrapping ErrRenameMapConflict. The tool cannot choose between two
// destinations for the user's content.
func TestValidateRenames_DuplicateOld_ReturnsConflictError(t *testing.T) {
	entries := []transform.RenameEntry{
		{Old: "Shared", New: "Dest1"},
		{Old: "Shared", New: "Dest2"},
	}

	err := transform.ValidateRenames(entries)
	if err == nil {
		t.Fatal("ValidateRenames must return an error when the same Old value appears twice; got nil")
	}
	if !errors.Is(err, transform.ErrRenameMapConflict) {
		t.Errorf("expected error wrapping transform.ErrRenameMapConflict for duplicate Old; got: %v", err)
	}
}

// TestValidateRenames_DuplicateOld_ErrorNamesConflictingEntry asserts that the error
// message for a duplicate Old includes the conflicting name, so the user can locate
// the problem without scanning the full table.
func TestValidateRenames_DuplicateOld_ErrorNamesConflictingEntry(t *testing.T) {
	entries := []transform.RenameEntry{
		{Old: "ConflictName", New: "DestA"},
		{Old: "ConflictName", New: "DestB"},
	}

	err := transform.ValidateRenames(entries)
	if err == nil {
		t.Fatal("ValidateRenames must return an error for a duplicate Old; got nil")
	}
	if !strings.Contains(err.Error(), "ConflictName") {
		t.Errorf("conflict error must name the conflicting Old value (%q) so the user can find it; got: %v",
			"ConflictName", err)
	}
}

// TestValidateRenames_MappingChain_ReturnsConflictError asserts that a mapping chain —
// a name appearing as both an Old and a New in the table — is rejected outright with an
// error wrapping ErrRenameMapConflict. Chains are never resolved transitively: the
// resolution order would depend on iteration and produce non-deterministic results.
func TestValidateRenames_MappingChain_ReturnsConflictError(t *testing.T) {
	// B appears as New in the first entry and as Old in the second — a chain A→B→C.
	entries := []transform.RenameEntry{
		{Old: "A", New: "B"},
		{Old: "B", New: "C"},
	}

	err := transform.ValidateRenames(entries)
	if err == nil {
		t.Fatal("ValidateRenames must return an error when a name appears as both Old and New (mapping chain); got nil")
	}
	if !errors.Is(err, transform.ErrRenameMapConflict) {
		t.Errorf("expected error wrapping transform.ErrRenameMapConflict for mapping chain; got: %v", err)
	}
}

// TestValidateRenames_MappingChain_ErrorNamesChainedValue asserts that the error
// for a mapping chain includes the chained name (the value that appears as both Old
// and New), so the user can identify which entry creates the chain.
func TestValidateRenames_MappingChain_ErrorNamesChainedValue(t *testing.T) {
	entries := []transform.RenameEntry{
		{Old: "Alpha", New: "Beta"},
		{Old: "Beta", New: "Gamma"},
	}

	err := transform.ValidateRenames(entries)
	if err == nil {
		t.Fatal("ValidateRenames must return an error for a mapping chain; got nil")
	}
	// Beta is the value that creates the chain — it appears as both New (entry 0) and Old (entry 1).
	if !strings.Contains(err.Error(), "Beta") {
		t.Errorf("chain conflict error must name the chained value (%q); got: %v", "Beta", err)
	}
}

// TestValidateRenames_SelfMap_ReturnsConflictError asserts that an entry where Old == New
// is rejected. A self-mapping produces no migration and misleads callers into believing
// a rename occurred.
func TestValidateRenames_SelfMap_ReturnsConflictError(t *testing.T) {
	entries := []transform.RenameEntry{
		{Old: "SameName", New: "SameName"},
	}

	err := transform.ValidateRenames(entries)
	if err == nil {
		t.Fatal("ValidateRenames must return an error when Old == New; got nil")
	}
	if !errors.Is(err, transform.ErrRenameMapConflict) {
		t.Errorf("expected error wrapping transform.ErrRenameMapConflict for self-map; got: %v", err)
	}
}

// TestValidateRenames_EmptyOld_ReturnsConflictError asserts that an entry with an empty
// Old field is rejected. An empty Old would match the empty string, not a real injection name.
func TestValidateRenames_EmptyOld_ReturnsConflictError(t *testing.T) {
	entries := []transform.RenameEntry{
		{Old: "", New: "Destination"},
	}

	err := transform.ValidateRenames(entries)
	if err == nil {
		t.Fatal("ValidateRenames must return an error when Old is empty; got nil")
	}
	if !errors.Is(err, transform.ErrRenameMapConflict) {
		t.Errorf("expected error wrapping transform.ErrRenameMapConflict for empty Old; got: %v", err)
	}
}

// TestValidateRenames_EmptyNew_ReturnsConflictError asserts that an entry with an empty
// New field is rejected. An empty New provides no destination for the migrated content.
func TestValidateRenames_EmptyNew_ReturnsConflictError(t *testing.T) {
	entries := []transform.RenameEntry{
		{Old: "Source", New: ""},
	}

	err := transform.ValidateRenames(entries)
	if err == nil {
		t.Fatal("ValidateRenames must return an error when New is empty; got nil")
	}
	if !errors.Is(err, transform.ErrRenameMapConflict) {
		t.Errorf("expected error wrapping transform.ErrRenameMapConflict for empty New; got: %v", err)
	}
}

// TestValidateRenames_ValidTable_ReturnsNil asserts that a valid, unambiguous rename table
// passes validation without error.
func TestValidateRenames_ValidTable_ReturnsNil(t *testing.T) {
	entries := []transform.RenameEntry{
		{Old: "OldAlpha", New: "NewAlpha"},
		{Old: "OldBeta", New: "NewBeta"},
	}

	if err := transform.ValidateRenames(entries); err != nil {
		t.Errorf("ValidateRenames must return nil for a valid, unambiguous table; got: %v", err)
	}
}

// TestValidateRenames_EmptyTable_ReturnsNil asserts that an empty table (no entries) is
// valid. The initially-empty package table must pass validation.
func TestValidateRenames_EmptyTable_ReturnsNil(t *testing.T) {
	if err := transform.ValidateRenames(nil); err != nil {
		t.Errorf("ValidateRenames must return nil for a nil (empty) table; got: %v", err)
	}
	if err := transform.ValidateRenames([]transform.RenameEntry{}); err != nil {
		t.Errorf("ValidateRenames must return nil for an empty table; got: %v", err)
	}
}

// TestApply_ConflictingRenameTable_ReturnsConflictError asserts that when a conflicting
// rename table is supplied via Request.InjectionRenames, Apply returns an error wrapping
// ErrRenameMapConflict before performing any document mutation.
func TestApply_ConflictingRenameTable_ReturnsConflictError(t *testing.T) {
	conflicting := []transform.RenameEntry{
		{Old: "SomeName", New: "DestA"},
		{Old: "SomeName", New: "DestB"},
	}

	req := transform.Request{
		Source:           []byte(renameSource),
		Deployed:         []byte(renameDeployedOldName),
		Kind:             domain.ArtifactAgent,
		Key:              "rename-test",
		Module:           newFixtureModule(t),
		Model:            fixtureModel(),
		Scope:            domain.ScopeProject,
		InjectionRenames: conflicting,
	}

	result, err := transform.Apply(req)
	if err == nil {
		t.Fatal("Apply must return an error when the rename table is conflicting; got nil")
	}
	if !errors.Is(err, transform.ErrRenameMapConflict) {
		t.Errorf("expected error wrapping transform.ErrRenameMapConflict; got: %v", err)
	}
	// The transform must produce no output on the conflict error path.
	if len(result.Output) != 0 {
		t.Errorf("Apply returned an error but result.Output is non-empty (%d bytes); "+
			"the transform must not partially mutate a document when the rename table is conflicting",
			len(result.Output))
	}
}

// TestApply_ConflictingRenameTable_ErrorNamesConflictingEntries asserts that the error
// message when Apply rejects a conflicting rename table includes the conflicting name,
// so the user can locate the problem without inspecting the full table.
func TestApply_ConflictingRenameTable_ErrorNamesConflictingEntries(t *testing.T) {
	conflicting := []transform.RenameEntry{
		{Old: "DoubleConflict", New: "Dest1"},
		{Old: "DoubleConflict", New: "Dest2"},
	}

	req := transform.Request{
		Source:           []byte(renameSource),
		Deployed:         []byte(renameDeployedOldName),
		Kind:             domain.ArtifactAgent,
		Key:              "rename-test",
		Module:           newFixtureModule(t),
		Model:            fixtureModel(),
		Scope:            domain.ScopeProject,
		InjectionRenames: conflicting,
	}

	_, err := transform.Apply(req)
	if err == nil {
		t.Fatal("Apply must return an error for a conflicting rename table; got nil")
	}
	if !strings.Contains(err.Error(), "DoubleConflict") {
		t.Errorf("conflict error must name the conflicting entry (%q); got: %v", "DoubleConflict", err)
	}
}

// TestApply_MappingChainInRenameTable_ReturnsConflictError asserts that a mapping chain
// supplied via Request.InjectionRenames causes Apply to return an error wrapping
// ErrRenameMapConflict before any mutation.
func TestApply_MappingChainInRenameTable_ReturnsConflictError(t *testing.T) {
	chain := []transform.RenameEntry{
		{Old: "First", New: "Second"},
		{Old: "Second", New: "Third"},
	}

	req := transform.Request{
		Source:           []byte(renameSource),
		Deployed:         []byte(renameDeployedOldName),
		Kind:             domain.ArtifactAgent,
		Key:              "rename-test",
		Module:           newFixtureModule(t),
		Model:            fixtureModel(),
		Scope:            domain.ScopeProject,
		InjectionRenames: chain,
	}

	_, err := transform.Apply(req)
	if err == nil {
		t.Fatal("Apply must return an error when the rename table contains a mapping chain; got nil")
	}
	if !errors.Is(err, transform.ErrRenameMapConflict) {
		t.Errorf("expected error wrapping transform.ErrRenameMapConflict for mapping chain; got: %v", err)
	}
}

// TestApply_NilInjectionRenames_UsesPackageTable asserts that calling Apply with
// Request.InjectionRenames left as its zero value (nil) succeeds and behaves identically to
// supplying an empty table. Nil is the normal case — every real caller uses nil so that the
// package-level transform.InjectionRenames table is consulted. The package table is empty
// by default, so the expected behavior is: no rename logic runs, content is preserved normally,
// no error is returned.
//
// This test covers the "normal case" code path that every real caller exercises but no other
// test in this suite reaches (all other Apply tests set req.InjectionRenames explicitly).
func TestApply_NilInjectionRenames_UsesPackageTable(t *testing.T) {
	// InjectionRenames intentionally omitted (nil zero value) — exercises the package-level
	// fallback path, not the explicit-override path used by all other tests in this file.
	req := transform.Request{
		Source:   []byte(unmappedSource),
		Deployed: []byte(unmappedDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "unmapped-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply with nil InjectionRenames returned an error: %v; "+
			"the nil/default path must succeed when the package-level InjectionRenames table is empty",
			err)
	}

	// Content must be preserved normally — nil must behave the same as an empty table.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	outNode, ok := outDoc.Body().Injection("UnmappedInjection")
	if !ok {
		t.Fatal("UnmappedInjection absent from output; nil InjectionRenames must not suppress normal injection preservation")
	}
	if !bytes.Contains(outNode.Content(), []byte("UNMAPPED-CONTENT")) {
		t.Errorf("UnmappedInjection content must be preserved unchanged when InjectionRenames is nil; "+
			"got: %q", outNode.Content())
	}

	// The region outcome must be RegionPreserved — no rename ran, so no migration occurred.
	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "UnmappedInjection" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatal("RegionOutcome for UnmappedInjection absent from report")
	}
	if outcome.Action != transform.RegionPreserved {
		t.Errorf("UnmappedInjection action: want %q (RegionPreserved), got %q; "+
			"with nil InjectionRenames the package table (empty by default) is used and no rename runs",
			transform.RegionPreserved, outcome.Action)
	}
}

// ---------------------------------------------------------------------------
// Unmapped names unaffected; genuine removals still orphan
// ---------------------------------------------------------------------------

// TestRename_UnmappedInjection_Unaffected asserts that an injection name not in the
// rename table is entirely unaffected by rename logic. Its content is preserved normally
// and its outcome is RegionPreserved, not RegionMigrated.
func TestRename_UnmappedInjection_Unaffected(t *testing.T) {
	// The rename table maps SomeOtherOld → SomeOtherNew — a name unrelated to UnmappedInjection.
	req := transform.Request{
		Source:   []byte(unmappedSource),
		Deployed: []byte(unmappedDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "unmapped-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		InjectionRenames: []transform.RenameEntry{
			{Old: "SomeOtherOld", New: "SomeOtherNew"},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// UnmappedInjection must be in the output with its content preserved.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	outNode, ok := outDoc.Body().Injection("UnmappedInjection")
	if !ok {
		t.Fatal("UnmappedInjection absent from output; a name not in the rename table must be unaffected")
	}
	if !bytes.Contains(outNode.Content(), []byte("UNMAPPED-CONTENT")) {
		t.Errorf("UnmappedInjection content must be preserved unchanged; got: %q", outNode.Content())
	}

	// The outcome must be RegionPreserved, not RegionMigrated.
	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "UnmappedInjection" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatal("RegionOutcome for UnmappedInjection absent from report")
	}
	if outcome.Action == transform.RegionMigrated {
		t.Errorf("UnmappedInjection action must not be RegionMigrated; the rename table does not mention this name. Got: %q", outcome.Action)
	}
	if outcome.Action != transform.RegionPreserved {
		t.Errorf("UnmappedInjection action: want %q (RegionPreserved), got %q", transform.RegionPreserved, outcome.Action)
	}
}

// TestRename_GenuineRemoval_StillOrphans asserts that a genuinely removed injection —
// absent from the source and not in any rename table entry — still produces RegionOrphaned
// and GapRemovedInjection, unchanged. Rename resolution must not swallow genuine removals.
func TestRename_GenuineRemoval_StillOrphans(t *testing.T) {
	// renameAndOrphanSource has NewName but no RemovedInjection.
	// renameAndOrphanDeployed has OldName (→ migrates to NewName) and RemovedInjection (genuine orphan).
	req := transform.Request{
		Source:   []byte(renameAndOrphanSource),
		Deployed: []byte(renameAndOrphanDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "rename-orphan-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		InjectionRenames: []transform.RenameEntry{
			{Old: "OldName", New: "NewName"},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// RemovedInjection must produce RegionOrphaned.
	var orphanOutcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "RemovedInjection" {
			orphanOutcome = &result.Report.Regions[i]
			break
		}
	}
	if orphanOutcome == nil {
		t.Fatalf("RegionOutcome for RemovedInjection absent from report; "+
			"a genuinely removed injection must still produce an orphan outcome. Report: %v", result.Report.Regions)
	}
	if orphanOutcome.Action != transform.RegionOrphaned {
		t.Errorf("RemovedInjection action: want %q (RegionOrphaned), got %q; "+
			"rename resolution must not swallow genuine removals", transform.RegionOrphaned, orphanOutcome.Action)
	}

	// GapRemovedInjection must still be emitted for RemovedInjection.
	var removalGap *domain.Gap
	for i := range result.Report.Gaps {
		if result.Report.Gaps[i].Kind == domain.GapRemovedInjection &&
			result.Report.Gaps[i].Subject == "RemovedInjection" {
			removalGap = &result.Report.Gaps[i]
			break
		}
	}
	if removalGap == nil {
		t.Fatalf("GapRemovedInjection for RemovedInjection must be emitted; "+
			"genuine removals still route to this gap regardless of the rename table. Gaps: %v", result.Report.Gaps)
	}
	if !strings.Contains(removalGap.Fragment, "ORPHAN-ME") {
		t.Errorf("GapRemovedInjection.Fragment for RemovedInjection must carry the orphaned content; got: %q", removalGap.Fragment)
	}
}

// TestRename_GenuineRemoval_ContentNotLostToRenameResolution asserts that rename
// resolution does not suppress the GapRemovedInjection for a genuinely removed injection
// by silently treating it as a renamed old key. Only names that appear as Old in the
// rename table are considered renamed; all others follow the existing removal path.
func TestRename_GenuineRemoval_ContentNotLostToRenameResolution(t *testing.T) {
	req := transform.Request{
		Source:   []byte(renameAndOrphanSource),
		Deployed: []byte(renameAndOrphanDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "rename-orphan-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		InjectionRenames: []transform.RenameEntry{
			{Old: "OldName", New: "NewName"},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The rename entry maps OldName → NewName. RemovedInjection is not in the rename table.
	// Rename resolution must not claim RemovedInjection as an old key for any new name.
	for _, o := range result.Report.Regions {
		if o.Name == "RemovedInjection" && o.Action == transform.RegionMigrated {
			t.Errorf("RemovedInjection action is RegionMigrated but it has no rename entry; "+
				"rename resolution must only apply to names that appear as Old in the table")
		}
	}
}

// TestRename_MigratedRegion_AndGenuineRemoval_BothHandledCorrectly asserts that when the
// deployed file contains both a renamed old name and a genuinely removed injection, the
// migration and orphan paths are independent: the renamed content reaches the new region
// and the removed injection still orphans. The two paths must not interfere.
func TestRename_MigratedRegion_AndGenuineRemoval_BothHandledCorrectly(t *testing.T) {
	req := transform.Request{
		Source:   []byte(renameAndOrphanSource),
		Deployed: []byte(renameAndOrphanDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "rename-orphan-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		InjectionRenames: []transform.RenameEntry{
			{Old: "OldName", New: "NewName"},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Parse the deployed file to get OldName's content for the byte-identity assertion.
	depDoc, err := docformat.Parse([]byte(renameAndOrphanDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	depOldNode, depOK := depDoc.Body().Injection("OldName")
	if !depOK {
		t.Fatal("OldName not found in deployed fixture; fixture is malformed")
	}

	// Migration: NewName must be in the output with OldName's content.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	outNewNode, newOK := outDoc.Body().Injection("NewName")
	if !newOK {
		t.Fatal("NewName absent from output; migrated content must reach the new region")
	}
	if !bytes.Equal(outNewNode.Content(), depOldNode.Content()) {
		t.Errorf("NewName content not byte-identical to deployed OldName:\ndeployed: %q\noutput:   %q",
			depOldNode.Content(), outNewNode.Content())
	}

	// Migration outcome: NewName reported as RegionMigrated.
	var migratedOutcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "NewName" {
			migratedOutcome = &result.Report.Regions[i]
			break
		}
	}
	if migratedOutcome == nil || migratedOutcome.Action != transform.RegionMigrated {
		t.Errorf("NewName must have RegionMigrated action; got: %v", migratedOutcome)
	}

	// Genuine removal: RemovedInjection must still produce RegionOrphaned and a gap.
	var hasOrphanOutcome bool
	for _, o := range result.Report.Regions {
		if o.Name == "RemovedInjection" && o.Action == transform.RegionOrphaned {
			hasOrphanOutcome = true
			break
		}
	}
	if !hasOrphanOutcome {
		t.Error("RemovedInjection must still produce RegionOrphaned; the migration path must not affect the orphan path")
	}

	var hasRemovalGap bool
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapRemovedInjection && g.Subject == "RemovedInjection" {
			hasRemovalGap = true
			break
		}
	}
	if !hasRemovalGap {
		t.Error("RemovedInjection must still produce GapRemovedInjection; " +
			"the rename table must not suppress genuine removal gaps for unmapped names")
	}
}
