package codextoml_test

// stampblock_read_test.go covers the stamp block reader (readStampBlock):
//
//   - Stamps are recovered regardless of the order they appear in the block.
//   - Blank lines inserted between stamp comment lines are ignored.
//   - A user comment above the block (before any stamp lines) does not prevent stamp
//     recovery and does not cause spurious "missing stamps" that would trigger a full
//     redeploy.
//   - CRLF line endings are tolerated; stamps are still read correctly.
//   - When stamps are deleted (the block is present but a stamp is absent), only the
//     remaining stamps are returned.
//   - A missing stamp block (no leading comments at all) yields an empty map, no error.
//   - Unknown comment lines (comments that do not match any stamp Deployed name) are not
//     carried into the result.
//   - Malformed stamp-shaped lines (wrong format, extra spaces in the key) are not
//     carried into the result.
//
// These tests exercise readStampBlock directly via the exported wrapper in export_test.go.
// They do not call Decode, so they run in isolation from the decode implementation.

import (
	"testing"

	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/agentformat/codextoml"
)

// buildStampBlock builds the raw TOML header bytes for the given stamp map,
// in agentfields.All() registry order, for use as reader test input.
func buildStampBlock(stamps map[string]string) []byte {
	var b []byte
	for _, f := range agentfields.All() {
		if v, ok := stamps[f.Deployed]; ok {
			b = append(b, []byte("# "+f.Deployed+": "+v+"\n")...)
		}
	}
	return b
}

// TestStampBlockReader_RecoverAllStamps verifies that all stamps present in the
// comment block are returned regardless of their line order.
func TestStampBlockReader_RecoverAllStamps(t *testing.T) {
	fields := agentfields.All()
	if len(fields) < 2 {
		t.Skip("need at least two stamp entries in agentfields.All()")
	}

	f0, f1 := fields[0], fields[1]

	// Provide stamps in REVERSE order relative to agentfields.All().
	reversed := []byte("# " + f1.Deployed + ": val1\n# " + f0.Deployed + ": val0\n")
	tomlRest := []byte("name = \"a\"\n")
	input := append(reversed, tomlRest...)

	stamps, _ := codextoml.ReadStampBlock(input)

	if stamps[f0.Deployed] != "val0" {
		t.Errorf("stamp %q = %q; want %q", f0.Deployed, stamps[f0.Deployed], "val0")
	}
	if stamps[f1.Deployed] != "val1" {
		t.Errorf("stamp %q = %q; want %q", f1.Deployed, stamps[f1.Deployed], "val1")
	}
}

// TestStampBlockReader_BlankLinesIgnored verifies that blank lines between and
// around stamp comment lines do not prevent recovery.
func TestStampBlockReader_BlankLinesIgnored(t *testing.T) {
	fields := agentfields.All()
	if len(fields) == 0 {
		t.Skip("no stamp entries in agentfields.All()")
	}

	f := fields[0]
	// Insert blank lines before, between, and after the stamp comment.
	input := []byte("\n# " + f.Deployed + ": v1\n\nname = \"a\"\n")

	stamps, _ := codextoml.ReadStampBlock(input)

	if stamps[f.Deployed] != "v1" {
		t.Errorf("stamp %q = %q; want %q (blank lines must not prevent stamp recovery)", f.Deployed, stamps[f.Deployed], "v1")
	}
}

// TestStampBlockReader_UserCommentAboveBlock verifies that a user comment that is not
// a stamp line does not prevent the stamps below it from being read. This is the
// anti-regression assertion: a user comment above the stamp block must not cause the
// stamps to be missed (which would trigger a spurious full redeploy).
func TestStampBlockReader_UserCommentAboveBlock(t *testing.T) {
	fields := agentfields.All()
	if len(fields) == 0 {
		t.Skip("no stamp entries in agentfields.All()")
	}

	f := fields[0]
	// A user comment precedes the stamp line.
	input := []byte("# This is a user comment, not a stamp.\n# " + f.Deployed + ": 3.0.0\nname = \"a\"\n")

	stamps, _ := codextoml.ReadStampBlock(input)

	if stamps[f.Deployed] != "3.0.0" {
		t.Errorf("stamp %q = %q; want %q (user comment above block must not prevent stamp recovery)", f.Deployed, stamps[f.Deployed], "3.0.0")
	}
}

// TestStampBlockReader_CRLF_Tolerated verifies that CRLF line endings do not prevent
// stamp recovery. The reader must strip the trailing CR before parsing.
func TestStampBlockReader_CRLF_Tolerated(t *testing.T) {
	fields := agentfields.All()
	if len(fields) == 0 {
		t.Skip("no stamp entries in agentfields.All()")
	}

	f := fields[0]
	// Build a CRLF-terminated stamp comment line.
	input := []byte("# " + f.Deployed + ": 2.1.0\r\nname = \"a\"\r\n")

	stamps, _ := codextoml.ReadStampBlock(input)

	if stamps[f.Deployed] != "2.1.0" {
		t.Errorf("stamp %q = %q; want %q (CRLF line endings must be tolerated)", f.Deployed, stamps[f.Deployed], "2.1.0")
	}
}

