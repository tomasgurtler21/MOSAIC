package pricing_test

// Tests for the pricing config store (internal/pricing).
//
// Coverage map:
//   T7.1 — Loading: well-formed file, absent file, comments-only file, malformed YAML
//   T7.2 — Lookup: configured model found, unconfigured model returns not-configured outcome,
//           lookup is by model ID alone (harness not part of key)
//   T7.3 — Validation: threshold entry has both output tiers, absent/null threshold yields
//           no long-context tier, invalid entry produces a named finding, one invalid entry
//           does not prevent valid entries from loading, threshold-present-but-missing-at-rate
//           is a distinct validation failure, entries with non-numeric or negative rate strings
//           are skipped with a named finding and do not prevent sibling entries from loading
//   T7.4 — Persistence: round-trip via Put then Load, existing entries preserved, absent
//           file and its parent directory are created, no-threshold entry reloads correctly,
//           rates survive serialisation without precision loss, pre-existing comments survive
//           a Put call in the raw file bytes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-log-analyzer/internal/domain"
	"mosaic-log-analyzer/internal/pricing"
)

// ---------------------------------------------------------------------------
// Helpers and test fixtures
// ---------------------------------------------------------------------------

// newMinimalEntry returns a valid ModelPricing with all required rates set and no
// long-context tier. Rate values are derived via ParseRate to ensure they match the
// internal nanodollar representation.
func newMinimalEntry(model domain.ModelID) domain.ModelPricing {
	inputRate, _ := domain.ParseRate("3.00")
	cachedRate, _ := domain.ParseRate("0.30")
	writeRate, _ := domain.ParseRate("3.75")
	underRate, _ := domain.ParseRate("15.00")
	return domain.ModelPricing{
		Model:                model,
		Input:                inputRate,
		CachedInput:          cachedRate,
		CacheWrite:           writeRate,
		OutputUnderThreshold: underRate,
		HasThreshold:         false,
	}
}

// newEntryWithThreshold returns a valid ModelPricing that includes a long-context tier.
func newEntryWithThreshold(model domain.ModelID) domain.ModelPricing {
	e := newMinimalEntry(model)
	atRate, _ := domain.ParseRate("22.50")
	e.OutputAtThreshold = atRate
	e.ContextLengthThreshold = 200_000
	e.HasThreshold = true
	return e
}

// makeRootWithConfigDir creates a temp root that already contains the pricing config
// directory. The returned pricingPath is the location where pricing.yaml should be
// written by the test.
func makeRootWithConfigDir(t *testing.T) (root string, pricingPath string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "MosaicLogAnalyzer", "config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("makeRootWithConfigDir: create config dir: %v", err)
	}
	return root, filepath.Join(dir, "pricing.yaml")
}

// writePricingYAML writes content to the given path.
func writePricingYAML(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writePricingYAML: %v", err)
	}
}

// ---------------------------------------------------------------------------
// YAML fixture strings
// ---------------------------------------------------------------------------

// wellFormedOneModel has a single fully-specified model without a long-context tier.
const wellFormedOneModel = `
models:
  test-model-a:
    input: "3.00"
    cached_input: "0.30"
    cache_write: "3.75"
    output_under_threshold: "15.00"
`

// wellFormedTwoModels has two valid entries: one without and one with a threshold.
const wellFormedTwoModels = `
models:
  test-model-a:
    input: "3.00"
    cached_input: "0.30"
    cache_write: "3.75"
    output_under_threshold: "15.00"
  test-model-b:
    input: "1.00"
    cached_input: "0.10"
    cache_write: "1.25"
    output_under_threshold: "5.00"
    output_at_threshold: "8.00"
    context_length_threshold: 100000
`

// nullThresholdYAML has a model with context_length_threshold explicitly set to null.
// Null must be treated identically to an absent field: no long-context tier.
const nullThresholdYAML = `
models:
  test-model-a:
    input: "3.00"
    cached_input: "0.30"
    cache_write: "3.75"
    output_under_threshold: "15.00"
    output_at_threshold: "22.50"
    context_length_threshold: null
`

