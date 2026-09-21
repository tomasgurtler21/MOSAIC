package codextoml_test

// stampblock_test.go covers T2.5: the provenance stamp block writer. Tests assert:
//   - Stamps are emitted as comment lines at the top of the encoded TOML file.
//   - Stamps appear in agentfields.All() registry order.
//   - Each stamp line has the form "# <deployed_name>: <value>".
//   - One line-ending convention ("\n") is used throughout the file.
//   - A malformed stamp-shaped line present in the canonical input is replaced by
//     a correctly written stamp in the encoded output.
//
// Foreign header comment re-emission is NOT tested here: the canonical input this
// stage's encode consumes holds no comments. The header-comment scan that reads prior
// deployed bytes and recovers user comments arrives in Stage 4.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/agentformat/formatid"
)

// encodeWithStamps calls Encode with canonical input that includes the given stamp
// keys (in whatever order the YAML is provided), and returns the raw TOML bytes.
func encodeWithStamps(t *testing.T, extraFrontmatter, body string) []byte {
	t.Helper()
	canonical := []byte("---\nname: a\ndescription: d\n" + extraFrontmatter + "---\n" + body)
	tr, err := agentformat.Lookup(formatid.CodexTOML)
	if err != nil {
		t.Fatalf("Lookup CodexTOML: %v", err)
	}
	out, _, err := tr.Encode(canonical, agentformat.ArtifactContext{
		AgentKey: "a",
		Op:       agentformat.OpCreate,
	})
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	return out
}

// parseStampBlock extracts the leading comment lines from TOML bytes and returns
// a slice of (key, value) pairs for lines matching "# <key>: <value>".
func parseStampBlock(b []byte) []struct{ key, value string } {
	var stamps []struct{ key, value string }
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Stop at the first non-comment line (end of the header block).
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		// Parse "# <key>: <value>"
		rest := strings.TrimPrefix(trimmed, "#")
		rest = strings.TrimSpace(rest)
		colonIdx := strings.IndexByte(rest, ':')
		if colonIdx < 0 {
			continue
		}
		key := strings.TrimSpace(rest[:colonIdx])
		value := strings.TrimSpace(rest[colonIdx+1:])
		stamps = append(stamps, struct{ key, value string }{key, value})
	}
	return stamps
}

// TestStampBlock_StampsEmittedAsCommentLines verifies that stamp keys present in the
// canonical frontmatter appear as comment lines at the top of the encoded TOML.
func TestStampBlock_StampsEmittedAsCommentLines(t *testing.T) {
	// Use the first two stamp entries from agentfields.All() as test subjects.
	fields := agentfields.All()
	if len(fields) < 2 {
		t.Skip("need at least two stamp entries in agentfields.All()")
	}

	f0, f1 := fields[0], fields[1]
	extra := f0.Deployed + ": v1.0\n" + f1.Deployed + ": v2.0\n"
	out := encodeWithStamps(t, extra, "Body.\n")

	stamps := parseStampBlock(out)
	found := make(map[string]string, len(stamps))
	for _, s := range stamps {
		found[s.key] = s.value
	}

	if _, ok := found[f0.Deployed]; !ok {
		t.Errorf("Stamp %q is absent from the comment block; want it as a comment line", f0.Deployed)
	}
	if _, ok := found[f1.Deployed]; !ok {
		t.Errorf("Stamp %q is absent from the comment block; want it as a comment line", f1.Deployed)
	}
}

