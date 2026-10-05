package app_test

// deployed_unparseable_classification_test.go drives Service.DeployAgents with the real planner
// against a harness that declares an agents directory, so id-based deployed-agent resolution is
// active, and asserts the plan action chosen for an existing file at an agent's planned path.
//
// Verified behaviours:
//   - An id-bearing agent whose planned path holds an existing file that is invalid UTF-8, uses a
//     rejected fence variant, has no frontmatter, or has frontmatter without an id is planned as
//     a CONFLICT whose reason carries the parse problem - never as a create.
//   - A genuinely absent file still plans a create; a cleanly parsed file with a different id
//     still plans a create.
//   - A BOM'd deployed agent is classified by version stamps when a manifest entry exists, and as
//     a manifest-missing CONFLICT (not a parse failure) when none exists.
//   - An id-less agent whose planned path holds an existing unparseable file is never planned as
//     a create and, without a manifest entry, is a manifest-missing CONFLICT.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

const classificationAgentsDir = ".mosaic"

var (
	classificationBOM = []byte("\xEF\xBB\xBF")

	// classificationIDAgent is an id-bearing ordinary subagent.
	classificationIDAgent = domain.Agent{
		Key:             "plan-review",
		NumericID:       "50",
		Version:         "1.0",
		Name:            "Plan Review",
		Description:     "Reviews plans before execution",
		Role:            domain.RoleSubagent,
		Category:        "Review",
		RecommendedTier: "HIGH",
	}

	// classificationIDLessStandalone is a standalone agent with no numeric id.
	classificationIDLessStandalone = domain.Agent{
		Key:             "session-logger",
		Version:         "1.0",
		Name:            "Session Logger",
		Description:     "Logs session events",
		Role:            domain.RoleStandalone,
		RecommendedTier: "LOW",
	}

	// classificationIDLessUtility is a utility agent with no numeric id.
	classificationIDLessUtility = domain.Agent{
		Key:             "doc-helper",
		Version:         "1.0",
		Name:            "Doc Helper",
		Description:     "Helps with documents",
		Role:            domain.RoleUtility,
		RecommendedTier: "LOW",
	}
)

// classificationRun is the outcome of one DeployAgents run for the classification tests.
type classificationRun struct {
	plan      *domain.Plan
	err       error
	workspace string
}

// item returns the plan item for the given agent key, failing when the plan has none.
func (r classificationRun) item(t *testing.T, key string) domain.PlanItem {
	t.Helper()
	if r.err != nil {
		t.Fatalf("DeployAgents returned error: %v", r.err)
	}
	if r.plan == nil {
		t.Fatal("executor was not called; no plan was captured")
	}
	it, ok := findPlanItem(r.plan.Items, key)
	if !ok {
		t.Fatalf("plan has no item for agent %q", key)
	}
	return it
}

// newClassificationDeps returns deps wired to the real planner and a plan-capturing executor,
// over a harness that declares classificationAgentsDir as its agents directory.
func newClassificationDeps(t *testing.T) (app.Deps, *planCapturingExecutor, string) {
	t.Helper()
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	cat := newMinimalCatalog()
	cat.agents = []domain.Agent{classificationIDAgent}
	cat.standaloneAgents = []domain.Agent{classificationIDLessStandalone}
	cat.utilityAgents = []domain.Agent{classificationIDLessUtility}
	deps.Catalog = cat
	deps.Registry = newIDAwareRegistry(newIDAwareHarnessModule(classificationAgentsDir))
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec
	return deps, capExec, workspace
}

// classificationRequest returns a headless DeployAgents request selecting the given agents.
func classificationRequest(workspace string, subagents, standalone, utility []string) app.DeployAgentsRequest {
	return app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		Scope:                  domain.ScopeProject,
		SubagentIDs:            subagents,
		StandaloneAgentIDs:     standalone,
		UtilityAgentIDs:        utility,
		InfrastructureAgentIDs: []string{},
		ConflictDefault:        domain.DecisionSkip,
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true,
		},
		AutoConfirmPlan: true,
	}
}