// commentsOnlyYAML simulates the shipped pricing.yaml that is documented-but-empty:
// all active content is commented out and no models map key is present.
const commentsOnlyYAML = `
# MosaicLogAnalyzer pricing configuration
#
# models:
#   example-model:
#     input: "3.00"
#     cached_input: "0.30"
#     cache_write: "3.75"
#     output_under_threshold: "15.00"

# No active model prices are defined in this file.
`

// malformedYAML is structurally invalid; it cannot be parsed as a YAML document.
const malformedYAML = `models: {bad yaml: [`

// oneInvalidOneValidYAML has one entry missing a required rate (output_under_threshold)
// and one entry that is fully valid. Used to verify error isolation.
const oneInvalidOneValidYAML = `
models:
  invalid-model:
    input: "3.00"
    cached_input: "0.30"
    cache_write: "3.75"
    # output_under_threshold is absent — this entry is missing a required rate
  valid-model:
    input: "1.00"
    cached_input: "0.10"
    cache_write: "1.25"
    output_under_threshold: "5.00"
`

// thresholdPresentMissingAtRateYAML has an entry that sets context_length_threshold
// (making HasThreshold true) but omits output_at_threshold. This exercises the
// Validate() branch `HasThreshold && OutputAtThreshold == 0`, which is distinct
// from the always-required-rate branch covered by oneInvalidOneValidYAML.
const thresholdPresentMissingAtRateYAML = `
models:
  threshold-no-at-rate:
    input: "3.00"
    cached_input: "0.30"
    cache_write: "3.75"
    output_under_threshold: "15.00"
    context_length_threshold: 200000
    # output_at_threshold is absent — required when context_length_threshold is set
  valid-model:
    input: "1.00"
    cached_input: "0.10"
    cache_write: "1.25"
    output_under_threshold: "5.00"
`

// nonNumericRateYAML has an entry that is syntactically valid YAML but contains a
// rate value that ParseRate rejects (non-numeric string). This is distinct from a
// malformed YAML document (document-level parse error) and from a missing/absent
// rate field (zero Rate after YAML unmarshalling).
const nonNumericRateYAML = `
models:
  bad-rate-model:
    input: "abc"
    cached_input: "0.30"
    cache_write: "3.75"
    output_under_threshold: "15.00"
  valid-model:
    input: "1.00"
    cached_input: "0.10"
    cache_write: "1.25"
    output_under_threshold: "5.00"
`

// negativeRateYAML has an entry where a rate string is a negative decimal, which
// ParseRate rejects. Negative rates are rejected by ParseRate and must cause the
// entry to be skipped with a named FindingInvalidPricingEntry.
const negativeRateYAML = `
models:
  negative-rate-model:
    input: "-3.00"
    cached_input: "0.30"
    cache_write: "3.75"
    output_under_threshold: "15.00"
  valid-model:
    input: "1.00"
    cached_input: "0.10"
    cache_write: "1.25"
    output_under_threshold: "5.00"
`

// commentedPricingYAML has a prominent comment and one existing model entry.
// Used to verify that Put preserves pre-existing comments in the raw file bytes.
const commentedPricingYAML = `# user note: these are our production prices
models:
  existing-model:
    input: "3.00"
    cached_input: "0.30"
    cache_write: "3.75"
    output_under_threshold: "15.00"
`

// ---------------------------------------------------------------------------
// T7.1 — Loading
// ---------------------------------------------------------------------------

func TestStore_Load_WellFormedFile_ReturnsExpectedTable(t *testing.T) {
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, wellFormedOneModel)

	store := pricing.NewStore(root)
	table, findings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load must not return an error for a well-formed file: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("Load of a well-formed file must produce no findings; got %d", len(findings))
	}
	if table.Len() != 1 {
		t.Fatalf("table must contain 1 entry; got %d", table.Len())
	}
	entry, found := table.Lookup("test-model-a")
	if !found {
		t.Fatal("test-model-a must be found in the loaded table")
	}
	if entry.Model != "test-model-a" {
		t.Errorf("entry.Model = %q, want %q", entry.Model, "test-model-a")
	}
	if entry.Input == 0 {
		t.Error("loaded Input rate must be non-zero")
	}
	if entry.CachedInput == 0 {
		t.Error("loaded CachedInput rate must be non-zero")
	}
	if entry.CacheWrite == 0 {
		t.Error("loaded CacheWrite rate must be non-zero")
	}
	if entry.OutputUnderThreshold == 0 {
		t.Error("loaded OutputUnderThreshold rate must be non-zero")
	}
}

