package docformat_test

// Tests for frontmatter edit operations (T3.4).
//
// Coverage:
//   - Set overwrites an existing key's value in place.
//   - Set appends a new key when it does not already exist.
//   - Editing one key leaves all other serialised lines byte-identical (AC3.3).
//   - Insert(Before) places the new key immediately before the named key.
//   - Insert(After) places the new key immediately after the named key.
//   - Insert with a zero Position appends the key at the end.
//   - Remove deletes the named key and returns true.
//   - Remove on an absent key returns false and makes no change.
//   - Reorder produces the requested key sequence.
//   - Keys not listed in Reorder keep their relative order and are appended after listed keys.
//   - Reorder followed by Bytes produces a document that still round-trips (no corruption).

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-deploy/internal/docformat"
	"mosaic-deploy/internal/domain"
)

// sourceWithKeys builds a minimal MOSAIC document whose frontmatter contains the given
// key:value pairs (all plain scalars) in order.
func sourceWithKeys(pairs ...string) []byte {
	if len(pairs)%2 != 0 {
		panic("sourceWithKeys requires an even number of arguments (key, value, ...)")
	}
	var sb strings.Builder
	sb.WriteString("---\n")
	for i := 0; i < len(pairs); i += 2 {
		sb.WriteString(pairs[i])
		sb.WriteString(": ")
		sb.WriteString(pairs[i+1])
		sb.WriteString("\n")
	}
	sb.WriteString("---\n")
	return []byte(sb.String())
}

// --- Set: overwrite existing key ---

func TestFrontmatter_Set_ExistingKey_UpdatesScalarValue(t *testing.T) {
	src := sourceWithKeys("name", "old-name", "version", "1.0.0")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	err = doc.Frontmatter().Set("name", domain.ScalarValue("new-name", domain.QuotePlain))

	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, ok := doc.Frontmatter().Get("name")
	if !ok {
		t.Fatal("Get(\"name\") returned false after Set")
	}
	if v.Scalar != "new-name" {
		t.Errorf("expected scalar %q after Set, got %q", "new-name", v.Scalar)
	}
}

func TestFrontmatter_Set_ExistingKey_KeyOrderUnchanged(t *testing.T) {
	src := sourceWithKeys("id", "1", "name", "test", "version", "1.0.0")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	wantOrder := []string{"id", "name", "version"}

	_ = doc.Frontmatter().Set("name", domain.ScalarValue("updated", domain.QuotePlain))

	gotKeys := doc.Frontmatter().Keys()
	if len(gotKeys) != len(wantOrder) {
		t.Fatalf("expected %d keys after Set, got %d: %v", len(wantOrder), len(gotKeys), gotKeys)
	}
	for i, want := range wantOrder {
		if gotKeys[i] != want {
			t.Errorf("keys[%d]: want %q, got %q", i, want, gotKeys[i])
		}
	}
}

// --- Set: new key (append) ---

func TestFrontmatter_Set_NewKey_AppendsToEnd(t *testing.T) {
	src := sourceWithKeys("name", "test", "version", "1.0.0")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	err = doc.Frontmatter().Set("added-key", domain.ScalarValue("added-value", domain.QuotePlain))

	if err != nil {
		t.Fatalf("Set new key: %v", err)
	}
	keys := doc.Frontmatter().Keys()
	if keys[len(keys)-1] != "added-key" {
		t.Errorf("expected new key to be last, got keys: %v", keys)
	}
}

// --- Byte fidelity when editing (AC3.3) ---

func TestFrontmatter_Set_EditingOneKey_LeavesOtherLinesByteIdentical(t *testing.T) {
	// Parse a rich fixture, change one scalar, then verify every other line in the
	// serialised output matches the original source line-for-line.
	src := fixtureBytes(t, "preserve-key-order.md")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Change only "name".
	err = doc.Frontmatter().Set("name", domain.ScalarValue("changed-name", domain.QuotePlain))
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	original := strings.Split(string(src), "\n")
	modified := strings.Split(string(doc.Bytes()), "\n")

	// Scan every line: any line that does not contain "changed-name" or "name:" must be
	// byte-identical to the corresponding original line.
	for i, origLine := range original {
		if i >= len(modified) {
			break
		}
		if strings.HasPrefix(strings.TrimSpace(origLine), "name:") {
			continue // this is the line we changed — skip it
		}
		if origLine != modified[i] {
			t.Errorf("line %d changed unexpectedly:\n  original: %q\n  modified: %q", i+1, origLine, modified[i])
		}
	}
}