// runClassification writes files (relative to the agents directory) into a fresh workspace and
// runs DeployAgents for the selected agents, with an optional manifest snapshot builder.
func runClassification(
	t *testing.T,
	files map[string][]byte,
	subagents, standalone, utility []string,
	snapFor func(workspace string) manifest.Snapshot,
) classificationRun {
	t.Helper()
	deps, capExec, workspace := newClassificationDeps(t)
	for name, content := range files {
		writeTempFileMkdir(t, filepath.Join(workspace, classificationAgentsDir), name, content)
	}
	if snapFor != nil {
		deps.Manifest = &stubManifestStore{snap: snapFor(workspace)}
	}
	svc := app.New(deps)

	_, err := svc.DeployAgents(context.Background(), classificationRequest(workspace, subagents, standalone, utility))
	return classificationRun{plan: capExec.capturedPlan, err: err, workspace: workspace}
}

// writeTempFileMkdir creates dir if needed and writes name into it.
func writeTempFileMkdir(t *testing.T, dir, name string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}
	writeTempFile(t, dir, name, content)
}

// idAgentTargetPath is the planned path of the id-bearing agent.
func idAgentTargetPath() string {
	return filepath.Join(classificationAgentsDir, classificationIDAgent.Key+".md")
}

// manifestWithAgentEntry returns a present manifest recording the id-bearing agent at its
// planned path with the hash of content.
func manifestWithAgentEntry(content []byte) manifest.Snapshot {
	return manifest.Snapshot{
		State: manifest.StatePresent,
		Manifest: domain.Manifest{
			SchemaVersion: manifest.SchemaVersion,
			HarnessID:     "stub-harness",
			Entries: []domain.ManifestEntry{{
				Ref:         domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: classificationIDAgent.Key},
				TargetPath:  idAgentTargetPath(),
				Version:     "0.5",
				ContentHash: manifest.Hash(content),
			}},
		},
	}
}

// unusableDeployedFiles returns contents, built in Go code, that cannot be used cleanly for the
// id-bearing agent (id "50"), with a substring the CONFLICT reason must contain.
func unusableDeployedFiles() map[string]struct {
	content    []byte
	wantReason string
} {
	type fx = struct {
		content    []byte
		wantReason string
	}
	return map[string]fx{
		"invalid-utf8":                   {[]byte("---\nid: \"50\"\nversion: \"1.0\"\n---\nbody \xff\xfe\n"), "line "},
		"leading-blank-line-before-fence": {[]byte("\n---\nid: \"50\"\nversion: \"1.0\"\n---\nbody\n"), "line "},
		"cr-only-line-endings":           {[]byte("---\rid: \"50\"\rversion: \"1.0\"\r---\rbody\r"), "line "},
		"no-frontmatter":                 {[]byte("# Plan Review\nNo frontmatter.\n"), "no frontmatter"},
		"frontmatter-without-id":         {[]byte("---\nversion: \"1.0\"\n---\nbody\n"), "no id"},
	}
}

func TestDeployAgents_IDBearingAgent_UnusableFileAtPlannedPath_PlansConflictNotCreate(t *testing.T) {
	for name, fx := range unusableDeployedFiles() {
		t.Run(name, func(t *testing.T) {
			// Arrange / Act
			run := runClassification(t,
				map[string][]byte{classificationIDAgent.Key + ".md": fx.content},
				[]string{classificationIDAgent.Key}, []string{}, []string{}, nil)

			// Assert
			item := run.item(t, classificationIDAgent.Key)
			if item.Action == domain.ActionCreate {
				t.Fatalf("Action = create; an existing unusable file at %q must never be planned as new", item.TargetPath)
			}
			if item.Action != domain.ActionConflict {
				t.Fatalf("Action = %v, want conflict", item.Action)
			}
			if item.Conflict == nil || !item.Conflict.ManifestMissing {
				t.Errorf("Conflict = %+v; want ManifestMissing set", item.Conflict)
			}
			if !strings.Contains(item.Reason, "could not be read/parsed") {
				t.Errorf("Reason = %q; want the parse-failed wording", item.Reason)
			}
			if !strings.Contains(item.Reason, fx.wantReason) {
				t.Errorf("Reason = %q; want it to carry the parse problem (%q)", item.Reason, fx.wantReason)
			}
		})
	}
}

