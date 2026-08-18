package config_test

// Reproduction test for plausible hand-authored mis-indentations of a `tool_destinations`
// block. The user hand-authored this block in their live config, copying the template's
// worked `mosaic-kb` example (Tools/Deployment/config/user-config.yaml, around lines
// 121-134), and was not confident the indentation survived the copy correctly.
//
// This test documents reality rather than forcing one particular result: for each variant
// it records the *actual observed* outcome of Load — parse error, MappingDeclError, or a
// successful load — and, for a successful load, the exact decoded value. See the doc
// comment on each Test function for what was found.
//
// FINDING (recorded for Stage-5/PlanProgress.md): of the four variants below, one
// (UniformOuterIndent) parses successfully to the *correct* value — YAML's relative
// indentation rule tolerates a uniform offset of an entire block. The other three
// (ShallowerDestinations, DeeperDestinations, MisalignedListItemField) all break YAML's
// block structure and produce a parse error, not a silent mis-parse. None of the four
// reaches outcome (ii) — a successful load whose value differs from what the text plainly
// means. This is the evidence Stage-5's T5.1 exists to gather: it does not prove outcome
// (ii) is unreachable for every conceivable mis-indentation, but no variant a user is
// plausibly likely to produce by re-indenting this block reaches it.

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
)

// expectedKnowledgeBaseMapping is the value the template's worked `mosaic-kb` example
// (Tools/Deployment/config/user-config.yaml, lines 121-134) plainly means: one generic tool
// ("knowledge_base") with two destinations, a DestMain and a DestField.
func expectedKnowledgeBaseMapping() []domain.ToolMapping {
	return []domain.ToolMapping{
		{
			Generic: "knowledge_base",
			Destinations: []domain.ToolDestination{
				{Kind: domain.DestMain, Names: []string{"Read"}},
				{
					Kind:   domain.DestField,
					Field:  "mcp_servers",
					Format: domain.FormatListBlock,
					Names:  []string{"mosaic-kb"},
				},
			},
		},
	}
}

// TestMisindentedToolDestinations_UniformOuterIndent verifies the outcome when the whole
// `tool_destinations` block is left at the leading indentation the worked example has
// inside its comment, with only the leading "#" characters stripped (the following space
// character is left in place). This is a plausible copy-paste mistake: a user copies the
// comment lines and deletes only the comment marker.
//
// OBSERVED OUTCOME: this succeeds and decodes to the *correct* value. The offset is
// applied uniformly to the entire block, and YAML's indentation rule only requires that
// siblings share the same indentation relative to each other — not that the document start
// at column 0. This is the one variant of the four that does NOT hard-fail.
func TestMisindentedToolDestinations_UniformOuterIndent(t *testing.T) {
	root := makeRoot(t)
	content := "" +
		"   tool_destinations:\n" +
		"     claude-code:\n" +
		"       - generic: knowledge_base\n" +
		"         destinations:\n" +
		"           - to: main\n" +
		"             names: [\"Read\"]\n" +
		"           - to: field\n" +
		"             field: mcp_servers\n" +
		"             format: list-block\n" +
		"             names: [\"mosaic-kb\"]\n"
	writeUserConfig(t, root, []byte(content))

	store := config.NewUserConfigStore(root)
	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned an unexpected error for a uniformly-offset block: %v", err)
	}

	want := expectedKnowledgeBaseMapping()
	got := cfg.ToolDestinations["claude-code"]
	assertToolMappingsEqual(t, got, want)
}

// TestMisindentedToolDestinations_ShallowerDestinations verifies the outcome when
// `destinations:` is indented one level shallower than its `generic:` sibling — the key
// that should be a second field of the same mapping instead sits outside the sequence
// item's mapping, dedented to the sequence item's own indentation without a leading "-".
//
// OBSERVED OUTCOME: this is not valid YAML at that position (a mapping key appears where
// only a further sequence item or a dedent back out of the sequence is expected) and Load
// returns a parse error — a hard failure, not a silent mis-parse.
func TestMisindentedToolDestinations_ShallowerDestinations(t *testing.T) {
	root := makeRoot(t)
	content := "" +
		"schema_version: \"1\"\n" +
		"tool_destinations:\n" +
		"  claude-code:\n" +
		"    - generic: knowledge_base\n" +
		"    destinations:\n" +
		"        - to: main\n" +
		"          names: [\"Read\"]\n" +
		"        - to: field\n" +
		"          field: mcp_servers\n" +
		"          format: list-block\n" +
		"          names: [\"mosaic-kb\"]\n"
	writeUserConfig(t, root, []byte(content))

	store := config.NewUserConfigStore(root)
	cfg, err := store.Load()

	if err == nil {
		t.Fatalf("Load unexpectedly succeeded for a shallower-destinations block; got cfg=%+v", cfg)
	}
	var declErr config.MappingDeclError
	if errors.As(err, &declErr) {
		t.Fatalf("Load returned a MappingDeclError for a shallower-destinations block; expected a raw parse error, got: %v", declErr)
	}
	assertErrorNamesFile(t, err, store.Path())
}

