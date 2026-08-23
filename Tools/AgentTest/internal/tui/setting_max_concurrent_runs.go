package tui

// SettingMaxConcurrentRuns is the SettingKind for the maximum concurrent runs
// per-suite bound. It maps to --max-concurrent-runs on the CLI.
//
// The value bounds how many tests × repetitions execute concurrently across
// the whole suite. Deliberately named "MaxConcurrentRuns" rather than anything
// using "concurrency" bare: internal/concurrency already means peak
// collaborator invocations in flight within one run, which is a different
// concept.
const SettingMaxConcurrentRuns SettingKind = "max_concurrent_runs"
