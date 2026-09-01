package suite

import (
	"fmt"
	"sync"

	"mosaic-agent-test/internal/domain"
)

// MaxRunsPerSuite is how many distinct run identities one suite run can carry,
// fixed at 65536 by the external format contract's four hex digits of suffix.
//
// A plan whose (test × repetition × attempt) matrix exceeds it must be refused
// before execution. Refusing is the only honest option: the alternative is a
// duplicate identity, which means two runs writing into one log folder and a
// cost figure attributed to the wrong run — a failure that would surface as
// puzzling data rather than as an error.
const MaxRunsPerSuite = 1 << 16

// runTuple is the key used to look up an ordinal for a (testID, runNumber,
// attempt) tuple.
type runTuple struct {
	testID    string
	runNumber int
	attempt   int
}

// RunIDGenerator authors collision-free run identities inside the external
// run-id format contract.
//
// The suffix is a per-suite-run ordinal rendered as four lowercase hex digits,
// not a truncated hash of the attempt's tuple. Derivation by ordinal gives
// distinct suffixes to distinct attempts by construction; a truncated hash
// gives them only with high probability, and reintroduces a 16-bit birthday
// collision across the whole (test × repetition × attempt) matrix within one
// timestamp second. Concurrency makes that matrix larger and puts far more of
// it inside one second, so the probabilistic version gets worse exactly where
// it matters.
//
// Ordinals are allocated on first request for a tuple and are stable
// thereafter: asking twice for the same (testID, runNumber, attempt) returns
// the same identity, so a lookup is never a second allocation. Allocation
// order follows request order, which under concurrency is scheduling order.
//
// Safe for concurrent use by many goroutines.
type RunIDGenerator struct {
	mu      sync.Mutex
	clock   domain.Clock
	ordinal int
	seen    map[runTuple]string
}

// NewRunIDGenerator returns a generator whose instants come from clock, so
// identities are deterministic under an injected clock at any bound.
func NewRunIDGenerator(clock domain.Clock) *RunIDGenerator {
	return &RunIDGenerator{
		clock: clock,
		seen:  make(map[runTuple]string),
	}
}

// Next returns the run identity for one attempt. The result always satisfies
// domain.ValidRunID.
//
// Ordinals are allocated on first call for a tuple. A second call with the
// same tuple returns the same identity without consuming a new ordinal.
//
// Capacity is MaxRunsPerSuite distinct tuples. Beyond that limit the ordinal
// would wrap and produce a duplicate identity; Suite refuses such a plan
// before execution rather than allowing the collision, so Next is never called
// past capacity in practice and needs no error return.
func (g *RunIDGenerator) Next(testID string, runNumber, attempt int) string {
	g.mu.Lock()
	defer g.mu.Unlock()

	key := runTuple{testID: testID, runNumber: runNumber, attempt: attempt}
	if id, ok := g.seen[key]; ok {
		return id
	}

	suffix := fmt.Sprintf("%04x", g.ordinal)
	g.ordinal++

	id := domain.FormatRunID(g.clock.Now(), suffix)
	g.seen[key] = id
	return id
}
