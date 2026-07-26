// Package logging writes structured deployment run logs to two sinks: latest.log (truncated at
// run start) and history.log (appended). Logger methods return nothing; a log write failure is
// accumulated in Degraded() and surfaced in the run summary rather than propagated as an error,
// so logging can never abort or alter a deployment.
package logging
