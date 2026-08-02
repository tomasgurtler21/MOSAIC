// Package analysis is the pure decision core of mosaic-log-analyzer.
// It takes already-decoded events (domain.Event) and a pricing table and
// produces a fully priced domain.Report, with all data-quality findings
// accumulated along the way. The package is deliberately free of I/O: it
// imports only internal/domain and the standard library's sort package,
// making it fast to test with in-memory fixtures and safe to run
// concurrently without locks.
package analysis