func TestDeployAgents_IDBearingAgent_AbsentFile_PlansCreate(t *testing.T) {
	// Act
	run := runClassification(t, nil, []string{classificationIDAgent.Key}, []string{}, []string{}, nil)

	// Assert
	if item := run.item(t, classificationIDAgent.Key); item.Action != domain.ActionCreate {
		t.Errorf("Action = %v, want create for a genuinely absent file", item.Action)
	}
}

func TestDeployAgents_IDBearingAgent_CleanFileWithDifferentID_PlansCreate(t *testing.T) {
	// Arrange - parses cleanly and carries another agent's id.
	other := []byte("---\nid: \"999\"\nversion: \"1.0\"\n---\nAnother agent.\n")

	// Act
	run := runClassification(t, map[string][]byte{classificationIDAgent.Key + ".md": other},
		[]string{classificationIDAgent.Key}, []string{}, []string{}, nil)

	// Assert
	if item := run.item(t, classificationIDAgent.Key); item.Action != domain.ActionCreate {
		t.Errorf("Action = %v, want create (today's behaviour for a file with a different id)", item.Action)
	}
}

func TestDeployAgents_BOMDeployedAgent_WithManifestEntry_ClassifiedByStamps(t *testing.T) {
	// Arrange - BOM'd deployed agent, older version stamp, manifest entry matching its bytes.
	content := append(append([]byte{}, classificationBOM...),
		[]byte("---\nid: \"50\"\nversion: \"0.5\"\n---\nbody\n")...)

	// Act
	run := runClassification(t, map[string][]byte{classificationIDAgent.Key + ".md": content},
		[]string{classificationIDAgent.Key}, []string{}, []string{},
		func(string) manifest.Snapshot { return manifestWithAgentEntry(content) })

	// Assert
	item := run.item(t, classificationIDAgent.Key)
	if item.Action != domain.ActionUpdate {
		t.Errorf("Action = %v (reason %q), want update; the BOM'd file must be read for its version stamp", item.Action, item.Reason)
	}
}

func TestDeployAgents_BOMDeployedAgent_WithoutManifestEntry_ManifestMissingConflict(t *testing.T) {
	// Arrange
	content := append(append([]byte{}, classificationBOM...),
		[]byte("---\nid: \"50\"\nversion: \"0.5\"\n---\nbody\n")...)

	// Act
	run := runClassification(t, map[string][]byte{classificationIDAgent.Key + ".md": content},
		[]string{classificationIDAgent.Key}, []string{}, []string{}, nil)

	// Assert
	item := run.item(t, classificationIDAgent.Key)
	if item.Action != domain.ActionConflict {
		t.Fatalf("Action = %v, want conflict", item.Action)
	}
	if item.Conflict == nil || !item.Conflict.ManifestMissing {
		t.Errorf("Conflict = %+v; want ManifestMissing set", item.Conflict)
	}
	if strings.Contains(item.Reason, "could not be read/parsed") {
		t.Errorf("Reason = %q; a BOM'd file parses cleanly and must not be reported as parse-failed", item.Reason)
	}
}

func TestDeployAgents_IDLessAgent_ExistingUnparseableFile_NotPlannedAsCreate(t *testing.T) {
	invalidUTF8 := []byte("---\nversion: \"1.0\"\n---\nbody \xff\xfe broken\n")
	cases := map[string]struct {
		standalone, utility []string
		key                 string
	}{
		"standalone": {[]string{classificationIDLessStandalone.Key}, []string{}, classificationIDLessStandalone.Key},
		"utility":    {[]string{}, []string{classificationIDLessUtility.Key}, classificationIDLessUtility.Key},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			run := runClassification(t, map[string][]byte{tc.key + ".md": invalidUTF8},
				[]string{}, tc.standalone, tc.utility, nil)

			// Assert
			item := run.item(t, tc.key)
			if item.Action == domain.ActionCreate {
				t.Fatalf("Action = create; an existing unparseable file must never be planned as new")
			}
			if item.Action != domain.ActionConflict {
				t.Fatalf("Action = %v, want conflict when no manifest entry exists", item.Action)
			}
			if item.Conflict == nil || !item.Conflict.ManifestMissing {
				t.Errorf("Conflict = %+v; want ManifestMissing set", item.Conflict)
			}
		})
	}
}