// TestMisindentedToolDestinations_DeeperDestinations verifies the outcome when
// `destinations:` is indented one level deeper than its `generic:` sibling, as though it
// were meant to nest under `generic`'s scalar value instead of being a second key in the
// same mapping.
//
// OBSERVED OUTCOME: this is not valid YAML — a mapping key cannot appear more deeply
// indented than a sibling key within the same block mapping — and Load returns a parse
// error, a hard failure.
func TestMisindentedToolDestinations_DeeperDestinations(t *testing.T) {
	root := makeRoot(t)
	content := "" +
		"schema_version: \"1\"\n" +
		"tool_destinations:\n" +
		"  claude-code:\n" +
		"    - generic: knowledge_base\n" +
		"        destinations:\n" +
		"          - to: main\n" +
		"            names: [\"Read\"]\n" +
		"          - to: field\n" +
		"            field: mcp_servers\n" +
		"            format: list-block\n" +
		"            names: [\"mosaic-kb\"]\n"
	writeUserConfig(t, root, []byte(content))

	store := config.NewUserConfigStore(root)
	cfg, err := store.Load()

	if err == nil {
		t.Fatalf("Load unexpectedly succeeded for a deeper-destinations block; got cfg=%+v", cfg)
	}
	var declErr config.MappingDeclError
	if errors.As(err, &declErr) {
		t.Fatalf("Load returned a MappingDeclError for a deeper-destinations block; expected a raw parse error, got: %v", declErr)
	}
	assertErrorNamesFile(t, err, store.Path())
}

// TestMisindentedToolDestinations_MisalignedListItemField verifies the outcome when one
// field inside an otherwise correctly-shaped destination list item is misaligned — here
// `names:` for the first destination (`to: main`) is indented one level deeper than its
// `to:` sibling, as if it were meant to nest under `to`'s scalar value.
//
// OBSERVED OUTCOME: this is not valid YAML — the same rule that rejects a too-deep
// `destinations:` key also rejects a too-deep `names:` key within a destination item —
// and Load returns a parse error, a hard failure.
func TestMisindentedToolDestinations_MisalignedListItemField(t *testing.T) {
	root := makeRoot(t)
	content := "" +
		"schema_version: \"1\"\n" +
		"tool_destinations:\n" +
		"  claude-code:\n" +
		"    - generic: knowledge_base\n" +
		"      destinations:\n" +
		"        - to: main\n" +
		"            names: [\"Read\"]\n" +
		"        - to: field\n" +
		"          field: mcp_servers\n" +
		"          format: list-block\n" +
		"          names: [\"mosaic-kb\"]\n"
	writeUserConfig(t, root, []byte(content))

	store := config.NewUserConfigStore(root)
	cfg, err := store.Load()

	if err == nil {
		t.Fatalf("Load unexpectedly succeeded for a misaligned-list-item-field block; got cfg=%+v", cfg)
	}
	var declErr config.MappingDeclError
	if errors.As(err, &declErr) {
		t.Fatalf("Load returned a MappingDeclError for a misaligned-list-item-field block; expected a raw parse error, got: %v", declErr)
	}
	assertErrorNamesFile(t, err, store.Path())
}

// ---------------------------------------------------------------------------
// Assertion helpers
// ---------------------------------------------------------------------------

// assertToolMappingsEqual fails the test unless got and want describe the same tool
// mappings field-by-field, independent of slice header identity.
func assertToolMappingsEqual(t *testing.T, got, want []domain.ToolMapping) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("tool mapping count = %d, want %d (got=%+v, want=%+v)", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i].Generic != want[i].Generic {
			t.Fatalf("mapping[%d].Generic = %q, want %q", i, got[i].Generic, want[i].Generic)
		}
		if len(got[i].Destinations) != len(want[i].Destinations) {
			t.Fatalf("mapping[%d] destination count = %d, want %d", i, len(got[i].Destinations), len(want[i].Destinations))
		}
		for j := range want[i].Destinations {
			gd, wd := got[i].Destinations[j], want[i].Destinations[j]
			if !reflect.DeepEqual(gd, wd) {
				t.Fatalf("mapping[%d].Destinations[%d] = %+v, want %+v", i, j, gd, wd)
			}
		}
	}
}

// assertErrorNamesFile fails the test unless err's message contains path, per the
// contract that a parse-error Load result names the file the same way a MappingDeclError
// already does (ContractsDesign.md, config.UserConfigStore.Load: "the parse error
// becomes... %s: parse user config: %w", with the path added exactly once, at Load).
func assertErrorNamesFile(t *testing.T, err error, path string) {
	t.Helper()
	msg := err.Error()
	if !strings.Contains(msg, path) {
		t.Fatalf("error message %q does not name the config file path %q", msg, path)
	}
}
