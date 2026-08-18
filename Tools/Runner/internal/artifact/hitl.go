package artifact

import (
	"context"
	"errors"
	"os"

	"mosaic-run/internal/domain"
)

// approvalReader implements domain.ApprovalReader using the existing
// splitDocument and parseFrontmatter helpers to read the human_approved
// frontmatter field without returning an error.
type approvalReader struct{}

// NewApprovalReader returns a domain.ApprovalReader that reads the
// human_approved frontmatter field from an artifact file. It reuses the
// artifact package's existing frontmatter parsing helpers so no second parser
// is introduced.
func NewApprovalReader() domain.ApprovalReader {
	return &approvalReader{}
}

// ReadApproval reads the human_approved frontmatter field from the artifact at
// artifactPath and maps every possible file and parse condition to a distinct
// HumanApproval value. It never returns an error and never panics.
func (r *approvalReader) ReadApproval(_ context.Context, artifactPath string) domain.HumanApproval {
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.ApprovalFileMissing
		}
		return domain.ApprovalUnreadable
	}

	fmContent, _, hasFM := splitDocument(data)
	if !hasFM {
		return domain.ApprovalNoFrontmatter
	}

	topLevel, _, parseErr := parseFrontmatter(fmContent)
	if parseErr != nil {
		return domain.ApprovalMalformed
	}

	val, ok := topLevel["human_approved"]
	if !ok {
		return domain.ApprovalAbsent
	}

	switch val {
	case "true":
		return domain.ApprovalTrue
	case "false":
		return domain.ApprovalFalse
	default:
		return domain.ApprovalMalformed
	}
}
