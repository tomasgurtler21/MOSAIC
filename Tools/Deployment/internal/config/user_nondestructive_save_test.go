package config_test

// Tests for the non-destructive contract of UserConfigStore.Save: a Save call must edit
// the four modeled keys in place within the existing document instead of regenerating it,
// so that comments the user wrote survive, and no value under any modeled field is lost
// by a Save triggered by an unrelated field's change.
//
// Verified invariants:
//
// Comment preservation across a Load -> mutate -> Save cycle:
//   - A leading file-header comment survives, and stays at the top of the file.
//   - A comment attached to a key survives even when that key's own value is the one
//     being changed, and stays on the line immediately above that key.
//   - An inline/trailing comment on an untouched key survives, and stays on the same
//     line as the value it trails.
//   - The file resulting from a comment-preserving Save still parses back through Load
//     to both the original (untouched) and the newly mutated values.
//
// Malformed existing file:
//   - Save against an existing file that is not valid YAML returns an error, does not
//     panic, and leaves the file on disk byte-for-byte unchanged.
//
// Value preservation across a Load -> mutate-one-field -> Save cycle, mirroring the
// load-merge-save pattern persistTierModels and persistCustomModelIDs follow:
//   - Recording a new tier model leaves pre-existing ToolDestinations intact.
//   - Recording a new tier model leaves pre-existing CustomModelIDs intact.
//   - Recording a new tier model for one harness leaves another harness's TierModels intact.
//   - Recording a new tier for one tier leaves another tier under the same harness intact.
//   - Recording a custom model ID leaves pre-existing ToolDestinations intact.
//   - Recording a custom model ID leaves pre-existing TierModels intact.
//
// Key removal:
//   - When a modeled field goes from populated to empty/nil and the file already had
//     that key, Save actively removes the key rather than leaving it stale.
//
// First-run case:
//   - Saving all four modeled fields when no file exists yet produces a file that Load
//     reads back correctly.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
)

// readSavedUserConfig returns the raw bytes written to user-config.yaml inside root.
func readSavedUserConfig(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "MosaicDeploy", "config", "user-config.yaml"))
	if err != nil {
		t.Fatalf("reading saved user-config.yaml: %v", err)
	}
	return string(data)
}

