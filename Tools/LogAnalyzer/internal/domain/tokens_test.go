package domain_test

// Tests for the optional token value type (T2.1) and four-category token usage
// aggregation (T2.2).
//
// Design invariant: absent ≠ present-zero at every point in the API, including
// after aggregation and rollup. These tests pin that invariant so no implementation
// can silently collapse the distinction.

import (
	"strings"
	"testing"

	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// T2.1 — Optional token value type: absent vs present-zero, absent-preserving addition
// ---------------------------------------------------------------------------

func TestAbsentTokens_IsAbsent(t *testing.T) {
	tc := domain.AbsentTokens()
	if tc.IsPresent() {
		t.Error("AbsentTokens() must not be present")
	}
}

func TestTokens_IsPresent(t *testing.T) {
	tc := domain.Tokens(5)
	if !tc.IsPresent() {
		t.Error("Tokens(5) must be present")
	}
}

func TestTokens_PresentZeroIsPresent(t *testing.T) {
	// Present-zero must be distinguishable from absent; Tokens(0).IsPresent() must be true.
	tc := domain.Tokens(0)
	if !tc.IsPresent() {
		t.Error("Tokens(0) must be present — present-zero is a distinct state from absent")
	}
}

func TestAbsentTokens_And_PresentZero_AreDistinguishable(t *testing.T) {
	absent := domain.AbsentTokens()
	presentZero := domain.Tokens(0)

	if absent.IsPresent() == presentZero.IsPresent() && !absent.IsPresent() {
		// absent is absent and presentZero is absent — that is wrong
		t.Error("absent and present-zero must be distinguishable via IsPresent()")
	}

	if !presentZero.IsPresent() {
		t.Error("present-zero must be present")
	}
	if absent.IsPresent() {
		t.Error("absent must not be present")
	}
}

func TestTokenCount_Value_Absent(t *testing.T) {
	tc := domain.AbsentTokens()
	v, ok := tc.Value()
	if ok {
		t.Errorf("absent.Value() ok = true, want false")
	}
	if v != 0 {
		t.Errorf("absent.Value() v = %d, want 0 (zero sentinel when absent)", v)
	}
}

func TestTokenCount_Value_Present(t *testing.T) {
	tc := domain.Tokens(42)
	v, ok := tc.Value()
	if !ok {
		t.Errorf("Tokens(42).Value() ok = false, want true")
	}
	if v != 42 {
		t.Errorf("Tokens(42).Value() v = %d, want 42", v)
	}
}

func TestTokenCount_Value_PresentZero(t *testing.T) {
	tc := domain.Tokens(0)
	v, ok := tc.Value()
	if !ok {
		t.Error("Tokens(0).Value() ok = false, want true (present-zero must survive)")
	}
	if v != 0 {
		t.Errorf("Tokens(0).Value() v = %d, want 0", v)
	}
}

func TestTokenCount_ValueOr_Absent(t *testing.T) {
	tc := domain.AbsentTokens()
	if got := tc.ValueOr(-1); got != -1 {
		t.Errorf("absent.ValueOr(-1) = %d, want -1", got)
	}
}

func TestTokenCount_ValueOr_Present(t *testing.T) {
	tc := domain.Tokens(7)
	if got := tc.ValueOr(-1); got != 7 {
		t.Errorf("Tokens(7).ValueOr(-1) = %d, want 7", got)
	}
}

func TestTokenCount_ValueOr_PresentZero(t *testing.T) {
	tc := domain.Tokens(0)
	if got := tc.ValueOr(-1); got != 0 {
		t.Errorf("Tokens(0).ValueOr(-1) = %d, want 0 (present-zero overrides default)", got)
	}
}

// --- Absent-preserving addition rules ---

func TestTokenCount_Add_AbsentPlusAbsent_IsAbsent(t *testing.T) {
	result := domain.AbsentTokens().Add(domain.AbsentTokens())
	if result.IsPresent() {
		t.Error("absent + absent must be absent")
	}
}

func TestTokenCount_Add_AbsentPlusPresent_IsPresent(t *testing.T) {
	result := domain.AbsentTokens().Add(domain.Tokens(5))
	v, ok := result.Value()
	if !ok {
		t.Error("absent + present(5) must be present")
	}
	if v != 5 {
		t.Errorf("absent + present(5) = %d, want 5", v)
	}
}

func TestTokenCount_Add_PresentPlusAbsent_IsPresent(t *testing.T) {
	// Absent contributes nothing but must not poison the sum.
	result := domain.Tokens(5).Add(domain.AbsentTokens())
	v, ok := result.Value()
	if !ok {
		t.Error("present(5) + absent must be present")
	}
	if v != 5 {
		t.Errorf("present(5) + absent = %d, want 5", v)
	}
}

func TestTokenCount_Add_PresentPlusPresent(t *testing.T) {
	result := domain.Tokens(3).Add(domain.Tokens(4))
	v, ok := result.Value()
	if !ok {
		t.Error("present(3) + present(4) must be present")
	}
	if v != 7 {
		t.Errorf("present(3) + present(4) = %d, want 7", v)
	}
}

func TestTokenCount_Add_PresentZeroPlusPresentZero(t *testing.T) {
	result := domain.Tokens(0).Add(domain.Tokens(0))
	v, ok := result.Value()
	if !ok {
		t.Error("present(0) + present(0) must be present")
	}
	if v != 0 {
		t.Errorf("present(0) + present(0) = %d, want 0", v)
	}
}

func TestTokenCount_Add_AbsentPlusPresentZero_IsPresentZero(t *testing.T) {
	result := domain.AbsentTokens().Add(domain.Tokens(0))
	v, ok := result.Value()
	if !ok {
		t.Error("absent + present(0) must be present (present-zero must survive)")
	}
	if v != 0 {
		t.Errorf("absent + present(0) = %d, want 0", v)
	}
}

// --- SumTokens ---

func TestSumTokens_Empty_IsAbsent(t *testing.T) {
	result := domain.SumTokens()
	if result.IsPresent() {
		t.Error("SumTokens() of empty input must be absent")
	}
}

func TestSumTokens_AllAbsent_IsAbsent(t *testing.T) {
	result := domain.SumTokens(domain.AbsentTokens(), domain.AbsentTokens(), domain.AbsentTokens())
	if result.IsPresent() {
		t.Error("SumTokens of all-absent counts must be absent")
	}
}

func TestSumTokens_WithSomePresent(t *testing.T) {
	result := domain.SumTokens(domain.AbsentTokens(), domain.Tokens(3), domain.AbsentTokens(), domain.Tokens(4))
	v, ok := result.Value()
	if !ok {
		t.Error("SumTokens(absent, 3, absent, 4) must be present")
	}
	if v != 7 {
		t.Errorf("SumTokens(absent, 3, absent, 4) = %d, want 7", v)
	}
}

func TestSumTokens_SingleAbsent(t *testing.T) {
	result := domain.SumTokens(domain.AbsentTokens())
	if result.IsPresent() {
		t.Error("SumTokens of a single absent count must be absent")
	}
}

func TestSumTokens_SinglePresent(t *testing.T) {
	result := domain.SumTokens(domain.Tokens(9))
	v, ok := result.Value()
	if !ok {
		t.Error("SumTokens of a single present count must be present")
	}
	if v != 9 {
		t.Errorf("SumTokens(9) = %d, want 9", v)
	}
}

func TestSumTokens_PreservesDistinctionThroughRollup(t *testing.T) {
	// A realistic category rollup: absent entries must not poison the total.
	// Odd indices are present: 1, 3, 5, 7, 9 = 25.
	counts := make([]domain.TokenCount, 10)
	for i := range counts {
		if i%2 == 0 {
			counts[i] = domain.AbsentTokens()
		} else {
			counts[i] = domain.Tokens(int64(i))
		}
	}
	result := domain.SumTokens(counts...)
	v, ok := result.Value()
	if !ok {
		t.Error("SumTokens rollup with absent entries must be present")
	}
	if v != 25 {
		t.Errorf("SumTokens rollup = %d, want 25 (1+3+5+7+9)", v)
	}
}

func TestSumTokens_OnlyAbsentInLargeSlice(t *testing.T) {
	counts := make([]domain.TokenCount, 100)
	for i := range counts {
		counts[i] = domain.AbsentTokens()
	}
	result := domain.SumTokens(counts...)
	if result.IsPresent() {
		t.Error("SumTokens of 100 absent counts must be absent")
	}
}

// ---------------------------------------------------------------------------
// T2.2 — Four-category token usage aggregation and total-context computation
// ---------------------------------------------------------------------------

func TestTokenUsage_IsEmpty_AllAbsent(t *testing.T) {
	u := domain.TokenUsage{}
	if !u.IsEmpty() {
		t.Error("TokenUsage with all-absent categories must be empty")
	}
}

func TestTokenUsage_IsEmpty_WithPresent(t *testing.T) {
	u := domain.TokenUsage{Input: domain.Tokens(1)}
	if u.IsEmpty() {
		t.Error("TokenUsage with a present category must not be empty")
	}
}

func TestTokenUsage_IsEmpty_OnlyOutputPresent(t *testing.T) {
	u := domain.TokenUsage{Output: domain.Tokens(5)}
	if u.IsEmpty() {
		t.Error("TokenUsage with present Output must not be empty")
	}
}

func TestTokenUsage_Add_PerCategoryAbsentPreserving(t *testing.T) {
	a := domain.TokenUsage{
		Input:     domain.Tokens(10),
		CacheRead: domain.Tokens(5),
	}
	b := domain.TokenUsage{
		Input:  domain.Tokens(3),
		Output: domain.Tokens(7),
	}
	result := a.Add(b)

	// Input: 10 + 3 = 13
	if v, ok := result.Input.Value(); !ok || v != 13 {
		t.Errorf("Add Input = (%d, %v), want (13, true)", v, ok)
	}
	// CacheRead: present(5) + absent = present(5)
	if v, ok := result.CacheRead.Value(); !ok || v != 5 {
		t.Errorf("Add CacheRead = (%d, %v), want (5, true)", v, ok)
	}
	// Output: absent + present(7) = present(7)
	if v, ok := result.Output.Value(); !ok || v != 7 {
		t.Errorf("Add Output = (%d, %v), want (7, true)", v, ok)
	}
	// CacheCreation: absent + absent = absent
	if result.CacheCreation.IsPresent() {
		t.Error("Add CacheCreation must remain absent when neither side had it")
	}
}

func TestTokenUsage_Add_BothSidesAllCategories(t *testing.T) {
	a := domain.TokenUsage{
		Input:         domain.Tokens(100),
		CacheRead:     domain.Tokens(50),
		CacheCreation: domain.Tokens(20),
		Output:        domain.Tokens(200),
	}
	b := domain.TokenUsage{
		Input:         domain.Tokens(10),
		CacheRead:     domain.Tokens(5),
		CacheCreation: domain.Tokens(2),
		Output:        domain.Tokens(20),
	}
	result := a.Add(b)

	checks := []struct {
		name string
		got  domain.TokenCount
		want int64
	}{
		{"Input", result.Input, 110},
		{"CacheRead", result.CacheRead, 55},
		{"CacheCreation", result.CacheCreation, 22},
		{"Output", result.Output, 220},
	}
	for _, c := range checks {
		v, ok := c.got.Value()
		if !ok || v != c.want {
			t.Errorf("Add %s = (%d, %v), want (%d, true)", c.name, v, ok, c.want)
		}
	}
}

// --- TotalContext ---

func TestTokenUsage_TotalContext_AllAbsent_IsAbsent(t *testing.T) {
	u := domain.TokenUsage{}
	if u.TotalContext().IsPresent() {
		t.Error("TotalContext of all-absent usage must be absent")
	}
}

func TestTokenUsage_TotalContext_OnlyOutputPresent_IsAbsent(t *testing.T) {
	// Output does NOT contribute to TotalContext; only input+cacheRead+cacheCreation do.
	u := domain.TokenUsage{Output: domain.Tokens(1000)}
	if u.TotalContext().IsPresent() {
		t.Error("TotalContext must be absent when only Output is present (Output is not a context category)")
	}
}

func TestTokenUsage_TotalContext_InputOnly(t *testing.T) {
	u := domain.TokenUsage{Input: domain.Tokens(100)}
	v, ok := u.TotalContext().Value()
	if !ok {
		t.Error("TotalContext with only Input present must be present")
	}
	if v != 100 {
		t.Errorf("TotalContext with Input=100 = %d, want 100", v)
	}
}

func TestTokenUsage_TotalContext_AllThreeContextCategories(t *testing.T) {
	u := domain.TokenUsage{
		Input:         domain.Tokens(100),
		CacheRead:     domain.Tokens(50),
		CacheCreation: domain.Tokens(25),
		Output:        domain.Tokens(999), // must not contribute
	}
	v, ok := u.TotalContext().Value()
	if !ok {
		t.Error("TotalContext with all three context categories present must be present")
	}
	if v != 175 {
		t.Errorf("TotalContext(input=100, cacheRead=50, cacheCreation=25) = %d, want 175", v)
	}
}

func TestTokenUsage_TotalContext_AbsentCategoriesExcludedNotTreatedAsZero(t *testing.T) {
	// Only Input present; cacheRead and cacheCreation are absent, not zero.
	u := domain.TokenUsage{
		Input: domain.Tokens(100),
		// CacheRead and CacheCreation absent
	}
	v, ok := u.TotalContext().Value()
	if !ok {
		t.Error("TotalContext with only Input present must be present (not absent)")
	}
	if v != 100 {
		t.Errorf("TotalContext = %d, want 100 (absent categories excluded)", v)
	}
}

func TestTokenUsage_TotalContext_CacheReadAndCreationOnly(t *testing.T) {
	u := domain.TokenUsage{
		CacheRead:     domain.Tokens(30),
		CacheCreation: domain.Tokens(10),
	}
	v, ok := u.TotalContext().Value()
	if !ok {
		t.Error("TotalContext must be present when cacheRead and cacheCreation are present")
	}
	if v != 40 {
		t.Errorf("TotalContext(cacheRead=30, cacheCreation=10) = %d, want 40", v)
	}
}

// --- Get ---

func TestTokenUsage_Get_KnownCategories(t *testing.T) {
	u := domain.TokenUsage{
		Input:         domain.Tokens(10),
		CacheRead:     domain.Tokens(20),
		CacheCreation: domain.Tokens(30),
		Output:        domain.Tokens(40),
	}

	cases := []struct {
		cat  domain.TokenCategory
		want int64
	}{
		{domain.CategoryInput, 10},
		{domain.CategoryCacheRead, 20},
		{domain.CategoryCacheCreation, 30},
		{domain.CategoryOutput, 40},
	}
	for _, c := range cases {
		got, ok := u.Get(c.cat)
		if !ok {
			t.Errorf("Get(%q) ok = false, want true", c.cat)
			continue
		}
		v, present := got.Value()
		if !present || v != c.want {
			t.Errorf("Get(%q) = (%d, %v), want (%d, true)", c.cat, v, present, c.want)
		}
	}
}

func TestTokenUsage_Get_UnrecognisedCategory(t *testing.T) {
	u := domain.TokenUsage{Input: domain.Tokens(1)}
	_, ok := u.Get("nonexistent_category")
	if ok {
		t.Error("Get with unrecognised category must return ok=false")
	}
}

func TestTokenUsage_Get_AbsentCategory(t *testing.T) {
	u := domain.TokenUsage{Input: domain.Tokens(5)}
	got, ok := u.Get(domain.CategoryOutput)
	if !ok {
		t.Error("Get for a recognised absent category must return ok=true (the category exists, the value is absent)")
	}
	if got.IsPresent() {
		t.Error("Get for an absent category must return an absent TokenCount")
	}
}

// --- SumUsage ---

func TestSumUsage_Empty_IsAllAbsent(t *testing.T) {
	result := domain.SumUsage()
	if !result.IsEmpty() {
		t.Error("SumUsage of empty input must be all-absent")
	}
}

func TestSumUsage_SingleUsage(t *testing.T) {
	u := domain.TokenUsage{Input: domain.Tokens(5)}
	result := domain.SumUsage(u)
	v, ok := result.Input.Value()
	if !ok || v != 5 {
		t.Errorf("SumUsage of single usage Input = (%d, %v), want (5, true)", v, ok)
	}
	if result.Output.IsPresent() {
		t.Error("SumUsage output must remain absent when never set")
	}
}

func TestSumUsage_AcrossMultipleUsages(t *testing.T) {
	a := domain.TokenUsage{Input: domain.Tokens(10)}
	b := domain.TokenUsage{Input: domain.Tokens(5), Output: domain.Tokens(3)}
	c := domain.TokenUsage{CacheRead: domain.Tokens(7)}

	result := domain.SumUsage(a, b, c)

	if v, ok := result.Input.Value(); !ok || v != 15 {
		t.Errorf("SumUsage Input = (%d, %v), want (15, true)", v, ok)
	}
	if v, ok := result.Output.Value(); !ok || v != 3 {
		t.Errorf("SumUsage Output = (%d, %v), want (3, true)", v, ok)
	}
	if v, ok := result.CacheRead.Value(); !ok || v != 7 {
		t.Errorf("SumUsage CacheRead = (%d, %v), want (7, true)", v, ok)
	}
	if result.CacheCreation.IsPresent() {
		t.Error("SumUsage CacheCreation must be absent when never set")
	}
}

func TestSumUsage_AbsentUsagesDoNotPoisonSum(t *testing.T) {
	empty := domain.TokenUsage{}
	present := domain.TokenUsage{Output: domain.Tokens(42)}

	result := domain.SumUsage(empty, present, empty)

	if v, ok := result.Output.Value(); !ok || v != 42 {
		t.Errorf("SumUsage with absent usages Output = (%d, %v), want (42, true)", v, ok)
	}
	if result.Input.IsPresent() {
		t.Error("SumUsage Input must remain absent when none of the inputs had it")
	}
}

// ---------------------------------------------------------------------------
// Display labels: TokenCategory.Label() and BillableCategories()
// ---------------------------------------------------------------------------

// TestTokenCategory_Label_InputCategory verifies the exact display label for
// CategoryInput is "input".
func TestTokenCategory_Label_InputCategory(t *testing.T) {
	got := domain.CategoryInput.Label()
	if got != "input" {
		t.Errorf("CategoryInput.Label() = %q, want %q", got, "input")
	}
}

// TestTokenCategory_Label_CacheReadCategory verifies the exact display label for
// CategoryCacheRead is "cache read".
func TestTokenCategory_Label_CacheReadCategory(t *testing.T) {
	got := domain.CategoryCacheRead.Label()
	if got != "cache read" {
		t.Errorf("CategoryCacheRead.Label() = %q, want %q", got, "cache read")
	}
}

// TestTokenCategory_Label_CacheCreationCategory verifies the display label for
// CategoryCacheCreation is "cache write". The machine identifier stays
// cache_creation; only the human-facing label changes to match pricing vocabulary.
func TestTokenCategory_Label_CacheCreationCategory(t *testing.T) {
	got := domain.CategoryCacheCreation.Label()
	if got != "cache write" {
		t.Errorf("CategoryCacheCreation.Label() = %q, want %q", got, "cache write")
	}
}

// TestTokenCategory_Label_OutputCategory verifies the exact display label for
// CategoryOutput is "output".
func TestTokenCategory_Label_OutputCategory(t *testing.T) {
	got := domain.CategoryOutput.Label()
	if got != "output" {
		t.Errorf("CategoryOutput.Label() = %q, want %q", got, "output")
	}
}

// TestTokenCategory_Label_UnrecognisedCategory verifies that an unrecognised
// category returns an empty string rather than panicking or returning a label.
func TestTokenCategory_Label_UnrecognisedCategory(t *testing.T) {
	got := domain.TokenCategory("nonexistent_category").Label()
	if got != "" {
		t.Errorf("unrecognised category Label() = %q, want empty string", got)
	}
}

// TestTokenCategory_Label_AllBillableLabelsNonEmpty verifies every billable
// category has a non-empty display label.
func TestTokenCategory_Label_AllBillableLabelsNonEmpty(t *testing.T) {
	billable := []domain.TokenCategory{
		domain.CategoryInput,
		domain.CategoryCacheRead,
		domain.CategoryCacheCreation,
		domain.CategoryOutput,
	}
	for _, cat := range billable {
		if label := cat.Label(); label == "" {
			t.Errorf("%q.Label() returned empty string — every billable category must have a label", cat)
		}
	}
}

// TestTokenCategory_Label_AllBillableLabelsAreDistinct verifies that no two
// billable categories share a display label.
func TestTokenCategory_Label_AllBillableLabelsAreDistinct(t *testing.T) {
	billable := []domain.TokenCategory{
		domain.CategoryInput,
		domain.CategoryCacheRead,
		domain.CategoryCacheCreation,
		domain.CategoryOutput,
	}
	seen := make(map[string]domain.TokenCategory)
	for _, cat := range billable {
		label := cat.Label()
		if prev, exists := seen[label]; exists {
			t.Errorf("duplicate label %q: both %q and %q share it", label, prev, cat)
		}
		seen[label] = cat
	}
}

// TestTokenCategory_Label_ContainsNoUnexplainedAbbreviations verifies that each
// word in a label is at least 3 characters long. Short tokens like "cr", "cw",
// "in", "out" are the abbreviations being replaced with full English words.
func TestTokenCategory_Label_ContainsNoUnexplainedAbbreviations(t *testing.T) {
	billable := []domain.TokenCategory{
		domain.CategoryInput,
		domain.CategoryCacheRead,
		domain.CategoryCacheCreation,
		domain.CategoryOutput,
	}
	for _, cat := range billable {
		label := cat.Label()
		words := strings.Fields(label)
		for _, word := range words {
			if len(word) < 3 {
				t.Errorf("%q.Label() = %q contains short word %q — labels must use full English words, not abbreviations",
					cat, label, word)
			}
		}
	}
}

// TestBillableCategories_ReturnsFourCategories verifies BillableCategories
// returns exactly four entries — one per billable token category.
func TestBillableCategories_ReturnsFourCategories(t *testing.T) {
	cats := domain.BillableCategories()
	if len(cats) != 4 {
		t.Errorf("BillableCategories() returned %d categories, want 4", len(cats))
	}
}

// TestBillableCategories_CanonicalOrder verifies the canonical presentation
// order: input, cache read (cache_read), cache write (cache_creation), output.
func TestBillableCategories_CanonicalOrder(t *testing.T) {
	cats := domain.BillableCategories()
	if len(cats) < 4 {
		t.Fatalf("BillableCategories() returned %d categories, want 4", len(cats))
	}
	want := []domain.TokenCategory{
		domain.CategoryInput,
		domain.CategoryCacheRead,
		domain.CategoryCacheCreation,
		domain.CategoryOutput,
	}
	for i, w := range want {
		if cats[i] != w {
			t.Errorf("BillableCategories()[%d] = %q, want %q", i, cats[i], w)
		}
	}
}

// TestBillableCategories_AllHaveNonEmptyLabels verifies that every category
// returned by BillableCategories has a non-empty Label().
func TestBillableCategories_AllHaveNonEmptyLabels(t *testing.T) {
	for _, cat := range domain.BillableCategories() {
		if label := cat.Label(); label == "" {
			t.Errorf("BillableCategories includes %q which has an empty label", cat)
		}
	}
}
