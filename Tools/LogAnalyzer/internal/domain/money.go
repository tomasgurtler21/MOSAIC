package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Currency is the only supported currency; the tool does no conversion.
const Currency = "USD"

// nanosPerDollar is the conversion factor from nanodollars to USD.
const nanosPerDollar = int64(1_000_000_000)

// Money is an exact amount in nanodollars (1e-9 USD). Nanodollars is an INTERNAL
// unit chosen so that per-token arithmetic is exact (see CD-5a); it is never the
// unit a user reads or writes.
type Money int64

// Nanos returns the amount in nanodollars.
func (m Money) Nanos() int64 { return int64(m) }

// String renders the exact value as a decimal USD string with trailing zeros
// trimmed (e.g. "3", "1.23", "0.00000001"). Used for JSON, where exactness matters.
func (m Money) String() string {
	n := int64(m)
	whole := n / nanosPerDollar
	frac := n % nanosPerDollar
	if frac == 0 {
		return strconv.FormatInt(whole, 10)
	}
	// Format fractional part as 9-digit string with leading zeros, then trim trailing zeros.
	fracStr := fmt.Sprintf("%09d", frac)
	fracStr = strings.TrimRight(fracStr, "0")
	return strconv.FormatInt(whole, 10) + "." + fracStr
}

// StringFixed renders the value rounded half-up to n decimal places (e.g.
// StringFixed(2) -> "1.23"). Frontends display money with n = 2.
func (m Money) StringFixed(n int) string {
	if n <= 0 {
		return strconv.FormatInt(int64(m)/nanosPerDollar, 10)
	}
	if n >= 9 {
		// All 9 nano digits, pad if needed.
		whole := int64(m) / nanosPerDollar
		frac := int64(m) % nanosPerDollar
		return fmt.Sprintf("%d.%09d", whole, frac)
	}
	// roundDivisor is 10^(9-n): the number of nanos in one unit of the nth decimal place.
	roundDivisor := int64(1)
	for i := 0; i < 9-n; i++ {
		roundDivisor *= 10
	}
	// Round half-up: add half the divisor before dividing.
	rounded := (int64(m) + roundDivisor/2) / roundDivisor
	// Split into whole and fractional (in units of the nth decimal place).
	whole := rounded / pow10(int64(n))
	frac := rounded % pow10(int64(n))
	return fmt.Sprintf("%d.%0*d", whole, n, frac)
}

func pow10(n int64) int64 {
	result := int64(1)
	for i := int64(0); i < n; i++ {
		result *= 10
	}
	return result
}

// Rate is a price in nanodollars per 1,000,000 tokens.
type Rate int64

// ParseRate converts a decimal USD-per-million-tokens string — as written in the
// pricing YAML, conventionally with two decimals, e.g. "3.00" or "0.30" — to a
// Rate. Values with more than 9 decimal places are rejected rather than silently
// truncated; in practice published rates use two. Returns an error for a
// non-numeric or negative value.
func ParseRate(s string) (Rate, error) {
	if s == "" {
		return 0, errors.New("rate string is empty")
	}

	parts := strings.SplitN(s, ".", 2)
	if len(parts) > 2 {
		return 0, fmt.Errorf("invalid rate %q: multiple decimal points", s)
	}

	intPart, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid rate %q: %w", s, err)
	}
	if intPart < 0 {
		return 0, fmt.Errorf("invalid rate %q: negative value", s)
	}

	nanos := intPart * nanosPerDollar

	if len(parts) == 2 {
		fracStr := parts[1]
		if len(fracStr) > 9 {
			return 0, fmt.Errorf("invalid rate %q: more than 9 decimal places", s)
		}
		if len(fracStr) == 0 {
			return 0, fmt.Errorf("invalid rate %q: trailing decimal point", s)
		}
		// Pad to 9 digits on the right.
		padded := fracStr + strings.Repeat("0", 9-len(fracStr))
		fracNanos, err := strconv.ParseInt(padded, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid rate %q: %w", s, err)
		}
		nanos += fracNanos
	}

	return Rate(nanos), nil
}

// String renders the rate back as the decimal USD-per-million-tokens figure the
// user wrote, so a write-then-read round trip is textually stable.
func (r Rate) String() string {
	// Rate is stored as nanodollars per 1M tokens, same format as Money nanos.
	return Money(r).String()
}

// Apply prices a token count at this rate. Absent tokens yield a NoData MoneyValue.
// Present tokens yield a Known MoneyValue using round-half-up integer arithmetic.
func (r Rate) Apply(t TokenCount) MoneyValue {
	v, ok := t.Value()
	if !ok {
		return NoMoneyData()
	}
	// tokens * rate / 1_000_000, round half-up.
	const tokenDivisor = int64(1_000_000)
	raw := v * int64(r)
	// Round half-up: add tokenDivisor/2 before dividing.
	amount := (raw + tokenDivisor/2) / tokenDivisor
	return KnownMoney(Money(amount))
}

// MoneyState distinguishes the three reasons a money cell can lack a number.
type MoneyState uint8

const (
	MoneyNoData   MoneyState = iota // no token data for this category
	MoneyUnpriced                   // tokens known, model has no pricing entry
	MoneyKnown                      // a real amount, possibly exactly zero
)

// MoneyValue is a money cell. "$0" (Known, 0) is distinct from "no data" and from
// "unpriced" at every layer, including the rendered output.
type MoneyValue struct {
	State  MoneyState
	Amount Money // meaningful only when State == MoneyKnown
}

// NoMoneyData returns a MoneyValue representing no token data.
func NoMoneyData() MoneyValue { return MoneyValue{State: MoneyNoData} }

// UnpricedMoney returns a MoneyValue representing known tokens with no pricing.
func UnpricedMoney() MoneyValue { return MoneyValue{State: MoneyUnpriced} }

// KnownMoney returns a MoneyValue with a known exact amount.
func KnownMoney(m Money) MoneyValue { return MoneyValue{State: MoneyKnown, Amount: m} }

// SumMoney folds a set of cells into one.
//   - all NoData (or empty)           -> (NoData, complete=true)
//   - no Known, at least one Unpriced -> (Unpriced, complete=false)
//   - at least one Known              -> (Known(sum of Known), complete = no Unpriced present)
//
// complete=false is the sole mechanism for "partial money total".
func SumMoney(values ...MoneyValue) (total MoneyValue, complete bool) {
	hasKnown := false
	hasUnpriced := false
	var sum Money

	for _, v := range values {
		switch v.State {
		case MoneyKnown:
			hasKnown = true
			sum += v.Amount
		case MoneyUnpriced:
			hasUnpriced = true
		}
	}

	switch {
	case hasKnown:
		return KnownMoney(sum), !hasUnpriced
	case hasUnpriced:
		return UnpricedMoney(), false
	default:
		return NoMoneyData(), true
	}
}
