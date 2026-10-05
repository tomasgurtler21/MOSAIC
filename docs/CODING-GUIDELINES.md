# Coding Guidelines

This codebase is developed and maintained entirely by AI agents. These guidelines exist for one reason: **agent performance**. Large files, long functions, and inconsistent patterns force agents to burn context window reading irrelevant code, make multiple read passes to find what they need, and pick wrong conventions when writing new code.

Follow these rules when writing or modifying code.

---

## File Size

**Hard limit: 500 lines per file.** If a file exceeds this, split it by concern before adding more code.

For test files specifically: split by the behavior or function under test. `session_test.go` testing 15 behaviors in 12,000 lines is not one file — it's 15 files that got concatenated. Use `session_connect_test.go`, `session_timeout_test.go`, etc.

When splitting, move tests and the code they exercise together so `grep` still finds related pieces nearby.

## Function Length

**Hard limit: 150 lines per function.** If a function exceeds this, extract named helpers.

Go functions run longer than in most languages — error handling, table-driven tests, and switch statements add legitimate verbosity. 150 lines accommodates that while still catching actual god-functions. If a function exceeds this limit, it's doing too many things.

This applies equally to test functions. A `TestSessionConnect` with 400 lines of setup, 15 subtests, and inline assertions should be broken into helpers: `setupConnectTest()`, individual `t.Run` blocks calling focused assertion helpers, etc.

## Test Helpers and Shared Fixtures

**Extract repeated setup into test helpers.** Do not copy-paste setup code across test functions.

When you see the same 20 lines of struct initialization in 10 test functions, extract it into a helper in a `_test.go` file in the same package (or a `testutil` sub-package if shared across packages).

Common patterns to extract:
- Builder functions: `newTestSession(t, opts...)` instead of inline struct construction
- Fixture factories: reusable test data with sensible defaults and option overrides
- Custom assertions: `assertEventPublished(t, events, expectedType)` instead of repeated loops with `t.Fatalf`

This is the single biggest lever for keeping test files small.

## Package Scope

A package should have a **focused responsibility**. If a package exports more than ~15 symbols (functions, types, constants), it's probably doing too much and should be split.

Signs a package needs splitting:
- The `doc.go` or package comment needs multiple paragraphs to explain what it does
- Files in the package don't import each other (they're independent concerns sharing a namespace)
- You need a filename like `session_connect.go` and `session_timeout.go` — that's two packages, not one

## Naming Consistency

Pick one convention per pattern and use it everywhere:

| Pattern | Convention | Not |
|---------|-----------|-----|
| Constructors | `NewFoo(...)` | `CreateFoo`, `BuildFoo`, `MakeFoo` |
| Test constructors | `newTestFoo(t, ...)` | `setupFoo`, `createTestFoo`, `makeFoo` |
| Options pattern | `WithBar(...)` | `SetBar`, `Bar(...)` |
| Interface mocks | `MockFoo` | `FakeFoo`, `StubFoo`, `TestFoo` |
| Test files | `foo_test.go` | `foo_tests.go`, `test_foo.go` |

When modifying existing code, match the surrounding convention even if you'd prefer a different one. Consistency beats preference.

## Import Organization

Group imports in this order, separated by blank lines:

1. Standard library
2. Third-party dependencies (modules from outside this repository, e.g. `github.com/...`)
3. First-party packages - the current module plus sibling modules of the same Go workspace (`Tools/go.work`, e.g. `mosaic-common/...`)

This is the standard Go convention. Agents rely on it to quickly identify a file's dependency footprint.