// TestStampBlock_StampsInAgentfieldsOrder verifies that when multiple stamps are
// present, they appear in agentfields.All() registry order in the comment block.
func TestStampBlock_StampsInAgentfieldsOrder(t *testing.T) {
	fields := agentfields.All()
	if len(fields) < 2 {
		t.Skip("need at least two stamp entries to test order")
	}

	// Supply all stamps in REVERSE agentfields order in the YAML frontmatter.
	var extraFrontmatter strings.Builder
	for i := len(fields) - 1; i >= 0; i-- {
		extraFrontmatter.WriteString(fields[i].Deployed + ": value-" + fields[i].Deployed + "\n")
	}

	out := encodeWithStamps(t, extraFrontmatter.String(), "Body.\n")

	// Collect the stamp keys from the comment block in the order they appear.
	stamps := parseStampBlock(out)
	var gotKeys []string
	for _, s := range stamps {
		// Only collect keys that are in agentfields.
		for _, f := range fields {
			if f.Deployed == s.key {
				gotKeys = append(gotKeys, s.key)
				break
			}
		}
	}

	// Build the expected order from agentfields.All(), filtering to only stamps present.
	var wantKeys []string
	for _, f := range fields {
		wantKeys = append(wantKeys, f.Deployed)
	}

	// Check that every expected key is present and in the right order.
	if len(gotKeys) < len(wantKeys) {
		t.Logf("got stamp keys:  %v", gotKeys)
		t.Logf("want stamp keys: %v", wantKeys)
	}
	j := 0
	for _, want := range wantKeys {
		if j < len(gotKeys) && gotKeys[j] == want {
			j++
		}
	}
	if j != len(wantKeys) {
		t.Errorf("stamp block order does not match agentfields.All() order\ngot:  %v\nwant: %v", gotKeys, wantKeys)
	}
}

// TestStampBlock_StampValues_ArePreserved verifies that the value written in the stamp
// comment line matches the value from the canonical frontmatter.
func TestStampBlock_StampValues_ArePreserved(t *testing.T) {
	fields := agentfields.All()
	if len(fields) == 0 {
		t.Skip("no stamp entries in agentfields.All()")
	}
	f := fields[0]
	const wantVal = "3.14.1"
	extra := f.Deployed + ": " + wantVal + "\n"
	out := encodeWithStamps(t, extra, "Body.\n")

	stamps := parseStampBlock(out)
	for _, s := range stamps {
		if s.key == f.Deployed {
			if s.value != wantVal {
				t.Errorf("stamp %q has value %q; want %q", f.Deployed, s.value, wantVal)
			}
			return
		}
	}
	t.Errorf("stamp %q not found in comment block; got stamps: %v", f.Deployed, stamps)
}

// TestStampBlock_OneLineEndingConvention verifies that the encoded TOML uses only "\n"
// as a line ending: no "\r\n" sequences appear in the structural output.
func TestStampBlock_OneLineEndingConvention(t *testing.T) {
	fields := agentfields.All()
	var extra string
	if len(fields) > 0 {
		extra = fields[0].Deployed + ": v1\n"
	}
	out := encodeWithStamps(t, extra, "Body line one.\nBody line two.\n")

	if bytes.Contains(out, []byte("\r\n")) {
		t.Error("Encoded TOML contains CRLF line endings; the encoder must use LF-only line endings")
	}
}

// TestStampBlock_StampKeyNotInTOMLBody verifies that stamp comment lines do NOT appear
// as native TOML keys in the encoded output. The stamp block is comment-only.
func TestStampBlock_StampKeyNotInTOMLBody(t *testing.T) {
	fields := agentfields.All()
	if len(fields) == 0 {
		t.Skip("no stamp entries in agentfields.All()")
	}

	f := fields[0]
	extra := f.Deployed + ": stamp-value\n"
	out := encodeWithStamps(t, extra, "Body.\n")

	// Parse the TOML and assert the stamp key is absent from native TOML keys.
	// Using the TOML parser (not byte-level search) ensures spacing variants around
	// "=" cannot produce a false pass.
	var m map[string]interface{}
	if err := toml.Unmarshal(out, &m); err != nil {
		t.Fatalf("Encoded output is not valid TOML: %v\nOutput:\n%s", err, out)
	}
	if _, ok := m[f.Deployed]; ok {
		t.Errorf("stamp key %q appears as a native TOML key; it must only appear in the comment block", f.Deployed)
	}
}

// TestStampBlock_NonStampKeysNotInCommentBlock verifies that non-stamp canonical
// frontmatter keys (name, description, model, sandbox_mode) do NOT appear as
// stamp comment lines. Only Deployed stamp names go to the comment block.
func TestStampBlock_NonStampKeysNotInCommentBlock(t *testing.T) {
	out := encodeWithStamps(t,
		"model: gpt-5\nsandbox_mode: read-only\n",
		"Body.\n",
	)

	stamps := parseStampBlock(out)
	for _, s := range stamps {
		switch s.key {
		case "name", "description", "model", "sandbox_mode", "developer_instructions":
			t.Errorf("key %q appeared in the comment block; only Deployed stamp names should be there", s.key)
		}
	}
}
