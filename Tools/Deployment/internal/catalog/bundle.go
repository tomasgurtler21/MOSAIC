package catalog

// bundle.go declares the contract surface for deployed-sections bundle loading and provides
// the filesystem-backed implementation.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/catalog/catalogpaths"
	"mosaic-deploy/internal/domain"
)

// BundleSourceRelPath is the bundle's path relative to the MOSAIC root.
const BundleSourceRelPath = catalogpaths.MosaicRelBundleFile

// Sentinel errors returned by BundleLoader.LoadBundle. Each is distinct so callers can
// discriminate failure modes with errors.Is.
var (
	// ErrBundleSourceMissing is returned when the bundle source file does not exist or
	// cannot be read.
	ErrBundleSourceMissing = errors.New("bundle source file missing or unreadable")

	// ErrBundleSourceUnparseable is returned when docformat.Parse fails on the bundle file.
	ErrBundleSourceUnparseable = errors.New("bundle source file could not be parsed")

	// ErrBundleVersionMissing is returned when the bundle source document's frontmatter
	// carries no scalar `bundle_version` field. A versionless bundle defeats staleness detection.
	ErrBundleVersionMissing = errors.New("bundle source document has no bundle_version in frontmatter")

	// ErrBundleBlocksMissing is returned when the bundle source document declares no blocks
	// in its frontmatter `blocks` list.
	ErrBundleBlocksMissing = errors.New("bundle source document declares no blocks")

	// ErrBundleBlockDeclInvalid is returned when a block declaration in the `blocks` list
	// is missing a required field (name, target, applies_to, or specified_in).
	ErrBundleBlockDeclInvalid = errors.New("bundle block declaration is missing a required field")

	// ErrBundleBlockMissing is returned when the section named by a block declaration does
	// not exist in the bundle document body.
	ErrBundleBlockMissing = errors.New("declared bundle block has no matching section in the document body")

	// ErrBundleBlockEmpty is returned when a declared block's section exists in the body but
	// has whitespace-only content. Deploying an empty block would produce an agent region that
	// appears complete but instructs nothing.
	ErrBundleBlockEmpty = errors.New("declared bundle block has empty content")
)

// BundleLoader reads the canonical deployed-sections bundle and returns its deployable
// payload. Implementations are stateless; the app layer calls LoadBundle once per run,
// before any file is written.
type BundleLoader interface {
	// LoadBundle reads <mosaicRoot>/Catalog/DeployedSections.md, extracts the
	// frontmatter `bundle_version` scalar and every entry of the frontmatter `blocks`
	// list, and for each entry extracts the body region named by that entry's `name`
	// via doc.Body().SectionDeep(name).
	//
	// The returned block payload is the boundary's inner content only: the block's own
	// tag lines and all surrounding maintainer prose are excluded.
	//
	// Every returned error wraps exactly one sentinel above and names BundleSourceRelPath.
	// A non-nil error is returned with a zero-value BundleContent — the loader is
	// all-or-nothing and never returns partial content.
	LoadBundle(mosaicRoot string) (domain.BundleContent, error)
}

// FileBundleLoader is the filesystem-backed implementation of BundleLoader.
type FileBundleLoader struct{}

// LoadBundle reads the bundle source document and returns the version and all declared blocks.
// It is all-or-nothing: any error returns a zero-value BundleContent.
func (FileBundleLoader) LoadBundle(mosaicRoot string) (domain.BundleContent, error) {
	relPath := BundleSourceRelPath
	absPath := filepath.Join(mosaicRoot, relPath)

	data, err := os.ReadFile(absPath)
	if err != nil {
		return domain.BundleContent{}, fmt.Errorf("%s: %w: %w", relPath, ErrBundleSourceMissing, err)
	}

	doc, parseErr := docformat.Parse(data)
	if parseErr != nil {
		return domain.BundleContent{}, fmt.Errorf("%s: %w: %w", relPath, ErrBundleSourceUnparseable, parseErr)
	}

	fm := doc.Frontmatter()

	// Extract bundle_version scalar.
	versionVal, ok := fm.Get("bundle_version")
	if !ok || versionVal.Kind != domain.KindScalar || versionVal.Scalar == "" {
		return domain.BundleContent{}, fmt.Errorf("%s: %w: no non-empty 'bundle_version' scalar in frontmatter", relPath, ErrBundleVersionMissing)
	}
	version := versionVal.Scalar

	// Extract the blocks list.
	blocksVal, ok := fm.Get("blocks")
	if !ok || blocksVal.Kind != domain.KindList || len(blocksVal.Items) == 0 {
		return domain.BundleContent{}, fmt.Errorf("%s: %w: no 'blocks' list in frontmatter or list is empty", relPath, ErrBundleBlocksMissing)
	}

	blocks := make([]domain.BundleBlock, 0, len(blocksVal.Items))
	for _, item := range blocksVal.Items {
		if item.Kind != domain.KindMapping {
			return domain.BundleContent{}, fmt.Errorf("%s: %w: block entry is not a mapping", relPath, ErrBundleBlockDeclInvalid)
		}

		// Build a key→value map from the mapping pairs (scalar values only).
		pairMap := make(map[string]string, len(item.Pairs))
		for _, pair := range item.Pairs {
			if pair.Value.Kind == domain.KindScalar {
				pairMap[pair.Key] = pair.Value.Scalar
			}
		}

		name := pairMap["name"]
		target := pairMap["target"]
		appliesTo := pairMap["applies_to"]
		specifiedIn := pairMap["specified_in"]

		if name == "" || target == "" || appliesTo == "" || specifiedIn == "" {
			return domain.BundleContent{}, fmt.Errorf(
				"%s: %w: one or more required fields (name, target, applies_to, specified_in) are missing or empty",
				relPath, ErrBundleBlockDeclInvalid,
			)
		}

		// Find the named section in the document body.
		node, found := doc.Body().SectionDeep(name)
		if !found {
			return domain.BundleContent{}, fmt.Errorf("%s: %w: section %q not found in document body", relPath, ErrBundleBlockMissing, name)
		}

		content := node.Content()
		if len(bytes.TrimSpace(content)) == 0 {
			return domain.BundleContent{}, fmt.Errorf("%s: %w: section %q has empty or whitespace-only content", relPath, ErrBundleBlockEmpty, name)
		}

		blocks = append(blocks, domain.BundleBlock{
			Name:        name,
			Target:      target,
			AppliesTo:   appliesTo,
			SpecifiedIn: specifiedIn,
			Content:     content,
		})
	}

	return domain.BundleContent{
		Version: version,
		Blocks:  blocks,
	}, nil
}