func TestStore_Load_MultipleEntries_AllLoaded(t *testing.T) {
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, wellFormedTwoModels)

	store := pricing.NewStore(root)
	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if table.Len() != 2 {
		t.Errorf("table must contain 2 entries; got %d", table.Len())
	}
	if _, found := table.Lookup("test-model-a"); !found {
		t.Error("test-model-a must be in the table")
	}
	entryB, found := table.Lookup("test-model-b")
	if !found {
		t.Error("test-model-b must be in the table")
	} else {
		// test-model-b is the thresholded entry in wellFormedTwoModels; verify the
		// threshold flag is captured correctly so the fixture's second entry is
		// exercised beyond a mere presence check.
		if !entryB.HasThreshold {
			t.Error("test-model-b (the thresholded entry) must have HasThreshold=true")
		}
		if entryB.ContextLengthThreshold == 0 {
			t.Error("test-model-b must have a non-zero ContextLengthThreshold")
		}
	}
}

func TestStore_Load_AbsentFile_ReturnsEmptyTableAndNoError(t *testing.T) {
	// No pricing.yaml exists anywhere under this root.
	root := t.TempDir()
	store := pricing.NewStore(root)

	table, findings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load with absent file must return nil error; got: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("absent file must produce no findings; got %d", len(findings))
	}
	if table.Len() != 0 {
		t.Errorf("absent file must yield an empty table; got %d entries", table.Len())
	}
}

func TestStore_Load_CommentsOnlyFile_ReturnsEmptyTable(t *testing.T) {
	// The shipped pricing.yaml has no active model entries — only comments.
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, commentsOnlyYAML)

	store := pricing.NewStore(root)
	table, findings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load of a comments-only file must not return an error: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("comments-only file must produce no findings; got %d", len(findings))
	}
	if table.Len() != 0 {
		t.Errorf("comments-only file must yield an empty table; got %d entries", table.Len())
	}
}

func TestStore_Load_MalformedYAML_ReturnsErrorAndEmptyTable(t *testing.T) {
	// A document-level parse failure must return a non-nil error and an empty table
	// (never a partial table from whatever was decoded before the error).
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, malformedYAML)

	store := pricing.NewStore(root)
	table, _, err := store.Load(context.Background())
	if err == nil {
		t.Fatal("Load of a malformed YAML document must return a non-nil error")
	}
	if table.Len() != 0 {
		t.Errorf("malformed YAML must yield an empty table (never partial); got %d entries", table.Len())
	}
}

// ---------------------------------------------------------------------------
// T7.2 — Lookup
// ---------------------------------------------------------------------------

func TestStore_Load_ConfiguredModel_IsFound(t *testing.T) {
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, wellFormedOneModel)

	store := pricing.NewStore(root)
	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, found := table.Lookup("test-model-a")
	if !found {
		t.Error("a model present in the YAML must be found in the loaded table")
	}
}

func TestStore_Load_UnconfiguredModel_ReturnsNotConfiguredOutcome(t *testing.T) {
	// Lookup for a model not in the YAML must return found=false — the explicit
	// not-configured outcome — never a zero-valued ModelPricing masquerading as $0.
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, wellFormedOneModel) // only test-model-a

	store := pricing.NewStore(root)
	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, found := table.Lookup("unconfigured-model")
	if found {
		t.Error("lookup for a model absent from the YAML must return found=false")
	}
	// Verify the configured model is still reachable to confirm the table loaded correctly.
	_, foundConfigured := table.Lookup("test-model-a")
	if !foundConfigured {
		t.Error("the configured model must still return found=true")
	}
}

