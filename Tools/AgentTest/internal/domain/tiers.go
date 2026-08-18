package domain

// TierTestSubject is the recommended_tier every agent-under-test in the test
// catalogue declares. Exactly one file carries it today: the orchestrator
// copy at catalog/Orchestrator/orchestrator.md.
//
// This is the single spelling of the tier name in Go source. No other package
// in this module may spell the literal; reference this constant instead.
const TierTestSubject = "TEST-SUBJECT"

// TierTestStub is the recommended_tier every test stub in the test catalogue
// declares, in both agents/ and catalog/Subagents/TestStubs/.
//
// This is the single spelling of the tier name in Go source. No other package
// in this module may spell the literal; reference this constant instead.
const TierTestStub = "TEST-STUB"