// lineIndexContaining returns the 0-indexed line number of the first line in content
// that contains substr, or -1 if no line contains it. Used to assert that a comment
// stays positioned next to the key it belongs to, not merely present anywhere in the file.
func lineIndexContaining(content, substr string) int {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.Contains(line, substr) {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// Comment preservation across a Save round trip (AC3.1)
// ---------------------------------------------------------------------------

// TestUserConfigStore_Save_PreservesLeadingHeaderComment verifies that a comment at the
// top of the file, above any key, survives a Load -> mutate -> Save cycle.
func TestUserConfigStore_Save_PreservesLeadingHeaderComment(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	initial := []byte("# Personal config - do not commit.\n" +
		"schema_version: \"1\"\n" +
		"tier_models:\n" +
		"  claude-code:\n" +
		"    HIGH: claude-opus-4-5\n")
	writeUserConfig(t, root, initial)

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Mutate a field, as a real persist call would (e.g. recording a new tier model).
	cfg.TierModels["claude-code"]["MEDIUM"] = "claude-sonnet-4-5"

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got := readSavedUserConfig(t, root)
	if !strings.Contains(got, "Personal config - do not commit.") {
		t.Errorf("saved file lost the leading file-header comment; " +
			"Save must preserve comments present before the write (AC3.1)")
	}
	// Positional check: a file-header comment must stay at the top of the file, not merely
	// be present somewhere in it (e.g. relocated to the end by a naive "append lost comments" fix).
	if idx := lineIndexContaining(got, "Personal config - do not commit."); idx != 0 {
		t.Errorf("leading file-header comment moved to line %d, want line 0 (top of file); "+
			"a relocated comment is not a preserved comment", idx)
	}
}

// TestUserConfigStore_Save_PreservesCommentAboveModifiedKey verifies that a comment
// attached to a key survives even when that specific key's value is the one being
// changed by this Save call.
func TestUserConfigStore_Save_PreservesCommentAboveModifiedKey(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	initial := []byte("schema_version: \"1\"\n" +
		"# Tier-to-model overrides recorded by mosaic-deploy.\n" +
		"tier_models:\n" +
		"  claude-code:\n" +
		"    HIGH: claude-opus-4-5\n")
	writeUserConfig(t, root, initial)

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Mutate the very key the comment sits above.
	cfg.TierModels["claude-code"]["HIGH"] = "claude-opus-4-6"

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got := readSavedUserConfig(t, root)
	if !strings.Contains(got, "Tier-to-model overrides recorded by mosaic-deploy.") {
		t.Errorf("saved file lost the comment above tier_models after that key's own value " +
			"changed; a key's own comments must stay attached when its value is replaced")
	}
	// Positional check: the comment must remain the line immediately above tier_models:,
	// not merely present somewhere in the file (e.g. relocated to the file's end).
	commentLine := lineIndexContaining(got, "Tier-to-model overrides recorded by mosaic-deploy.")
	keyLine := lineIndexContaining(got, "tier_models:")
	if commentLine == -1 || keyLine == -1 || keyLine != commentLine+1 {
		t.Errorf("comment above tier_models is not immediately attached to it: comment at line %d, "+
			"tier_models: at line %d; want the comment on the line directly above the key", commentLine, keyLine)
	}
}

// TestUserConfigStore_Save_PreservesInlineTrailingComment verifies that an inline
// comment trailing an untouched key's value survives a Save triggered by a different
// field changing.
func TestUserConfigStore_Save_PreservesInlineTrailingComment(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	initial := []byte("schema_version: \"1\"\n" +
		"tier_models:\n" +
		"  claude-code:\n" +
		"    HIGH: claude-opus-4-5 # my preferred daily driver\n")
	writeUserConfig(t, root, initial)

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Change an unrelated field; the tier_models entry and its comment should be untouched.
	cfg.CustomModelIDs = map[string][]string{"claude-code": {"claude-opus-special"}}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got := readSavedUserConfig(t, root)
	if !strings.Contains(got, "my preferred daily driver") {
		t.Errorf("saved file lost the inline/trailing comment on an untouched key; " +
			"Save must preserve inline comments (AC3.1)")
	}
	// Positional check: an inline comment must stay on the same line as the value it
	// trails, not merely be present somewhere in the file.
	commentLine := lineIndexContaining(got, "my preferred daily driver")
	valueLine := lineIndexContaining(got, "claude-opus-4-5")
	if commentLine == -1 || valueLine == -1 || commentLine != valueLine {
		t.Errorf("inline comment is not attached to its value's line: comment at line %d, "+
			"claude-opus-4-5 at line %d; want them on the same line", commentLine, valueLine)
	}
}

// TestUserConfigStore_Save_AfterPreservingSave_LoadReturnsMutatedAndOriginalValues verifies
// that the file produced by a comment-preserving Save is still valid YAML that Load parses
// back correctly, for both the value that was changed and the value that was not (AC3.4).
func TestUserConfigStore_Save_AfterPreservingSave_LoadReturnsMutatedAndOriginalValues(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	initial := []byte("# Personal config.\n" +
		"schema_version: \"1\"\n" +
		"tier_models:\n" +
		"  claude-code:\n" +
		"    HIGH: claude-opus-4-5 # notes\n")
	writeUserConfig(t, root, initial)

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.TierModels["claude-code"]["MEDIUM"] = "claude-sonnet-4-5"

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load after comment-preserving Save: %v; the written file must remain "+
			"valid YAML that Load reads back correctly (AC3.4)", err)
	}
	if got.TierModels["claude-code"]["HIGH"] != "claude-opus-4-5" {
		t.Errorf("HIGH = %q after Save with comments, want \"claude-opus-4-5\" (original value must survive)",
			got.TierModels["claude-code"]["HIGH"])
	}
	if got.TierModels["claude-code"]["MEDIUM"] != "claude-sonnet-4-5" {
		t.Errorf("MEDIUM = %q after Save with comments, want \"claude-sonnet-4-5\" (mutated value must be recorded)",
			got.TierModels["claude-code"]["MEDIUM"])
	}
}