func TestStore_Load_LookupByModelIDOnly_HarnessNotPartOfKey(t *testing.T) {
	// The pricing YAML is keyed by bare model ID only; harness is never included
	// in the key. A model stored as "test-model-a" must be found by that exact ID
	// and must NOT be found by any harness-qualified variant.
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, `
models:
  test-model-a:
    input: "3.00"
    cached_input: "0.30"
    cache_write: "3.75"
    output_under_threshold: "15.00"
`)

	store := pricing.NewStore(root)
	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Bare model ID lookup must succeed.
	if _, found := table.Lookup("test-model-a"); !found {
		t.Error("bare model ID lookup must find the entry")
	}

	// Harness-qualified variants must not be found: the key never includes harness.
	for _, harnessQualified := range []domain.ModelID{
		"claude-code/test-model-a",
		"opencode/test-model-a",
		"vscode-ghcp/test-model-a",
	} {
		if _, found := table.Lookup(harnessQualified); found {
			t.Errorf("harness-qualified key %q must not be found; lookup is by model ID alone", harnessQualified)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.3 — Validation
// ---------------------------------------------------------------------------

func TestStore_Load_EntryWithThreshold_HasBothOutputTiers(t *testing.T) {
	// An entry that specifies context_length_threshold must expose both output
	// rate tiers and report HasThreshold=true.
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, wellFormedTwoModels) // test-model-b has a threshold

	store := pricing.NewStore(root)
	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	entry, found := table.Lookup("test-model-b")
	if !found {
		t.Fatal("test-model-b must be in the table")
	}
	if !entry.HasThreshold {
		t.Error("entry with context_length_threshold must have HasThreshold=true")
	}
	if !entry.HasLongContextTier() {
		t.Error("entry with context_length_threshold must HasLongContextTier() == true")
	}
	if entry.OutputAtThreshold == 0 {
		t.Error("entry with threshold must have a non-zero OutputAtThreshold rate")
	}
	if entry.ContextLengthThreshold == 0 {
		t.Error("entry with threshold must have a non-zero ContextLengthThreshold value")
	}
}

func TestStore_Load_EntryWithAbsentThreshold_HasNoLongContextTier(t *testing.T) {
	// An entry that omits context_length_threshold must have HasThreshold=false and
	// HasLongContextTier()==false.
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, wellFormedOneModel) // test-model-a has no threshold

	store := pricing.NewStore(root)
	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	entry, found := table.Lookup("test-model-a")
	if !found {
		t.Fatal("test-model-a must be in the table")
	}
	if entry.HasThreshold {
		t.Error("entry without context_length_threshold must have HasThreshold=false")
	}
	if entry.HasLongContextTier() {
		t.Error("entry without context_length_threshold must HasLongContextTier() == false")
	}
}

func TestStore_Load_EntryWithNullThreshold_HasNoLongContextTier(t *testing.T) {
	// context_length_threshold: null is equivalent to omitting the field:
	// the model has no long-context tier.
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, nullThresholdYAML)

	store := pricing.NewStore(root)
	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	entry, found := table.Lookup("test-model-a")
	if !found {
		t.Fatal("test-model-a must be in the table")
	}
	if entry.HasThreshold {
		t.Error("null context_length_threshold must be treated as absent: HasThreshold must be false")
	}
	if entry.HasLongContextTier() {
		t.Error("null context_length_threshold must yield HasLongContextTier() == false")
	}
}

func TestStore_Load_EntryMissingRequiredRate_ProducesNamedFinding(t *testing.T) {
	// An entry that fails Validate() must produce a FindingInvalidPricingEntry.
	// The finding's Detail must name the model so the user can identify which entry
	// to fix.
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, oneInvalidOneValidYAML)

	store := pricing.NewStore(root)
	_, findings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load must not return an error for per-entry validation failures: %v", err)
	}

	var invalidFindings []domain.Finding
	for _, f := range findings {
		if f.Kind == domain.FindingInvalidPricingEntry {
			invalidFindings = append(invalidFindings, f)
		}
	}
	if len(invalidFindings) == 0 {
		t.Fatal("Load must produce a FindingInvalidPricingEntry for an entry missing a required rate")
	}

	// The finding must name the model so the user can locate the problem.
	for _, f := range invalidFindings {
		if !strings.Contains(f.Detail, "invalid-model") {
			t.Errorf("FindingInvalidPricingEntry Detail must name the model; got: %q", f.Detail)
		}
	}
}

