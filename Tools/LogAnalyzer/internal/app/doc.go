// Package app is the single use-case layer of mosaic-log-analyzer. It
// wires together the three I/O adapters (logscan, logread, pricing) via
// their domain port interfaces and drives the pure analysis core to
// produce a priced domain.Report. Both the CLI and the TUI frontend call
// into this package; neither the CLI nor the TUI package is ever imported
// here. Concrete adapter implementations are injected at construction time
// by the composition root in cmd/mosaic-log-analyzer.
package app
