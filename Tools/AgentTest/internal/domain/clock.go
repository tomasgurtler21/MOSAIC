package domain

import "time"

// Clock is injected so pure cores stay pure and time-dependent protocols
// (notably the runstate lock's acquisition timeout and staleness threshold)
// are testable without sleeping.
type Clock interface {
	Now() time.Time
}
