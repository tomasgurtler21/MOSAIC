// Package intercept is a conforming fixture standing in for a pure core: it
// imports only domain, takes time as a parameter, and touches no I/O.
package intercept

import (
	"time"

	"mosaic-agent-test/internal/domain"
)

// Decide is a stand-in signature; time is a parameter, never read ambiently.
func Decide(now time.Time) domain.Verdict {
	_ = now
	return domain.VerdictPass
}