// ---------------------------------------------------------------------------
// Malformed existing file (ContractsDesign.md: "File exists and does not parse")
// ---------------------------------------------------------------------------

// TestUserConfigStore_Save_MalformedExistingFile_ReturnsErrorAndLeavesFileUntouched verifies
// that Save on an existing file that is not valid YAML returns an error instead of panicking,
// and does not overwrite or otherwise modify the malformed file on disk.
func TestUserConfigStore_Save_MalformedExistingFile_ReturnsErrorAndLeavesFileUntouched(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	// An unterminated flow sequence is invalid YAML under any conforming parser.
	malformed := []byte("schema_version: \"1\"\n" +
		"tier_models: [claude-code\n")
	writeUserConfig(t, root, malformed)

	cfg := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": {"HIGH": "claude-opus-4-5"},
		},
	}

	err := store.Save(cfg)
	if err == nil {
		t.Fatalf("Save against a malformed existing file returned nil error; " +
			"want a wrapped error, not a silent success (ContractsDesign.md: do not panic, " +
			"do not overwrite the file)")
	}

	got := readSavedUserConfig(t, root)
	if got != string(malformed) {
		t.Errorf("Save against a malformed existing file modified the file on disk: "+
			"want the file left exactly as the user left it.\ngot:\n%s\nwant:\n%s", got, malformed)
	}
}

// ---------------------------------------------------------------------------
// Value preservation: recording a tier model must not lose other modeled fields (AC3.2, AC3.3)
// ---------------------------------------------------------------------------

// TestUserConfigStore_RecordTierModel_PreservesExistingToolDestinations verifies that the
// load-merge-save pattern used to record a new tier model does not lose a pre-existing
// tool_destinations entry.
func TestUserConfigStore_RecordTierModel_PreservesExistingToolDestinations(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	initial := config.UserConfig{
		ToolDestinations: config.ToolDestinationsByHarness{
			"claude-code": {
				{Generic: "user_interaction", Destinations: []domain.ToolDestination{
					{Kind: domain.DestMain, Names: []string{"AskUserQuestion"}},
				}},
			},
		},
	}
	if err := store.Save(initial); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	// Simulate recording a tier-model selection: Load the current config, merge in the
	// new tier mapping, and Save the whole result back — the pattern persistTierModels
	// follows.
	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.TierModels = map[string]map[domain.Tier]string{
		"claude-code": {"HIGH": "claude-opus-4-5"},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save after recording tier model: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	mappings := got.ToolDestinations["claude-code"]
	if len(mappings) != 1 || mappings[0].Generic != "user_interaction" {
		t.Errorf("ToolDestinations lost after recording a tier model: got %v, want the pre-existing "+
			"user_interaction mapping intact (AC3.2)", mappings)
	}
}

// TestUserConfigStore_RecordTierModel_PreservesExistingCustomModelIDs verifies that
// recording a new tier model does not lose pre-existing custom model IDs.
func TestUserConfigStore_RecordTierModel_PreservesExistingCustomModelIDs(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	initial := config.UserConfig{
		CustomModelIDs: map[string][]string{
			"claude-code": {"claude-opus-special"},
		},
	}
	if err := store.Save(initial); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.TierModels = map[string]map[domain.Tier]string{
		"claude-code": {"HIGH": "claude-opus-4-5"},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save after recording tier model: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !stringSliceContains(got.CustomModelIDs["claude-code"], "claude-opus-special") {
		t.Errorf("CustomModelIDs lost after recording a tier model: got %v, want to still contain "+
			"\"claude-opus-special\" (AC3.2)", got.CustomModelIDs["claude-code"])
	}
}

// TestUserConfigStore_RecordTierModel_PreservesOtherHarnessTierModels verifies that
// recording a tier model for one harness leaves another harness's tier mappings intact.
func TestUserConfigStore_RecordTierModel_PreservesOtherHarnessTierModels(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	initial := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"ghcp-cli": {"HIGH": "gpt-4.1"},
		},
	}
	if err := store.Save(initial); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TierModels == nil {
		cfg.TierModels = map[string]map[domain.Tier]string{}
	}
	cfg.TierModels["claude-code"] = map[domain.Tier]string{"HIGH": "claude-opus-4-5"}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save after recording tier model for a different harness: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.TierModels["ghcp-cli"]["HIGH"] != "gpt-4.1" {
		t.Errorf("ghcp-cli HIGH lost after recording a claude-code tier model: got %q, want \"gpt-4.1\" (AC3.3)",
			got.TierModels["ghcp-cli"]["HIGH"])
	}
}

