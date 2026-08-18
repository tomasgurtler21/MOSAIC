package agentdeploy_test

// Compile-time assertions that *RenderError and *DeployError satisfy the
// authoring.StructuredFailure interface. These blank-identifier declarations
// fail at compile time if either type is missing any of the four accessor
// methods (FailureReason, FailureDetail, FailureToolMessage, FailureExitCode),
// making the structural constraint explicit and regression-proof against future
// interface signature changes.
//
// These assertions are in a test file so that the authoring package can be
// imported here without widening the production-code import rules enforced by
// importcheck.

import (
	"mosaic-agent-test/internal/agentdeploy"
	"mosaic-agent-test/internal/authoring"
)

var _ authoring.StructuredFailure = (*agentdeploy.RenderError)(nil)
var _ authoring.StructuredFailure = (*agentdeploy.DeployError)(nil)