// --- Insert: Before ---

func TestFrontmatter_Insert_Before_PlacesKeyAtCorrectPosition(t *testing.T) {
	src := sourceWithKeys("alpha", "1", "gamma", "3")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	err = doc.Frontmatter().Insert("beta", domain.ScalarValue("2", domain.QuotePlain),
		docformat.Position{Before: "gamma"})

	if err != nil {
		t.Fatalf("Insert Before: %v", err)
	}
	keys := doc.Frontmatter().Keys()
	wantOrder := []string{"alpha", "beta", "gamma"}
	if len(keys) != len(wantOrder) {
		t.Fatalf("expected keys %v, got %v", wantOrder, keys)
	}
	for i, want := range wantOrder {
		if keys[i] != want {
			t.Errorf("keys[%d]: want %q, got %q", i, want, keys[i])
		}
	}
}

// --- Insert: After ---

func TestFrontmatter_Insert_After_PlacesKeyAtCorrectPosition(t *testing.T) {
	src := sourceWithKeys("alpha", "1", "gamma", "3")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	err = doc.Frontmatter().Insert("beta", domain.ScalarValue("2", domain.QuotePlain),
		docformat.Position{After: "alpha"})

	if err != nil {
		t.Fatalf("Insert After: %v", err)
	}
	keys := doc.Frontmatter().Keys()
	wantOrder := []string{"alpha", "beta", "gamma"}
	if len(keys) != len(wantOrder) {
		t.Fatalf("expected keys %v, got %v", wantOrder, keys)
	}
	for i, want := range wantOrder {
		if keys[i] != want {
			t.Errorf("keys[%d]: want %q, got %q", i, want, keys[i])
		}
	}
}

// --- Insert: zero Position (append) ---

func TestFrontmatter_Insert_ZeroPosition_Appends(t *testing.T) {
	src := sourceWithKeys("alpha", "1", "beta", "2")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	err = doc.Frontmatter().Insert("gamma", domain.ScalarValue("3", domain.QuotePlain),
		docformat.Position{})

	if err != nil {
		t.Fatalf("Insert with zero Position: %v", err)
	}
	keys := doc.Frontmatter().Keys()
	if keys[len(keys)-1] != "gamma" {
		t.Errorf("expected appended key to be last, got keys: %v", keys)
	}
}

// --- Remove ---

func TestFrontmatter_Remove_ExistingKey_RemovesItAndReturnsTrue(t *testing.T) {
	src := sourceWithKeys("alpha", "1", "beta", "2", "gamma", "3")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	removed := doc.Frontmatter().Remove("beta")

	if !removed {
		t.Error("Remove returned false for a key that exists")
	}
	keys := doc.Frontmatter().Keys()
	for _, k := range keys {
		if k == "beta" {
			t.Error("\"beta\" still present in Keys() after Remove")
		}
	}
}

func TestFrontmatter_Remove_ExistingKey_OtherKeysSurvive(t *testing.T) {
	src := sourceWithKeys("alpha", "1", "beta", "2", "gamma", "3")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	doc.Frontmatter().Remove("beta")

	keys := doc.Frontmatter().Keys()
	wantOrder := []string{"alpha", "gamma"}
	if len(keys) != len(wantOrder) {
		t.Fatalf("expected keys %v after Remove, got %v", wantOrder, keys)
	}
	for i, want := range wantOrder {
		if keys[i] != want {
			t.Errorf("keys[%d]: want %q, got %q", i, want, keys[i])
		}
	}
}

func TestFrontmatter_Remove_AbsentKey_ReturnsFalse(t *testing.T) {
	src := sourceWithKeys("alpha", "1")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	removed := doc.Frontmatter().Remove("nonexistent")

	if removed {
		t.Error("Remove should return false for a key that does not exist")
	}
}

func TestFrontmatter_Remove_AbsentKey_KeyCountUnchanged(t *testing.T) {
	src := sourceWithKeys("alpha", "1", "beta", "2")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	before := len(doc.Frontmatter().Keys())

	doc.Frontmatter().Remove("nonexistent")

	after := len(doc.Frontmatter().Keys())
	if after != before {
		t.Errorf("key count changed after removing an absent key: was %d, now %d", before, after)
	}
}

// --- Insert: missing anchor key ---