// TestStampBlockReader_DeletedStamp_RemainingReturned verifies that when one stamp
// is absent from the block, the remaining stamps are still returned. Only the
// stamps that are present in the block come back; absence is not an error.
func TestStampBlockReader_DeletedStamp_RemainingReturned(t *testing.T) {
	fields := agentfields.All()
	if len(fields) < 2 {
		t.Skip("need at least two stamp entries in agentfields.All()")
	}

	f0, f1 := fields[0], fields[1]
	// Only f0 is present; f1 is deleted.
	input := []byte("# " + f0.Deployed + ": present\nname = \"a\"\n")

	stamps, _ := codextoml.ReadStampBlock(input)

	if stamps[f0.Deployed] != "present" {
		t.Errorf("stamp %q = %q; want %q", f0.Deployed, stamps[f0.Deployed], "present")
	}
	if _, ok := stamps[f1.Deployed]; ok {
		t.Errorf("stamp %q is present but should be absent (it was deleted from the block)", f1.Deployed)
	}
}

// TestStampBlockReader_MissingBlock_EmptyResult verifies that when there is no
// stamp block at all (no leading comments), the reader returns an empty map and
// a zero header-end offset. This is the "missing block yields no stamps" invariant.
// The header-end offset is checked explicitly so that an implementation that wrongly
// reports a non-zero offset (claiming to have consumed header lines that do not exist)
// is caught here.
func TestStampBlockReader_MissingBlock_EmptyResult(t *testing.T) {
	// No comments at all; starts directly with a TOML key.
	input := []byte("name = \"a\"\ndescription = \"d\"\n")

	stamps, headerEnd := codextoml.ReadStampBlock(input)

	if len(stamps) != 0 {
		t.Errorf("expected empty stamp map for input with no stamp block; got: %v", stamps)
	}
	if headerEnd != 0 {
		t.Errorf("headerEnd = %d; want 0 for input with no leading comment header", headerEnd)
	}
}

// TestStampBlockReader_EmptyInput_EmptyResult verifies that empty input yields an
// empty stamp map. No panic, no error.
func TestStampBlockReader_EmptyInput_EmptyResult(t *testing.T) {
	stamps, headerEnd := codextoml.ReadStampBlock(nil)

	if len(stamps) != 0 {
		t.Errorf("expected empty stamp map for nil input; got: %v", stamps)
	}
	if headerEnd != 0 {
		t.Errorf("headerEnd = %d; want 0 for nil input", headerEnd)
	}
}

// TestStampBlockReader_UnknownCommentLines_NotCarried verifies that comment lines
// that look like stamps but use unknown (non-Deployed) keys are ignored.
func TestStampBlockReader_UnknownCommentLines_NotCarried(t *testing.T) {
	// "unknown_key" is not in agentfields.All() Deployed names.
	input := []byte("# unknown_key: some-value\nname = \"a\"\n")

	stamps, _ := codextoml.ReadStampBlock(input)

	if _, ok := stamps["unknown_key"]; ok {
		t.Error("unknown_key appeared in stamp map; only known Deployed stamp names must be carried")
	}
}

// TestStampBlockReader_MalformedLines_NotCarried verifies that malformed comment
// lines (those that do not match the "# <key>: <value>" form, or have extra
// structure the reader would not recognise) are not carried into the stamp map.
func TestStampBlockReader_MalformedLines_NotCarried(t *testing.T) {
	fields := agentfields.All()
	if len(fields) == 0 {
		t.Skip("no stamp entries in agentfields.All()")
	}

	f := fields[0]
	malformedLines := []string{
		// Comment with no colon separator.
		"# " + f.Deployed + " no-colon-value",
		// Comment that is just a hash with no content.
		"#",
		// Well-formed stamp that is NOT preceded by a colon at all.
		"# " + f.Deployed,
	}

	for _, line := range malformedLines {
		line := line
		t.Run(line, func(t *testing.T) {
			input := []byte(line + "\nname = \"a\"\n")
			stamps, _ := codextoml.ReadStampBlock(input)
			if _, ok := stamps[f.Deployed]; ok {
				t.Errorf("stamp %q was recovered from a malformed comment line %q; malformed lines must not be carried", f.Deployed, line)
			}
		})
	}
}

// TestStampBlockReader_HeaderEnd_PointsToFirstNonHeader verifies that the
// headerEnd return value points to the byte offset of the first non-comment,
// non-blank line (i.e. the start of the TOML key-value content).
func TestStampBlockReader_HeaderEnd_PointsToFirstNonHeader(t *testing.T) {
	fields := agentfields.All()
	if len(fields) == 0 {
		t.Skip("no stamp entries in agentfields.All()")
	}

	f := fields[0]
	stampLine := "# " + f.Deployed + ": 1.0\n"
	tomlPart := "name = \"a\"\n"
	input := []byte(stampLine + tomlPart)

	_, headerEnd := codextoml.ReadStampBlock(input)

	// headerEnd must point to the start of the TOML part.
	if headerEnd != len(stampLine) {
		t.Errorf("headerEnd = %d; want %d (start of first non-header line)", headerEnd, len(stampLine))
	}
	if string(input[headerEnd:]) != tomlPart {
		t.Errorf("input[headerEnd:] = %q; want %q", input[headerEnd:], tomlPart)
	}
}
