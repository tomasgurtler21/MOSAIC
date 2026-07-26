// Package registry discovers and constructs harness modules. It aggregates built-in harnesses
// (registered via init()), descriptor-only harnesses found under the MosaicDeploy/harnesses
// directory, and opted-in external harness executables, presenting them through a uniform Registry
// interface. Precedence when one harness id is provided by more than one tier is deterministic:
// external > descriptor-only > built-in. External harnesses require an explicit opt-in and are
// gated at Resolve time, not at listing time.
package registry