func TestFrontmatter_Insert_Before_MissingAnchorKey_ReturnsError(t *testing.T) {
	// Specifying Position{Before: "nonexistent"} when the named key is absent must return
	// a non-nil error. This pins the contract: Insert does not silently fall back to
	// append when the caller has named a specific anchor that does not exist.
	src := sourceWithKeys("alpha", "1", "beta", "2")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	err = doc.Frontmatter().Insert("gamma", domain.ScalarValue("3", domain.QuotePlain),
		docformat.Position{Before: "nonexistent"})

	if err == nil {
		t.Error("Insert with Before pointing to a missing anchor key must return an error")
	}
}

func TestFrontmatter_Insert_Before_MissingAnchorKey_FrontmatterUnchanged(t *testing.T) {
	// When Insert fails because the Before anchor is missing, the frontmatter must not
	// be modified: no partial insertion, no change in key count.
	src := sourceWithKeys("alpha", "1", "beta", "2")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	before := doc.Frontmatter().Keys()

	_ = doc.Frontmatter().Insert("gamma", domain.ScalarValue("3", domain.QuotePlain),
		docformat.Position{Before: "nonexistent"})

	after := doc.Frontmatter().Keys()
	if len(after) != len(before) {
		t.Errorf("frontmatter key count changed after Insert error: was %d, got %d", len(before), len(after))
	}
}

func TestFrontmatter_Insert_After_MissingAnchorKey_ReturnsError(t *testing.T) {
	// Specifying Position{After: "nonexistent"} when the named key is absent must return
	// a non-nil error. Same contract as Before, applied to the After anchor.
	src := sourceWithKeys("alpha", "1", "beta", "2")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	err = doc.Frontmatter().Insert("gamma", domain.ScalarValue("3", domain.QuotePlain),
		docformat.Position{After: "nonexistent"})

	if err == nil {
		t.Error("Insert with After pointing to a missing anchor key must return an error")
	}
}

func TestFrontmatter_Insert_After_MissingAnchorKey_FrontmatterUnchanged(t *testing.T) {
	// When Insert fails because the After anchor is missing, the frontmatter must not
	// be modified: no partial insertion, no change in key count.
	src := sourceWithKeys("alpha", "1", "beta", "2")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	before := doc.Frontmatter().Keys()

	_ = doc.Frontmatter().Insert("gamma", domain.ScalarValue("3", domain.QuotePlain),
		docformat.Position{After: "nonexistent"})

	after := doc.Frontmatter().Keys()
	if len(after) != len(before) {
		t.Errorf("frontmatter key count changed after Insert error: was %d, got %d", len(before), len(after))
	}
}

// --- Set: list and mapping values ---

func TestFrontmatter_Set_ListValue_FlowStyle_RoundTrips(t *testing.T) {
	// Set with a KindList value using flow style. After serialising and re-parsing the
	// document, the key must report Kind == KindList with the declared items and ListStyle.
	// The transform pipeline calls Set with KindList values for the "tools" key.
	src := sourceWithKeys("name", "test", "version", "1.0.0")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	tools := domain.ListValue([]domain.FieldValue{
		domain.ScalarValue("read-file", domain.QuotePlain),
		domain.ScalarValue("write-file", domain.QuotePlain),
	}, domain.ListFlow)

	if err := doc.Frontmatter().Set("tools", tools); err != nil {
		t.Fatalf("Set KindList: %v", err)
	}

	// Re-parse the serialised form to verify the round-trip is stable.
	doc2, err := docformat.Parse(doc.Bytes())
	if err != nil {
		t.Fatalf("Parse of document after Set KindList: %v", err)
	}

	v, ok := doc2.Frontmatter().Get("tools")
	if !ok {
		t.Fatal("Get(\"tools\") returned false after Set KindList round-trip")
	}
	if v.Kind != domain.KindList {
		t.Errorf("expected Kind == KindList, got %q", v.Kind)
	}
	if len(v.Items) != 2 {
		t.Fatalf("expected 2 list items, got %d", len(v.Items))
	}
	if v.Items[0].Scalar != "read-file" {
		t.Errorf("items[0]: want %q, got %q", "read-file", v.Items[0].Scalar)
	}
	if v.Items[1].Scalar != "write-file" {
		t.Errorf("items[1]: want %q, got %q", "write-file", v.Items[1].Scalar)
	}
	if v.List != domain.ListFlow {
		t.Errorf("expected ListFlow style, got %q", v.List)
	}
}

