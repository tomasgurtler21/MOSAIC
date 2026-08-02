package app

// workflow_update.go is a stub that satisfies the Service interface while the
// UpdateWorkflows use case implementation is being developed (TDD RED phase).
// Replace this file with the full implementation once all tests are green.

import (
	"context"
	"errors"

	"mosaic-deploy/internal/domain"
)

// UpdateWorkflows is the workflow-only update use case stub.
// This stub exists solely to make the package compile during the TDD RED phase.
// All tests that call UpdateWorkflows will fail because this returns an error.
func (s *service) UpdateWorkflows(ctx context.Context, req WorkflowUpdateRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, errors.New("UpdateWorkflows: not implemented")
}
