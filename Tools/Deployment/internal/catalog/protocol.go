package catalog

// protocol.go declares the contract surface for protocol source loading and provides
// the filesystem-backed implementation.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// ProtocolSourceRelPath is the protocol document's path relative to the MOSAIC root.
const ProtocolSourceRelPath = "Development/Designs/CommunicationProtocol.md"

// Boundary names of the two deployable blocks in the protocol source document.
const (
	// ProtocolBlockSubagent is the compound section name for the worker/utility protocol block.
	ProtocolBlockSubagent = "CommunicationProtocol:Subagent"
	// ProtocolBlockOrchestrator is the compound section name for the orchestrator protocol block.
	ProtocolBlockOrchestrator = "CommunicationProtocol:Orchestrator"
)

// Sentinel errors returned by ProtocolLoader.LoadProtocol. Each is distinct so callers
// can discriminate failure modes with errors.Is.
var (
	// ErrProtocolSourceMissing is returned when the protocol source file does not exist
	// or cannot be read.
	ErrProtocolSourceMissing = errors.New("protocol source file missing or unreadable")

	// ErrProtocolSourceUnparseable is returned when docformat.Parse fails on the protocol
	// source file.
	ErrProtocolSourceUnparseable = errors.New("protocol source file could not be parsed")

	// ErrProtocolBlockMissing is returned when a required role block (Subagent or
	// Orchestrator) is absent from the protocol source document.
	ErrProtocolBlockMissing = errors.New("required protocol role block is absent from the source document")

	// ErrProtocolBlockEmpty is returned when a required role block is present but has
	// whitespace-only content. A blank block would silently deploy an empty protocol section.
	ErrProtocolBlockEmpty = errors.New("required protocol role block has empty content")

	// ErrProtocolVersionMissing is returned when the protocol source document's frontmatter
	// carries no scalar `version` field. A versionless protocol defeats staleness detection.
	ErrProtocolVersionMissing = errors.New("protocol source document has no version in frontmatter")
)

// ProtocolLoader reads the canonical protocol source document and returns its deployable payload.
// Implementations are expected to be stateless; the app layer calls LoadProtocol once per run.
type ProtocolLoader interface {
	// LoadProtocol reads <mosaicRoot>/Development/Designs/CommunicationProtocol.md, extracts
	// the two role blocks and the frontmatter version, and returns them as a ProtocolContent.
	//
	// The returned block payload is each boundary's inner content only, with the block's own
	// tag lines and all surrounding maintainer documentation excluded.
	//
	// Errors (each distinct and identifying the file path and the problem):
	//   ErrProtocolSourceMissing      — the file does not exist or cannot be read
	//   ErrProtocolSourceUnparseable  — docformat.Parse failed
	//   ErrProtocolBlockMissing       — a required role block is absent
	//   ErrProtocolBlockEmpty         — a required role block has whitespace-only content
	//   ErrProtocolVersionMissing     — frontmatter carries no scalar `version`
	LoadProtocol(mosaicRoot string) (domain.ProtocolContent, error)
}

// FileProtocolLoader is the filesystem-backed implementation of ProtocolLoader.
// Its body is added in I5.1.
type FileProtocolLoader struct{}

// LoadProtocol reads the protocol source document and returns the two role blocks plus
// the source version.
func (FileProtocolLoader) LoadProtocol(mosaicRoot string) (domain.ProtocolContent, error) {
	relPath := ProtocolSourceRelPath
	absPath := filepath.Join(mosaicRoot, relPath)

	data, err := os.ReadFile(absPath)
	if err != nil {
		return domain.ProtocolContent{}, fmt.Errorf(
			"%s: %w: %w", relPath, ErrProtocolSourceMissing, err,
		)
	}

	doc, err := docformat.Parse(data)
	if err != nil {
		return domain.ProtocolContent{}, fmt.Errorf(
			"%s: %w: %w", relPath, ErrProtocolSourceUnparseable, err,
		)
	}

	// Extract the frontmatter version field.
	fmVersion, ok := doc.Frontmatter().Get("version")
	if !ok {
		return domain.ProtocolContent{}, fmt.Errorf(
			"%s: %w: no 'version' key in frontmatter", relPath, ErrProtocolVersionMissing,
		)
	}
	if fmVersion.Scalar == "" {
		return domain.ProtocolContent{}, fmt.Errorf(
			"%s: %w: 'version' key is present but has an empty value", relPath, ErrProtocolVersionMissing,
		)
	}

	// Extract both role blocks. The blocks are SECTION nodes in the document, addressed
	// by their compound names.
	subagentBlock, err := extractBlock(doc, relPath, ProtocolBlockSubagent)
	if err != nil {
		return domain.ProtocolContent{}, err
	}

	orchestratorBlock, err := extractBlock(doc, relPath, ProtocolBlockOrchestrator)
	if err != nil {
		return domain.ProtocolContent{}, err
	}

	return domain.ProtocolContent{
		Version: fmVersion.Scalar,
		Blocks: map[domain.ProtocolVariant][]byte{
			domain.ProtocolSubagent:     subagentBlock,
			domain.ProtocolOrchestrator: orchestratorBlock,
		},
	}, nil
}

// extractBlock finds a named section block in the document and returns its inner content.
// Returns ErrProtocolBlockMissing when the block is absent, ErrProtocolBlockEmpty when
// the block is present but has whitespace-only content.
func extractBlock(doc *docformat.Document, relPath, blockName string) ([]byte, error) {
	node, found := doc.Body().SectionDeep(blockName)
	if !found {
		return nil, fmt.Errorf(
			"%s: %w: block %q not found", relPath, ErrProtocolBlockMissing, blockName,
		)
	}

	content := node.Content()
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, fmt.Errorf(
			"%s: %w: block %q has empty content", relPath, ErrProtocolBlockEmpty, blockName,
		)
	}

	return content, nil
}