func TestFrontmatter_Set_MappingValue_RoundTrips(t *testing.T) {
	// Set with a KindMapping value. After serialising and re-parsing the document, the
	// key must report Kind == KindMapping with the correct pairs in their declared order.
	// The transform pipeline calls Set with KindMapping values for the "permission" key.
	src := sourceWithKeys("name", "test", "version", "1.0.0")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	perm := domain.MappingValue([]domain.FieldPair{
		{Key: "allow", Value: domain.ListValue([]domain.FieldValue{
			domain.ScalarValue("read", domain.QuotePlain),
		}, domain.ListFlow)},
		{Key: "deny", Value: domain.ListValue([]domain.FieldValue{
			domain.ScalarValue("write", domain.QuotePlain),
		}, domain.ListFlow)},
	})

	if err := doc.Frontmatter().Set("permission", perm); err != nil {
		t.Fatalf("Set KindMapping: %v", err)
	}

	doc2, err := docformat.Parse(doc.Bytes())
	if err != nil {
		t.Fatalf("Parse of document after Set KindMapping: %v", err)
	}

	v, ok := doc2.Frontmatter().Get("permission")
	if !ok {
		t.Fatal("Get(\"permission\") returned false after Set KindMapping round-trip")
	}
	if v.Kind != domain.KindMapping {
		t.Errorf("expected Kind == KindMapping, got %q", v.Kind)
	}
	if len(v.Pairs) != 2 {
		t.Fatalf("expected 2 mapping pairs, got %d", len(v.Pairs))
	}
	if v.Pairs[0].Key != "allow" {
		t.Errorf("pairs[0].Key: want %q, got %q", "allow", v.Pairs[0].Key)
	}
	if v.Pairs[1].Key != "deny" {
		t.Errorf("pairs[1].Key: want %q, got %q", "deny", v.Pairs[1].Key)
	}
}

// --- Reorder ---

func TestFrontmatter_Reorder_ListedKeysAppearFirst(t *testing.T) {
	src := sourceWithKeys("alpha", "1", "beta", "2", "gamma", "3")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	err = doc.Frontmatter().Reorder([]string{"gamma", "alpha", "beta"})

	if err != nil {
		t.Fatalf("Reorder: %v", err)
	}
	keys := doc.Frontmatter().Keys()
	wantOrder := []string{"gamma", "alpha", "beta"}
	for i, want := range wantOrder {
		if i >= len(keys) || keys[i] != want {
			t.Errorf("keys[%d]: want %q, got %q (full: %v)", i, want, keys[i], keys)
		}
	}
}

func TestFrontmatter_Reorder_UnlistedKeysAppendedAfterInOriginalRelativeOrder(t *testing.T) {
	src := sourceWithKeys("alpha", "1", "beta", "2", "gamma", "3", "delta", "4")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Reorder lists only gamma and alpha; beta and delta are unlisted.
	err = doc.Frontmatter().Reorder([]string{"gamma", "alpha"})

	if err != nil {
		t.Fatalf("Reorder: %v", err)
	}
	keys := doc.Frontmatter().Keys()
	// Expected: gamma, alpha (listed in order), then beta, delta (unlisted, original relative order).
	wantOrder := []string{"gamma", "alpha", "beta", "delta"}
	if len(keys) != len(wantOrder) {
		t.Fatalf("expected keys %v, got %v", wantOrder, keys)
	}
	for i, want := range wantOrder {
		if keys[i] != want {
			t.Errorf("keys[%d]: want %q, got %q", i, want, keys[i])
		}
	}
}

func TestFrontmatter_Reorder_DocumentRoundTripsAfterReorder(t *testing.T) {
	// A reordered document must still be parseable and produce the same key set.
	src := sourceWithKeys("alpha", "1", "beta", "2", "gamma", "3")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	_ = doc.Frontmatter().Reorder([]string{"gamma", "alpha", "beta"})
	reordered := doc.Bytes()

	doc2, err := docformat.Parse(reordered)
	if err != nil {
		t.Fatalf("Parse of reordered document: %v", err)
	}

	// Bytes should be stable: a second parse-bytes cycle must yield the same bytes.
	if !bytes.Equal(reordered, doc2.Bytes()) {
		t.Error("reordered document is not stable across a second parse-bytes cycle")
	}

	keys := doc2.Frontmatter().Keys()
	wantOrder := []string{"gamma", "alpha", "beta"}
	if len(keys) != len(wantOrder) {
		t.Fatalf("reordered document has %d keys, want %d", len(keys), len(wantOrder))
	}
}
