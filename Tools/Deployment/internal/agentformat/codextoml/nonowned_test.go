package codextoml_test

// nonowned_test.go pins the interim non-owned-key rule for Stage 3:
//
//   - A TOML file containing a key MOSAIC does not own (a scalar and a nested table)
//     decodes without error.
//   - The key does not appear in the decoded canonical frontmatter.
//   - Keys named "version", "id" and "role" (the Legacy names from agentfields) are
//     classified as non-owned, decoded without error, and absent from canonical output.
//
// This test exists to give the carriage stage a defined starting point and a visible
// marker of what changes there, not to specify carriage behaviour. The carriage stage
// changes the handling of non-owned keys in one direction only (keys recovered through
// the prior-bytes channel gain a canonical home); this stage's tests remain valid.
//
// The "version"/"id"/"role" case turns red if isMosaicOwned is built on
// agentfields.IsMosaicOnlyDeployedKey, which matches unprefixed Legacy names and would
// treat these three keys as owned (and thus silently drop them rather than carry them).

import (
	"testing"
)

// TestNonOwned_ScalarKey_DecodeWithoutError verifies that a TOML file carrying a
// user-owned scalar key decodes without error.
func TestNonOwned_ScalarKey_DecodeWithoutError(t *testing.T) {
	input := []byte("name = \"a\"\nmy_user_key = \"user value\"\ndeveloper_instructions = \"body\"\n")
	decodeToml(t, input, "a") // must not error
}

// TestNonOwned_ScalarKey_AbsentFromCanonical verifies that a user-owned scalar key
// does not appear in the decoded canonical frontmatter.
func TestNonOwned_ScalarKey_AbsentFromCanonical(t *testing.T) {
	input := []byte("name = \"a\"\nmy_user_key = \"user value\"\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, input, "a")
	doc := parseCanonical(t, canonical)

	if _, ok := doc.Frontmatter().Get("my_user_key"); ok {
		t.Error("user-owned scalar key appeared in canonical frontmatter; user-owned keys must not appear at canonical top level")
	}
}

// TestNonOwned_IntegerKey_DecodeWithoutError verifies that a TOML integer-valued
// user key decodes without error.
func TestNonOwned_IntegerKey_DecodeWithoutError(t *testing.T) {
	input := []byte("name = \"a\"\ndeveloper_instructions = \"body\"\nuser_count = 42\n")
	decodeToml(t, input, "a") // must not error
}

// TestNonOwned_TableKey_DecodeWithoutError verifies that a TOML nested table
// (user-owned) decodes without error. Tables are the second of the two shapes the
// test plan requires: a scalar and a nested table.
func TestNonOwned_TableKey_DecodeWithoutError(t *testing.T) {
	input := []byte("name = \"a\"\ndeveloper_instructions = \"body\"\n[mcp_servers.github]\nurl = \"https://example.com\"\n")
	decodeToml(t, input, "a") // must not error
}

// TestNonOwned_TableKey_AbsentFromCanonical verifies that a nested TOML table
// (user-owned) does not appear as an owned key in the decoded canonical frontmatter.
// The test checks both the top-level key name ("mcp_servers") and the full dotted
// key name ("mcp_servers.github") to give tighter coverage: neither form may appear
// in the canonical output.
func TestNonOwned_TableKey_AbsentFromCanonical(t *testing.T) {
	input := []byte("name = \"a\"\ndeveloper_instructions = \"body\"\n[mcp_servers.github]\nurl = \"https://example.com\"\n")

	canonical := decodeToml(t, input, "a")
	doc := parseCanonical(t, canonical)

	if _, ok := doc.Frontmatter().Get("mcp_servers"); ok {
		t.Error("user-owned table key \"mcp_servers\" appeared in canonical frontmatter; user-owned keys must not appear at canonical top level")
	}
	if _, ok := doc.Frontmatter().Get("mcp_servers.github"); ok {
		t.Error("user-owned dotted key \"mcp_servers.github\" appeared in canonical frontmatter; user-owned keys must not appear at canonical top level")
	}
}

// TestNonOwned_LegacyNames_version_DecodeWithoutError verifies that a key named
// "version" (a Legacy name in agentfields) is treated as a user-owned key: decode
// succeeds and the key is absent from the canonical frontmatter.
//
// "version" must return false from isMosaicOwned because it is not in emittedKeys and
// its Deployed name is "mosaic_version", not "version". The carriage stage requires
// this so that a user key named "version" is carriable.
func TestNonOwned_LegacyNames_version_DecodeWithoutError(t *testing.T) {
	input := []byte("name = \"a\"\nversion = \"user-version\"\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, input, "a")
	doc := parseCanonical(t, canonical)

	if _, ok := doc.Frontmatter().Get("version"); ok {
		t.Error("key \"version\" appeared in canonical frontmatter; it is a Legacy name and must be treated as non-owned (absent from canonical output)")
	}
}

// TestNonOwned_LegacyNames_id_DecodeWithoutError verifies that a key named "id"
// (Legacy of "mosaic_id") is treated as non-owned.
func TestNonOwned_LegacyNames_id_DecodeWithoutError(t *testing.T) {
	input := []byte("name = \"a\"\nid = \"user-id\"\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, input, "a")
	doc := parseCanonical(t, canonical)

	if _, ok := doc.Frontmatter().Get("id"); ok {
		t.Error("key \"id\" appeared in canonical frontmatter; it is a Legacy name and must be treated as non-owned")
	}
}

// TestNonOwned_LegacyNames_role_DecodeWithoutError verifies that a key named "role"
// (Legacy of "mosaic_role") is treated as non-owned.
func TestNonOwned_LegacyNames_role_DecodeWithoutError(t *testing.T) {
	input := []byte("name = \"a\"\nrole = \"user-role\"\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, input, "a")
	doc := parseCanonical(t, canonical)

	if _, ok := doc.Frontmatter().Get("role"); ok {
		t.Error("key \"role\" appeared in canonical frontmatter; it is a Legacy name and must be treated as non-owned")
	}
}

// TestNonOwned_MultipleUserKeys_AllAbsentFromCanonical verifies that when multiple
// user-owned keys are present alongside valid owned keys, all user-owned keys are
// absent from the canonical frontmatter and the owned keys are still present.
func TestNonOwned_MultipleUserKeys_AllAbsentFromCanonical(t *testing.T) {
	input := []byte("name = \"a\"\ndescription = \"d\"\nuser_key_one = \"val1\"\nuser_key_two = \"val2\"\nversion = \"1.0\"\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, input, "a")
	doc := parseCanonical(t, canonical)

	// User-owned keys must be absent.
	for _, key := range []string{"user_key_one", "user_key_two", "version"} {
		if _, ok := doc.Frontmatter().Get(key); ok {
			t.Errorf("user-owned key %q present in canonical frontmatter; must be absent", key)
		}
	}

	// Owned keys must still be present.
	if _, ok := doc.Frontmatter().Get("name"); !ok {
		t.Error("owned key \"name\" absent from canonical frontmatter; must be present")
	}
	if _, ok := doc.Frontmatter().Get("description"); !ok {
		t.Error("owned key \"description\" absent from canonical frontmatter; must be present")
	}
}
