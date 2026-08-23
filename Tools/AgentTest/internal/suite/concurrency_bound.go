package suite

// DefaultMaxConcurrentRuns is the bound used when no value is configured.
//
// Four, chosen conservatively. Each concurrent run is a full harness process
// plus a deployed agent tree plus a relocated harness configuration tree on
// disk, and every dispatch inside it spawns short-lived interceptor processes
// contending on that run's own lock file. Four gives most of the wall-clock
// relief the feature exists for while keeping simultaneous sandboxes to a
// handful and staying well below the concurrency a provider account is likely
// to permit. A user who wants more sets it explicitly, having read the
// documented disk and process cost.
//
// Note: this constant deliberately avoids the word "concurrency" to prevent
// confusion with internal/concurrency, which means peak collaborator
// invocations in flight within one run — a different thing entirely.
const DefaultMaxConcurrentRuns = 4
