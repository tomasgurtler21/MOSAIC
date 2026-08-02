// Package screens defines the Screen interface and the screen-ID constants
// used by the mosaic-log-analyzer TUI. Each screen (source selection, run
// list, run detail, pricing entry, and the no-data placeholder) satisfies
// the Screen interface so that the top-level tui.Model can route input and
// render output uniformly without knowing the details of any individual
// screen. This package must not import internal/cli.
package screens