func TestStore_Load_OneInvalidEntry_DoesNotPreventOtherEntriesLoading(t *testing.T) {
	// One invalid entry must be skipped and reported as a Finding while the
	// remaining valid entries still load into the table.
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, oneInvalidOneValidYAML)

	store := pricing.NewStore(root)
	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load must not return an error for per-entry validation failures: %v", err)
	}

	// The valid entry must be present.
	if _, found := table.Lookup("valid-model"); !found {
		t.Error("a valid entry must load even when another entry in the file is invalid")
	}
	// The invalid entry must NOT be in the table.
	if _, found := table.Lookup("invalid-model"); found {
		t.Error("an invalid entry must not appear in the loaded table")
	}
}

func TestStore_Load_EntryWithThresholdButMissingAtRate_ProducesNamedFinding(t *testing.T) {
	// An entry that sets context_length_threshold (HasThreshold=true) but omits
	// output_at_threshold fails Validate() on the branch `HasThreshold && OutputAtThreshold == 0`.
	// This is a distinct validation path from "missing an always-required rate".
	// The loader must produce a FindingInvalidPricingEntry that names the model.
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, thresholdPresentMissingAtRateYAML)

	store := pricing.NewStore(root)
	_, findings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load must not return an error for a per-entry validation failure: %v", err)
	}

	var invalidFindings []domain.Finding
	for _, f := range findings {
		if f.Kind == domain.FindingInvalidPricingEntry {
			invalidFindings = append(invalidFindings, f)
		}
	}
	if len(invalidFindings) == 0 {
		t.Fatal("Load must produce a FindingInvalidPricingEntry for an entry with a threshold but no output_at_threshold")
	}
	for _, f := range invalidFindings {
		if !strings.Contains(f.Detail, "threshold-no-at-rate") {
			t.Errorf("FindingInvalidPricingEntry Detail must name the model; got: %q", f.Detail)
		}
	}
}

func TestStore_Load_EntryWithThresholdButMissingAtRate_DoesNotPreventOtherEntriesLoading(t *testing.T) {
	// The threshold-present-but-missing-at-rate entry must be skipped in isolation:
	// the sibling valid entry must still load.
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, thresholdPresentMissingAtRateYAML)

	store := pricing.NewStore(root)
	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load must not return an error for a per-entry validation failure: %v", err)
	}

	if _, found := table.Lookup("valid-model"); !found {
		t.Error("the valid sibling entry must load even when another entry has a threshold but no output_at_threshold")
	}
	if _, found := table.Lookup("threshold-no-at-rate"); found {
		t.Error("the invalid entry (threshold present, output_at_threshold absent) must not appear in the loaded table")
	}
}

func TestStore_Load_EntryWithNonNumericRate_IsSkippedWithNamedFinding(t *testing.T) {
	// An entry whose rate field contains a syntactically valid YAML string that
	// ParseRate cannot parse (e.g., "abc") is a per-entry error, distinct from a
	// malformed YAML document and distinct from a missing/zero rate. The entry must
	// be skipped with a FindingInvalidPricingEntry naming the model, while the sibling
	// valid entry still loads.
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, nonNumericRateYAML)

	store := pricing.NewStore(root)
	table, findings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load must not return a document-level error for a per-entry parse failure: %v", err)
	}

	var invalidFindings []domain.Finding
	for _, f := range findings {
		if f.Kind == domain.FindingInvalidPricingEntry {
			invalidFindings = append(invalidFindings, f)
		}
	}
	if len(invalidFindings) == 0 {
		t.Fatal("Load must produce a FindingInvalidPricingEntry for an entry with an unparseable rate string")
	}
	for _, f := range invalidFindings {
		if !strings.Contains(f.Detail, "bad-rate-model") {
			t.Errorf("FindingInvalidPricingEntry Detail must name the model; got: %q", f.Detail)
		}
	}

	// The sibling valid entry must still be in the table.
	if _, found := table.Lookup("valid-model"); !found {
		t.Error("the valid sibling entry must load even when another entry has an unparseable rate string")
	}
	if _, found := table.Lookup("bad-rate-model"); found {
		t.Error("the entry with a non-numeric rate must not appear in the loaded table")
	}
}

