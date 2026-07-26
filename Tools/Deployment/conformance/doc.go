// Package conformance provides the cross-tier conformance suite that keeps the three
// harness provision tiers honest.
//
// The known failure mode of tiered extensibility is that the external escape hatch is
// never exercised and silently rots. The mitigation here is structural: the OpenCode
// built-in harness is also built as a reference external module from the same source, and
// this suite drives every built-in through both the in-process path and the external path,
// asserting byte-identical output on the full golden agent set.
//
// The descriptor-only tier is also driven through the suite so all three tiers are covered
// by one executable.
//
// The suite is wired into the standard test run so it executes on every build.
package conformance
