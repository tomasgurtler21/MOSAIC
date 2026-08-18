package logging_test

// Tests for per-item update records (T15.3) and skipped/backed-up file records (T15.4).
//
// Every update action must record which specific version field drove the change and the
// before/after values in both sinks. Files skipped or backed up due to local-modification
// detection must appear in both sinks with sufficient detail for after-the-fact reconstruction.
//
// Shared helpers (makeLogRoot, sampleRunContext, sampleEvent, readSinkFile) are defined in
// dual_sink_test.go and are available because all files share package logging_test.

import (
	"strings"
	"testing"
	"time"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/logging"
)

// updateAction builds an ActionRecord representing an update driven by the named version field.
func updateAction(ref domain.ArtifactRef, field, before, after string) domain.ActionRecord {
	return domain.ActionRecord{
		Ref:        ref,
		TargetPath: "/workspace/agents/" + ref.Key + ".md",
		Taken:      domain.TakenUpdated,
		Stale: []domain.VersionDelta{
			{Field: field, Deployed: before, Source: after},
		},
	}
}

// doRunWithAction sets up a logger, records one action, and returns the content of both sinks.
func doRunWithAction(t *testing.T, action domain.ActionRecord) (latest, history string) {
	t.Helper()
	root := t.TempDir()
	log := logging.New(root, config.ToolConfig{})
	log.StartRun(sampleRunContext("run-rec-" + action.Ref.Key))
	log.Action(action)
	log.EndRun(domain.OutcomeSuccess)
	paths := log.Paths()
	return readSinkFile(t, paths.Latest), readSinkFile(t, paths.History)
}

// checkBothSinks asserts that all expected strings appear in both latest and history content.
func checkBothSinks(t *testing.T, latest, history string, expects ...string) {
	t.Helper()
	for _, s := range expects {
		if !strings.Contains(latest, s) {
			t.Errorf("latest.log missing expected string %q", s)
		}
		if !strings.Contains(history, s) {
			t.Errorf("history.log missing expected string %q", s)
		}
	}
}

// ---------------------------------------------------------------------------
// T15.3: Per-item update records — driving field and before/after values
// ---------------------------------------------------------------------------

// TestRecord_UpdateNamesVersionField verifies that when an update is driven by the "version"
// field, that field name appears in both log sinks.
func TestRecord_UpdateNamesVersionField(t *testing.T) {
	ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "version-agent"}
	latest, history := doRunWithAction(t, updateAction(ref, "version", "1.0.0", "2.0.0"))
	checkBothSinks(t, latest, history, "version")
}

// TestRecord_UpdateNamesTransformVersionField verifies that a transform_version-driven update
// names that specific field in both sinks.
func TestRecord_UpdateNamesTransformVersionField(t *testing.T) {
	ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "transform-version-agent"}
	latest, history := doRunWithAction(t, updateAction(ref, "transform_version", "3", "4"))
	checkBothSinks(t, latest, history, "transform_version")
}

// TestRecord_UpdateNamesInjectionsVersionField verifies that an injections_version-driven update
// names that specific field in both sinks.
func TestRecord_UpdateNamesInjectionsVersionField(t *testing.T) {
	ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "injections-version-agent"}
	latest, history := doRunWithAction(t, updateAction(ref, "injections_version", "7", "8"))
	checkBothSinks(t, latest, history, "injections_version")
}

// TestRecord_UpdateNamesSkillVersionField verifies that a skill update driven by "version"
// names that field in both sinks.
func TestRecord_UpdateNamesSkillVersionField(t *testing.T) {
	ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}
	latest, history := doRunWithAction(t, updateAction(ref, "version", "2.1.0", "2.2.0"))
	checkBothSinks(t, latest, history, "version")
}

// TestRecord_UpdateNamesHookVersionField verifies that a hook bundle update driven by "version"
// names that field in both sinks. Hook bundles carry the same "version" field name as skills and
// agents; this test confirms the attribution path works for the ArtifactHook kind specifically.
func TestRecord_UpdateNamesHookVersionField(t *testing.T) {
	ref := domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "pre-commit-hooks"}
	latest, history := doRunWithAction(t, updateAction(ref, "version", "1.0.0", "1.1.0"))
	checkBothSinks(t, latest, history, "version", ref.Key)
}

// TestRecord_UpdateIncludesDeployedBeforeValue verifies that the deployed (before) version
// value appears in both log sinks for an update record.
func TestRecord_UpdateIncludesDeployedBeforeValue(t *testing.T) {
	ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "before-value-agent"}
	before := "9.1.2-distinctive"
	latest, history := doRunWithAction(t, updateAction(ref, "version", before, "10.0.0"))
	checkBothSinks(t, latest, history, before)
}

// TestRecord_UpdateIncludesSourceAfterValue verifies that the source (after) version value
// appears in both log sinks for an update record.
func TestRecord_UpdateIncludesSourceAfterValue(t *testing.T) {
	ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "after-value-agent"}
	after := "10.0.0-distinctive"
	latest, history := doRunWithAction(t, updateAction(ref, "version", "9.1.2", after))
	checkBothSinks(t, latest, history, after)
}

