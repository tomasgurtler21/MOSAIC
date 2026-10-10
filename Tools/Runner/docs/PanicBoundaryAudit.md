# Panic Boundary Audit

Defense in depth: a panic in a Runner `tea.Cmd` closure running session or test
work, or in a CLI run, becomes a reported error or outcome, and the panic value
and full stack are written to the Runner debug log (`runner.error`, field
`site`). Boundaries do not replace fixing panics at their source.

Search basis: every `go` statement and every `func() tea.Msg` closure in
non-test code under `Tools/Runner` and `Tools/Common`.

## Bubble Tea finding

Bubble Tea (`github.com/charmbracelet/bubbletea` v1.3.10) recovers panics in
`tea.Cmd` goroutines unless `WithoutCatchPanics` is set. It then prints the
panic to stdout, sends `ErrProgramPanic` and cancels the program: the TUI is
torn down and nothing reaches the Runner debug log. A per-command boundary
(`guardCmd`) is therefore required so the panic becomes a message the model
handles.

## Sites

| Site | Kind | Decision | Reason |
|------|------|----------|--------|
| `internal/tui/update_run.go` `startSession` | `tea.Cmd` running `session.Start` | Boundary (`guardCmd`, site `session`) | Yields `runErrorMsg` with prefix `panic in session: `; logs value and stack. |
| `internal/tui/update_testflow.go` `launchTestRun` closure | `tea.Cmd` running the test flow (catalog load, deploy, orchestrations) | Boundary (`guardCmd`, site `test run`) | Yields `testAllDoneMsg` carrying the error as `DeployError`; logs value and stack. |
| `internal/tui/update_setup.go` `launchSession` | Builds the session, batches `startSession` | Not needed | Runs no work itself; covered by `startSession`. |
| `internal/cli/run.go` `runCmd.RunE` | CLI run | Existing boundary kept as last resort | Stack to error output, `ExitFailure`. `cli.Run` stays logger-free. |
| `cmd/mosaic-run/main.go` CLI composition (`newPanicLoggingSession`) | CLI session | Boundary added | Converts a panic in `Start` into an error with a `RunFailed` outcome and logs value and stack; sits inside `newRunLifecycleSession` so `run_end` is still written. |
| `cmd/mosaic-run/mosaiclog_wiring.go` `newCLIRunContext` goroutine | Signal watcher | Not needed | A `select` that only calls `cancel()` and the signal-stop seam; no work, no I/O. |
| `Tools/Common/harness/spawn.go` `Run` wait goroutine | `waitCh <- execCmd.Wait()` | Not needed | Standard-library `Wait` only; Common is shared with AgentTest and Deployment, so a change there is out of proportion. |
| Harness stdout/stderr readers, background loggers | n/a | None exist | `Run` assigns `bytes.Buffer`s; `os/exec` copies internally. |

The inventory search found no additional site. No open follow-ups.

Other tools' goroutines (AgentTest, Deployment) are out of scope.