// TestUserConfigStore_RecordTierModel_PreservesOtherTiersSameHarness verifies that
// recording one tier's model leaves another tier's model, under the same harness, intact.
func TestUserConfigStore_RecordTierModel_PreservesOtherTiersSameHarness(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	initial := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": {"LOW": "claude-haiku-3"},
		},
	}
	if err := store.Save(initial); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.TierModels["claude-code"]["HIGH"] = "claude-opus-4-5"
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save after recording HIGH tier model: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.TierModels["claude-code"]["LOW"] != "claude-haiku-3" {
		t.Errorf("claude-code LOW lost after recording HIGH: got %q, want \"claude-haiku-3\" (AC3.3)",
			got.TierModels["claude-code"]["LOW"])
	}
}

// ---------------------------------------------------------------------------
// Value preservation: recording a custom model ID must not lose other modeled fields (AC3.2)
// ---------------------------------------------------------------------------

// TestUserConfigStore_RecordCustomModelID_PreservesToolDestinations verifies that the
// load-merge-save pattern used to record a new custom model ID does not lose a
// pre-existing tool_destinations entry.
func TestUserConfigStore_RecordCustomModelID_PreservesToolDestinations(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	initial := config.UserConfig{
		ToolDestinations: config.ToolDestinationsByHarness{
			"claude-code": {
				{Generic: "subagent", Destinations: []domain.ToolDestination{
					{Kind: domain.DestMain, Names: []string{"Task"}},
				}},
			},
		},
	}
	if err := store.Save(initial); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.CustomModelIDs = map[string][]string{"claude-code": {"claude-opus-special"}}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save after recording a custom model ID: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	mappings := got.ToolDestinations["claude-code"]
	if len(mappings) != 1 || mappings[0].Generic != "subagent" {
		t.Errorf("ToolDestinations lost after recording a custom model ID: got %v, want the pre-existing "+
			"subagent mapping intact (AC3.2)", mappings)
	}
}

// TestUserConfigStore_RecordCustomModelID_PreservesTierModels verifies that recording a
// new custom model ID does not disturb pre-existing tier-to-model mappings.
func TestUserConfigStore_RecordCustomModelID_PreservesTierModels(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	initial := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": {"HIGH": "claude-opus-4-5"},
		},
	}
	if err := store.Save(initial); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.CustomModelIDs = map[string][]string{"claude-code": {"claude-opus-special"}}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save after recording a custom model ID: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.TierModels["claude-code"]["HIGH"] != "claude-opus-4-5" {
		t.Errorf("TierModels lost after recording a custom model ID: got %q, want \"claude-opus-4-5\" (AC3.2)",
			got.TierModels["claude-code"]["HIGH"])
	}
}

