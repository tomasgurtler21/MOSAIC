package docformat_test

// Vocabulary tests for ArtifactProvenance reclassification — superseded by Stage 2.
//
// ArtifactProvenance was reclassified from a canonical SECTION to a tool-managed
// DEPLOYED boundary prior to Stage 2, as uncommitted, task-unattributed pre-work.
// This change was NOT a completed Stage 1 task: Stage 1's PlanProgress.md shows all
// I1.* implementation tasks unchecked and Stage 1's Plan.md explicitly disclaims
// vocabulary work ("its vocabulary tables are Stage 2's concern").
//
// In Stage 2, ArtifactProvenance is removed from the vocabulary entirely: it is
// absent from CanonicalDeployed, CanonicalOrder, and DeployedParent.
// ArtifactProvenanceExtension is likewise removed from InjectionParent.
//
// All tests that asserted ArtifactProvenance's pre-Stage-2 classification
// (InjectionProvenance class, position in CanonicalOrder, DeployedParent entry) and
// ArtifactProvenanceExtension's pre-Stage-2 parent ("" top-level sentinel) are removed
// here. The Stage 2 assertions for absence of these names live in vocabulary_stage2_test.go
// and vocabulary_boundary_sync_test.go.
//
// This file is intentionally empty of test functions. It is retained as a record of
// the pre-Stage-2 → Stage 2 vocabulary transition.
