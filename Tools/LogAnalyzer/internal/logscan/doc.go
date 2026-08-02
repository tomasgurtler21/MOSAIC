// Package logscan implements the domain.LogSource port. It discovers
// log data by inspecting the directory structure of an OrchestrationLogs
// tree, a single run folder, or the unattributable bucket — without
// reading any file contents. Every ambiguous or unrecognised path
// produces a domain.Finding rather than an error, so the rest of the
// tool always receives a fully described outcome regardless of what the
// filesystem contains.
package logscan
