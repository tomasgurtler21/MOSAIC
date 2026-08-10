package domain

// LayerSuppresses reports whether a test layer suppresses an assertion class
// entirely — the class is not evaluated, whatever the evidence says.
//
// This is the single source of truth. Evaluation consults it to return
// AssertionNotEvaluated; preflight consults it to warn an author before a run
// that a declared assertion will do nothing. Restating it in two packages is
// exactly how the two would drift.
//
// Current rule: layer: orchestrator suppresses protocol_violations. Nothing
// else is suppressed.
func LayerSuppresses(layer TestLayer, class AssertionClass) bool {
	return layer == LayerOrchestrator && class == ClassProtocolViolations
}

// SuppressionReason explains a suppression in the words the report and the
// diagnostic both use, so an author reads one sentence, not two.
//
// It is only meaningful when LayerSuppresses reports true for the same
// arguments; callers are expected to check that first.
func SuppressionReason(layer TestLayer, class AssertionClass) string {
	if layer == LayerOrchestrator && class == ClassProtocolViolations {
		return "layer: orchestrator suppresses protocol-message validation"
	}
	return ""
}