func TestStore_Load_EntryWithNegativeRate_IsSkippedWithNamedFinding(t *testing.T) {
	// A rate string that is a syntactically valid negative decimal (e.g., "-3.00") is
	// rejected by ParseRate. The entry must be skipped with a FindingInvalidPricingEntry
	// naming the model, while the sibling valid entry still loads.
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, negativeRateYAML)

	store := pricing.NewStore(root)
	table, findings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load must not return a document-level error for a per-entry parse failure: %v", err)
	}

	var invalidFindings []domain.Finding
	for _, f := range findings {
		if f.Kind == domain.FindingInvalidPricingEntry {
			invalidFindings = append(invalidFindings, f)
		}
	}
	if len(invalidFindings) == 0 {
		t.Fatal("Load must produce a FindingInvalidPricingEntry for an entry with a negative rate string")
	}
	for _, f := range invalidFindings {
		if !strings.Contains(f.Detail, "negative-rate-model") {
			t.Errorf("FindingInvalidPricingEntry Detail must name the model; got: %q", f.Detail)
		}
	}

	// The sibling valid entry must still be in the table.
	if _, found := table.Lookup("valid-model"); !found {
		t.Error("the valid sibling entry must load even when another entry has a negative rate string")
	}
	if _, found := table.Lookup("negative-rate-model"); found {
		t.Error("the entry with a negative rate must not appear in the loaded table")
	}
}

// ---------------------------------------------------------------------------
// T7.4 — Persistence
// ---------------------------------------------------------------------------

func TestStore_Put_NewEntry_RoundTripsViaWriteThenLoad(t *testing.T) {
	root := t.TempDir()
	store := pricing.NewStore(root)
	entry := newMinimalEntry("round-trip-model")

	if err := store.Put(context.Background(), entry); err != nil {
		t.Fatalf("Put: %v", err)
	}

	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load after Put: %v", err)
	}

	got, found := table.Lookup("round-trip-model")
	if !found {
		t.Fatal("entry must be found after round-trip through Put then Load")
	}
	if got.Model != entry.Model {
		t.Errorf("round-trip Model: got %q, want %q", got.Model, entry.Model)
	}
}

func TestStore_Put_RatesPrecisionPreservedInRoundTrip(t *testing.T) {
	// Money precision contract: rate values must survive YAML serialisation without
	// precision loss. This test pins the exact nanodollar values that ParseRate
	// produces for two-decimal USD strings so that any float-based round-trip
	// error is detected.
	root := t.TempDir()
	store := pricing.NewStore(root)

	inputRate, _ := domain.ParseRate("3.00")
	cachedRate, _ := domain.ParseRate("0.30")
	writeRate, _ := domain.ParseRate("3.75")
	underRate, _ := domain.ParseRate("15.00")
	atRate, _ := domain.ParseRate("22.50")

	entry := domain.ModelPricing{
		Model:                  "precision-model",
		Input:                  inputRate,
		CachedInput:            cachedRate,
		CacheWrite:             writeRate,
		OutputUnderThreshold:   underRate,
		OutputAtThreshold:      atRate,
		ContextLengthThreshold: 200_000,
		HasThreshold:           true,
	}

	if err := store.Put(context.Background(), entry); err != nil {
		t.Fatalf("Put: %v", err)
	}

	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load after Put: %v", err)
	}

	got, found := table.Lookup("precision-model")
	if !found {
		t.Fatal("entry not found after round-trip")
	}

	if got.Input != inputRate {
		t.Errorf("Input: got %d nanos, want %d nanos (loss of precision in round-trip)", int64(got.Input), int64(inputRate))
	}
	if got.CachedInput != cachedRate {
		t.Errorf("CachedInput: got %d nanos, want %d nanos", int64(got.CachedInput), int64(cachedRate))
	}
	if got.CacheWrite != writeRate {
		t.Errorf("CacheWrite: got %d nanos, want %d nanos", int64(got.CacheWrite), int64(writeRate))
	}
	if got.OutputUnderThreshold != underRate {
		t.Errorf("OutputUnderThreshold: got %d nanos, want %d nanos", int64(got.OutputUnderThreshold), int64(underRate))
	}
	if got.OutputAtThreshold != atRate {
		t.Errorf("OutputAtThreshold: got %d nanos, want %d nanos", int64(got.OutputAtThreshold), int64(atRate))
	}
	if got.ContextLengthThreshold != 200_000 {
		t.Errorf("ContextLengthThreshold: got %d, want 200000", got.ContextLengthThreshold)
	}
}

