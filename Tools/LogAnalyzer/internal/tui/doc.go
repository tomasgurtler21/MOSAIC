// Package tui is the interactive terminal-UI frontend adapter for
// mosaic-log-analyzer. It uses the bubbletea framework together with
// the shared scaffold from mosaic-common/tui to present a full-screen
// interface for browsing run reports, navigating per-actor breakdowns,
// and entering missing model prices interactively. The TUI is the
// default frontend when the tool detects an attached terminal. This
// package must not import internal/cli.
package tui