// ---------------------------------------------------------------------------
// Key removal when a previously-present value becomes empty/nil (ContractsDesign.md
// per-key write rule: "Value empty/nil, key present in file -> Key removed, together with
// any comment attached to it")
// ---------------------------------------------------------------------------

// TestUserConfigStore_Save_ClearingToolDestinations_RemovesExistingKey verifies that when a
// modeled field goes from populated to empty/nil, and the file already had that key, Save
// actively removes the key from the document rather than leaving stale or corrupt content
// behind. This is the counterpart to the already-covered "value written when non-empty" case,
// and the one case a plain-marshal Save satisfies for free but an in-place editor must handle
// explicitly.
func TestUserConfigStore_Save_ClearingToolDestinations_RemovesExistingKey(t *testing.T) {
	root := makeRoot(t)
	store := config.NewUserConfigStore(root)

	initial := config.UserConfig{
		ToolDestinations: config.ToolDestinationsByHarness{
			"claude-code": {
				{Generic: "user_interaction", Destinations: []domain.ToolDestination{
					{Kind: domain.DestMain, Names: []string{"AskUserQuestion"}},
				}},
			},
		},
	}
	if err := store.Save(initial); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	// Sanity check on the test's own setup: the key must actually be present in the file
	// before we exercise clearing it.
	before := readSavedUserConfig(t, root)
	if !strings.Contains(before, "tool_destinations") {
		t.Fatalf("test setup invalid: tool_destinations key missing from file before clearing it: %s", before)
	}

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.ToolDestinations = nil
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save clearing tool_destinations: %v", err)
	}

	got := readSavedUserConfig(t, root)
	if strings.Contains(got, "tool_destinations") {
		t.Errorf("tool_destinations key still present after clearing the field to nil; want the "+
			"key actively removed from the document, not left behind as stale content: %s", got)
	}

	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load after clearing tool_destinations: %v", err)
	}
	if len(reloaded.ToolDestinations) != 0 {
		t.Errorf("ToolDestinations after clearing and reloading: got %v, want empty", reloaded.ToolDestinations)
	}
}

// ---------------------------------------------------------------------------
// First-run case (AC3.4)
// ---------------------------------------------------------------------------

// TestUserConfigStore_FirstRun_AllFourFields_RoundTripCorrectly verifies that saving all
// four modeled fields when no user-config.yaml exists yet produces a file that Load reads
// back correctly. This is the fallback path when there is no existing document to edit.
func TestUserConfigStore_FirstRun_AllFourFields_RoundTripCorrectly(t *testing.T) {
	root := makeRoot(t) // MosaicDeploy/config exists, but user-config.yaml does not.
	store := config.NewUserConfigStore(root)

	cfg := config.UserConfig{
		TierModels: map[string]map[domain.Tier]string{
			"claude-code": {"HIGH": "claude-opus-4-5"},
		},
		CustomModelIDs: map[string][]string{
			"claude-code": {"claude-opus-special"},
		},
		ToolDestinations: config.ToolDestinationsByHarness{
			"claude-code": {
				{Generic: "user_interaction", Destinations: []domain.ToolDestination{
					{Kind: domain.DestMain, Names: []string{"AskUserQuestion"}},
				}},
			},
		},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("first-run Save: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load after first-run Save: %v", err)
	}
	if got.TierModels["claude-code"]["HIGH"] != "claude-opus-4-5" {
		t.Errorf("first-run TierModels: got %q, want \"claude-opus-4-5\"", got.TierModels["claude-code"]["HIGH"])
	}
	if !stringSliceContains(got.CustomModelIDs["claude-code"], "claude-opus-special") {
		t.Errorf("first-run CustomModelIDs: got %v, want to contain \"claude-opus-special\"",
			got.CustomModelIDs["claude-code"])
	}
	mappings := got.ToolDestinations["claude-code"]
	if len(mappings) != 1 || mappings[0].Generic != "user_interaction" {
		t.Errorf("first-run ToolDestinations: got %v, want one mapping for user_interaction", mappings)
	}
}