func TestStore_Put_ExistingFileWithEntries_PreservesExistingEntries(t *testing.T) {
	// Writing a new entry must not discard entries already in the file.
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, wellFormedOneModel) // seeds test-model-a

	store := pricing.NewStore(root)

	if err := store.Put(context.Background(), newMinimalEntry("test-model-new")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load after Put into existing file: %v", err)
	}

	if _, found := table.Lookup("test-model-a"); !found {
		t.Error("Put must not discard the pre-existing test-model-a entry")
	}
	if _, found := table.Lookup("test-model-new"); !found {
		t.Error("Put must add the new entry to the file")
	}
}

func TestStore_Put_AbsentFile_CreatesFileAndParentDirectories(t *testing.T) {
	// When neither the pricing file nor its parent directories exist, Put must
	// create them. This is necessary for first-run scenarios where the config
	// tree has never been written.
	root := t.TempDir()
	// Deliberately do NOT create MosaicLogAnalyzer/config/.
	store := pricing.NewStore(root)

	if err := store.Put(context.Background(), newMinimalEntry("create-test-model")); err != nil {
		t.Fatalf("Put must create missing file and parent directories: %v", err)
	}

	// The file must now exist at the path the store advertises.
	if _, err := os.Stat(store.Path()); os.IsNotExist(err) {
		t.Error("Put must create the pricing file when it does not exist")
	}

	// The file must be loadable and contain the written entry.
	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load after file creation: %v", err)
	}
	if _, found := table.Lookup("create-test-model"); !found {
		t.Error("entry written by Put must be loadable after file creation")
	}
}

func TestStore_Put_EntryWithNoThreshold_ReloadsAsNoLongContextTier(t *testing.T) {
	// An entry persisted with HasThreshold=false must reload with HasThreshold=false.
	root := t.TempDir()
	store := pricing.NewStore(root)
	entry := newMinimalEntry("no-threshold-model") // HasThreshold=false

	if err := store.Put(context.Background(), entry); err != nil {
		t.Fatalf("Put: %v", err)
	}

	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load after Put: %v", err)
	}

	got, found := table.Lookup("no-threshold-model")
	if !found {
		t.Fatal("entry must be found after round-trip")
	}
	if got.HasThreshold {
		t.Error("entry written without a threshold must reload with HasThreshold=false")
	}
	if got.HasLongContextTier() {
		t.Error("entry written without a threshold must reload with HasLongContextTier()==false")
	}
}

func TestStore_Put_EntryWithThreshold_ThresholdRoundTrips(t *testing.T) {
	// An entry persisted with a threshold must reload with HasThreshold=true and
	// the exact same ContextLengthThreshold value.
	root := t.TempDir()
	store := pricing.NewStore(root)
	entry := newEntryWithThreshold("threshold-model")

	if err := store.Put(context.Background(), entry); err != nil {
		t.Fatalf("Put: %v", err)
	}

	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load after Put: %v", err)
	}

	got, found := table.Lookup("threshold-model")
	if !found {
		t.Fatal("entry must be found after round-trip")
	}
	if !got.HasThreshold {
		t.Error("entry written with a threshold must reload with HasThreshold=true")
	}
	if !got.HasLongContextTier() {
		t.Error("entry written with a threshold must reload with HasLongContextTier()==true")
	}
	if got.ContextLengthThreshold != entry.ContextLengthThreshold {
		t.Errorf("ContextLengthThreshold: got %d, want %d", got.ContextLengthThreshold, entry.ContextLengthThreshold)
	}
}