// TestRecord_MultipleVersionDeltasAllRecorded verifies that when multiple version fields drove
// an update, every field name appears in both sinks.
func TestRecord_MultipleVersionDeltasAllRecorded(t *testing.T) {
	ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "multi-delta-agent"}
	action := domain.ActionRecord{
		Ref:        ref,
		TargetPath: "/workspace/agents/multi-delta-agent.md",
		Taken:      domain.TakenUpdated,
		Stale: []domain.VersionDelta{
			{Field: "version", Deployed: "1.0.0", Source: "2.0.0"},
			{Field: "transform_version", Deployed: "3", Source: "5"},
			{Field: "injections_version", Deployed: "11", Source: "12"},
		},
	}
	latest, history := doRunWithAction(t, action)
	checkBothSinks(t, latest, history, "version", "transform_version", "injections_version")
}

// TestRecord_ArtifactKeyAppearsInUpdateRecord verifies that the artifact key is recorded
// alongside the update attribution so the log reader can identify which file changed.
func TestRecord_ArtifactKeyAppearsInUpdateRecord(t *testing.T) {
	ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "identifiable-agent-key"}
	latest, history := doRunWithAction(t, updateAction(ref, "version", "1.0", "2.0"))
	checkBothSinks(t, latest, history, ref.Key)
}

// ---------------------------------------------------------------------------
// T15.4: Skipped and backed-up file records
// ---------------------------------------------------------------------------

// TestRecord_SkippedFileAppearsInBothSinks verifies that a TakenSkipped action record appears
// in both log sinks with the artifact key and the skipped action value.
func TestRecord_SkippedFileAppearsInBothSinks(t *testing.T) {
	ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "skipped-agent-key"}
	action := domain.ActionRecord{
		Ref:        ref,
		TargetPath: "/workspace/agents/skipped-agent-key.md",
		Taken:      domain.TakenSkipped,
	}
	latest, history := doRunWithAction(t, action)
	checkBothSinks(t, latest, history, ref.Key, string(domain.TakenSkipped))
}

// TestRecord_BackedUpFileAppearsInBothSinks verifies that a TakenBackedUp action appears in
// both sinks with the artifact key, the backed-up action value, and the backup path.
func TestRecord_BackedUpFileAppearsInBothSinks(t *testing.T) {
	ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "backup-agent-key"}
	backupPath := "/workspace/.mosaic/backups/agents/backup-agent-key.md.20240115T120000Z.bak"
	action := domain.ActionRecord{
		Ref:        ref,
		TargetPath: "/workspace/agents/backup-agent-key.md",
		Taken:      domain.TakenBackedUp,
		BackupPath: backupPath,
	}
	latest, history := doRunWithAction(t, action)
	checkBothSinks(t, latest, history, ref.Key, string(domain.TakenBackedUp), backupPath)
}

// TestRecord_MultipleConflictActionsAllRecorded verifies that when a run has both a skipped
// and a backed-up file, both artifact keys appear in both sinks.
func TestRecord_MultipleConflictActionsAllRecorded(t *testing.T) {
	root := t.TempDir()
	log := logging.New(root, config.ToolConfig{})
	log.StartRun(logging.RunContext{
		RunID:     "run-multi-conflict",
		StartedAt: time.Now(),
		Mode:      domain.ModeUpdateWorkspace,
		Harness:   domain.HarnessRef{ID: "test-harness", Tier: domain.TierBuiltin},
	})
	log.Action(domain.ActionRecord{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "skipped-in-multi"},
		TargetPath: "/workspace/agents/skipped-in-multi.md",
		Taken:      domain.TakenSkipped,
	})
	log.Action(domain.ActionRecord{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "backed-up-in-multi"},
		TargetPath: "/workspace/agents/backed-up-in-multi.md",
		Taken:      domain.TakenBackedUp,
		BackupPath: "/workspace/.mosaic/backups/agents/backed-up-in-multi.md.20240115.bak",
	})
	log.EndRun(domain.OutcomeCompletedWithGaps)

	paths := log.Paths()
	latest := readSinkFile(t, paths.Latest)
	history := readSinkFile(t, paths.History)
	checkBothSinks(t, latest, history, "skipped-in-multi", "backed-up-in-multi")
}

// TestRecord_EventChangesFieldAppearInBothSinks verifies that when an Event carries VersionDelta
// changes (update attribution via Event.Changes), those field names appear in both sinks.
func TestRecord_EventChangesFieldAppearInBothSinks(t *testing.T) {
	root := t.TempDir()
	log := logging.New(root, config.ToolConfig{})
	log.StartRun(sampleRunContext("run-event-changes"))
	log.Event(logging.Event{
		Time:    time.Now(),
		Level:   logging.LevelInfo,
		Kind:    "transform",
		Subject: "event-changes-agent",
		Message: "agent updated",
		Changes: []domain.VersionDelta{
			{Field: "transform_version", Deployed: "5", Source: "6"},
		},
	})
	log.EndRun(domain.OutcomeSuccess)

	paths := log.Paths()
	latest := readSinkFile(t, paths.Latest)
	history := readSinkFile(t, paths.History)
	checkBothSinks(t, latest, history, "transform_version")
}
