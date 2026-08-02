package domain

// TokenCategory names one of the four billable categories.
type TokenCategory string

const (
	CategoryInput         TokenCategory = "input"
	CategoryCacheRead     TokenCategory = "cache_read"
	CategoryCacheCreation TokenCategory = "cache_creation"
	CategoryOutput        TokenCategory = "output"
)

// TokenCount is an optional token quantity. The zero value is absent.
// Absent is a distinct state from present-zero and must survive all arithmetic.
type TokenCount struct {
	value   int64
	present bool
}

// AbsentTokens constructs the absent value.
func AbsentTokens() TokenCount { return TokenCount{} }

// Tokens constructs a present value (including present zero).
func Tokens(v int64) TokenCount { return TokenCount{value: v, present: true} }

// IsPresent reports whether the count is present (as opposed to absent).
func (t TokenCount) IsPresent() bool { return t.present }

// Value returns the token count and whether it is present.
// value is 0 when absent.
func (t TokenCount) Value() (int64, bool) { return t.value, t.present }

// ValueOr returns the count if present, or def if absent.
func (t TokenCount) ValueOr(def int64) int64 {
	if t.present {
		return t.value
	}
	return def
}

// Add returns absent+absent = absent; present+absent = present (absent contributes
// nothing but never poisons the sum); present+present = present sum.
func (t TokenCount) Add(other TokenCount) TokenCount {
	switch {
	case !t.present && !other.present:
		return AbsentTokens()
	case t.present && !other.present:
		return t
	case !t.present && other.present:
		return other
	default:
		return Tokens(t.value + other.value)
	}
}

// SumTokens folds Add over the slice. Empty slice yields absent.
func SumTokens(counts ...TokenCount) TokenCount {
	result := AbsentTokens()
	for _, c := range counts {
		result = result.Add(c)
	}
	return result
}

// TokenUsage is the four-category usage block. Each category is independently optional.
type TokenUsage struct {
	Input         TokenCount
	CacheRead     TokenCount
	CacheCreation TokenCount
	Output        TokenCount
}

// IsEmpty reports true when all four categories are absent.
func (u TokenUsage) IsEmpty() bool {
	return !u.Input.present && !u.CacheRead.present && !u.CacheCreation.present && !u.Output.present
}

// Add performs a per-category Add.
func (u TokenUsage) Add(other TokenUsage) TokenUsage {
	return TokenUsage{
		Input:         u.Input.Add(other.Input),
		CacheRead:     u.CacheRead.Add(other.CacheRead),
		CacheCreation: u.CacheCreation.Add(other.CacheCreation),
		Output:        u.Output.Add(other.Output),
	}
}

// SumUsage folds Add over the slice. Empty slice yields an all-absent TokenUsage.
func SumUsage(usages ...TokenUsage) TokenUsage {
	result := TokenUsage{}
	for _, u := range usages {
		result = result.Add(u)
	}
	return result
}

// TotalContext returns input + cache read + cache creation, used for output tier
// selection. Absent categories are excluded, not treated as zero. Absent when all
// three contributing categories are absent.
func (u TokenUsage) TotalContext() TokenCount {
	return SumTokens(u.Input, u.CacheRead, u.CacheCreation)
}

// Get returns the count for a category; ok is false for an unrecognised category.
func (u TokenUsage) Get(c TokenCategory) (TokenCount, bool) {
	switch c {
	case CategoryInput:
		return u.Input, true
	case CategoryCacheRead:
		return u.CacheRead, true
	case CategoryCacheCreation:
		return u.CacheCreation, true
	case CategoryOutput:
		return u.Output, true
	default:
		return AbsentTokens(), false
	}
}
