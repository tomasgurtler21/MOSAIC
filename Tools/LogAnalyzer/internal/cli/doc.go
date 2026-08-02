// Package cli is the command-line interface frontend adapter for
// mosaic-log-analyzer. It builds the cobra command tree, defines the
// stable exit codes and JSON wire contracts, and provides the
// non-blocking Interaction implementation used when the tool runs
// without a terminal. The CLI never blocks waiting for user input;
// it resolves every answer from flags immediately or skips. This
// package must not import internal/tui.
package cli
