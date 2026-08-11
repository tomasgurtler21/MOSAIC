package app

// transform_contracts.go declares the public contract for the harness-to-harness transform
// mode: request/result types, per-file status constants, and the Service implementation.
// The real implementation lives in transform_service_impl.go.

import (
	"context"

	"mosaic-deploy/internal/domain"
)

// TransformHarnessRequest drives the harness-to-harness transform mode. Following CD-6,
// any field that is set short-circuits its interactive question; an empty field triggers
// the question through domain.Interaction.
type TransformHarnessRequest struct {
	// SourceHarnessID is the harness the input files were deployed for. Supplied by the
	// first-screen harness selection (TUI) or --harness (CLI). Required.
	SourceHarnessID string
	// TargetHarnessID is the harness to produce. Empty triggers QTransformTargetHarness.
	TargetHarnessID string
	// Path is a single agent file or a directory of agent files. Empty triggers the
	// existing workspace/path question. Quote-stripped by the frontend via pathinput.
	Path string
	// TargetModel is the target harness's model identifier. Empty triggers
	// QTransformTargetModel; a skipped or empty answer leaves the output's model field
	// empty. SkipAll[QTransformTargetModel] suppresses the question entirely.
	TargetModel string
	// Overwrite permits replacing an existing destination file. Without it, an existing
	// destination is a per-file failure, never a silent overwrite.
	Overwrite bool
	// DryRun computes and reports every outcome but writes no file.
	DryRun bool
	// SkipAll suppresses the named questions, matching every other request type.
	// Note: QTransformTargetHarness is intentionally not honoured here — a target harness
	// is a mandatory input and has no "none, don't ask" answer. Only QTransformTargetModel
	// is skippable via SkipAll (and is explicitly documented above as such).
	SkipAll map[domain.QuestionID]bool
}

// TransformFileStatus is the per-file outcome kind.
type TransformFileStatus string

const (
	// StatusTransformed — output produced (or computed, under DryRun).
	StatusTransformed TransformFileStatus = "transformed"
	// StatusSkippedMismatch — the file's detected harness is not SourceHarnessID (FR-17).
	StatusSkippedMismatch TransformFileStatus = "skipped-mismatch"
	// StatusSkippedNotAgent — the file is not a transformed MOSAIC agent.
	StatusSkippedNotAgent TransformFileStatus = "skipped-not-agent"
	// StatusFailed — the file matched but could not be transformed or written, including
	// the refused-overwrite case.
	StatusFailed TransformFileStatus = "failed"
)

// TransformFileOutcome is one input file's result. Exactly one entry exists per input file
// considered, in enumeration order (lexicographic by file name for a directory input).
type TransformFileOutcome struct {
	SourcePath string `json:"sourcePath"`
	// DestinationPath is the path resolved via the target harness's own path resolution.
	// Empty for every status other than StatusTransformed.
	DestinationPath string              `json:"destinationPath,omitempty"`
	Status          TransformFileStatus `json:"status"`
	// Reason explains any status other than StatusTransformed, in user-facing language.
	Reason string `json:"reason,omitempty"`
	// Warning carries a non-blocking note for a transformed file — currently the
	// indeterminate-harness-detection case.
	Warning string `json:"warning,omitempty"`
	// AgentKey is the artifact slug, when resolved.
	AgentKey string `json:"agentKey,omitempty"`
	// Tools are the target-harness tool names written to the output, in emission order.
	Tools []string `json:"tools,omitempty"`
	// CarriedVerbatimTools are custom tools kept under their original name.
	CarriedVerbatimTools []string `json:"carriedVerbatimTools,omitempty"`
	// StrippedFields lists fields removed from this file's output.
	StrippedFields []StrippedField `json:"strippedFields,omitempty"`
}

// TransformHarnessResult is the whole-run report, rendered by both frontends.
type TransformHarnessResult struct {
	SourceHarnessID string `json:"sourceHarnessId"`
	TargetHarnessID string `json:"targetHarnessId"`
	// TargetModel is the model written into every output. Empty when skipped.
	TargetModel string `json:"targetModel,omitempty"`
	// InputPath is the file or directory the run was given, after quote stripping.
	InputPath string `json:"inputPath"`
	// InputIsDirectory records which input shape was used, so a size-1 batch is
	// distinguishable from a single-file run in the rendered output.
	InputIsDirectory bool `json:"inputIsDirectory"`
	DryRun           bool `json:"dryRun"`
	// Files is the per-file outcome list, one entry per file considered.
	Files []TransformFileOutcome `json:"files"`
	// Counts are the summary tallies; each equals the number of Files entries with the
	// corresponding status.
	Transformed     int `json:"transformed"`
	SkippedMismatch int `json:"skippedMismatch"`
	SkippedNotAgent int `json:"skippedNotAgent"`
	Failed          int `json:"failed"`
}

// TransformHarness converts one or more already-deployed agents from a source harness
// into a target harness's deployed form. The real implementation is in transform_service_impl.go.
func (s *service) TransformHarness(ctx context.Context, req TransformHarnessRequest) (TransformHarnessResult, error) {
	return transformHarness(ctx, s, req)
}