func TestStore_Put_UpdatesExistingEntry_NewValueReplacesOld(t *testing.T) {
	// Calling Put for a model that already exists in the file must update the entry,
	// not create a duplicate. The loaded table must reflect the new rates.
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, wellFormedOneModel) // test-model-a with "3.00" input

	store := pricing.NewStore(root)

	// Write a new entry for the same model with a different input rate.
	updatedInputRate, _ := domain.ParseRate("5.00")
	updated := newMinimalEntry("test-model-a")
	updated.Input = updatedInputRate

	if err := store.Put(context.Background(), updated); err != nil {
		t.Fatalf("Put (update): %v", err)
	}

	table, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load after update: %v", err)
	}

	if table.Len() != 1 {
		t.Errorf("table must still have 1 entry after an update; got %d", table.Len())
	}
	got, found := table.Lookup("test-model-a")
	if !found {
		t.Fatal("test-model-a must be found after update")
	}
	if got.Input != updatedInputRate {
		t.Errorf("Input after update: got %d nanos, want %d nanos", int64(got.Input), int64(updatedInputRate))
	}
}

// ---------------------------------------------------------------------------
// Path exposure (AC7.8)
// ---------------------------------------------------------------------------

func TestStore_Path_IsNonEmpty(t *testing.T) {
	store := pricing.NewStore(t.TempDir())
	if store.Path() == "" {
		t.Error("Path() must return a non-empty string so frontends can display it to the user")
	}
}

func TestStore_Path_IsAbsolutePath(t *testing.T) {
	store := pricing.NewStore(t.TempDir())
	if !filepath.IsAbs(store.Path()) {
		t.Errorf("Path() = %q must be an absolute path", store.Path())
	}
}

func TestStore_Path_IsUnderMosaicRoot(t *testing.T) {
	root := t.TempDir()
	store := pricing.NewStore(root)

	rel, err := filepath.Rel(root, store.Path())
	if err != nil {
		t.Fatalf("filepath.Rel: %v", err)
	}
	if filepath.IsAbs(rel) {
		t.Errorf("Path() = %q must be under mosaicRoot %q", store.Path(), root)
	}
}

func TestStore_Path_EndsWithConfigRelPath(t *testing.T) {
	root := t.TempDir()
	store := pricing.NewStore(root)

	// Normalise to forward slashes for comparison — the constant uses forward slashes.
	got := filepath.ToSlash(store.Path())
	if !strings.HasSuffix(got, pricing.ConfigRelPath) {
		t.Errorf("Path() = %q must end with pricing.ConfigRelPath %q", got, pricing.ConfigRelPath)
	}
}

// ---------------------------------------------------------------------------
// Comment preservation (minor — guards the risk called out in Plan.md)
// ---------------------------------------------------------------------------

func TestStore_Put_ExistingFileWithComments_RawFileRetainsComments(t *testing.T) {
	// The Plan calls out "Rewriting the YAML on persist destroys the user's comments
	// and formatting" as a named risk. The binding rule is "Writing preserves the
	// file's comments where practical". This test directly protects against that risk
	// by checking the raw file bytes after a Put call still contain the pre-existing
	// comment marker — verifying comment survival at the byte level, not just through
	// Load (which ignores comments).
	root, pricingPath := makeRootWithConfigDir(t)
	writePricingYAML(t, pricingPath, commentedPricingYAML)

	store := pricing.NewStore(root)
	if err := store.Put(context.Background(), newMinimalEntry("new-model")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	raw, err := os.ReadFile(pricingPath)
	if err != nil {
		t.Fatalf("reading pricing file after Put: %v", err)
	}
	const wantComment = "# user note: these are our production prices"
	if !strings.Contains(string(raw), wantComment) {
		t.Errorf("Put must preserve pre-existing comments in the raw file; comment %q not found in file after Put", wantComment)
	}
}
