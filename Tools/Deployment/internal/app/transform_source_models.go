package app

// transform_source_models.go declares the pre-pass types and functions that discover the
// distinct source-model set across a transform batch. Declarations are exported so both
// in-package callers and external test packages can reference them directly.

import (
	"os"
	"sort"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// SourceFile is one enumerated transform input, read exactly once. The pre-pass
// produces these; the per-file loop consumes them instead of re-reading from disk.
type SourceFile struct {
	// Path is the absolute or caller-supplied path of the input file.
	Path string
	// Content is the file's bytes. Nil when ReadErr is non-nil.
	Content []byte
	// ReadErr is the error from reading the file, or nil. A non-nil ReadErr is a
	// per-file failure (StatusFailed), never a whole-run failure.
	ReadErr error
	// SourceModel is the model identifier read from Content's frontmatter, or
	// UnsetSourceModel when the file declares none or could not be read/parsed.
	SourceModel string
}

// UnsetSourceModel is the mapping key for the group of agents whose source file
// carries no model at all. It is the empty string so that a zero-value mapping
// lookup and an explicit "unset" lookup are the same operation.
const UnsetSourceModel = ""

// SourceModelIndex is the transform pre-pass's output: every enumerated input file
// read exactly once, plus the distinct source-model set the questions are driven from.
type SourceModelIndex struct {
	// Files is one entry per enumerated input path, in the caller's enumeration order
	// (lexicographic for a directory input). Length always equals len(filePaths).
	Files []SourceFile
	// Distinct is the deduplicated set of SourceModel values across Files, sorted
	// lexicographically ascending, with UnsetSourceModel last when present. Empty
	// only when Files is empty.
	Distinct []string
}

// ReadSourceModel extracts the source harness's model identifier from an agent
// document's frontmatter. Resolution order, matching how retarget.go identifies the
// source model when stripping it:
//
//  1. srcDesc.Frontmatter.ModelKey, when that key is declared and present as a
//     non-empty scalar.
//  2. The generic "model" key, when present as a non-empty scalar.
//  3. Otherwise UnsetSourceModel.
//
// Unparseable content yields UnsetSourceModel and no error: a file that cannot be
// parsed is classified by the existing per-file detection path, not here.
func ReadSourceModel(content []byte, srcDesc domain.HarnessDescriptor) string {
	if len(content) == 0 {
		return UnsetSourceModel
	}
	doc, err := docformat.Parse(content)
	if err != nil {
		return UnsetSourceModel
	}
	fm := doc.Frontmatter()

	// Step 1: check the harness-declared model key when the descriptor names one.
	if srcDesc.Frontmatter.ModelKey != "" {
		if v, ok := fm.Get(srcDesc.Frontmatter.ModelKey); ok {
			if v.Kind == domain.KindScalar && v.Scalar != "" {
				return v.Scalar
			}
		}
	}

	// Step 2: fall back to the generic "model" key.
	if v, ok := fm.Get("model"); ok {
		if v.Kind == domain.KindScalar && v.Scalar != "" {
			return v.Scalar
		}
	}

	return UnsetSourceModel
}

// IndexSourceModels reads each enumerated path once and returns the batch's source
// files together with the distinct source-model set. Enumeration and extension
// filtering are the caller's job — this function is given the final path list, so
// the pre-pass and the per-file loop can never diverge on which files are in scope.
func IndexSourceModels(filePaths []string, srcDesc domain.HarnessDescriptor) SourceModelIndex {
	if len(filePaths) == 0 {
		return SourceModelIndex{}
	}

	files := make([]SourceFile, len(filePaths))
	seen := make(map[string]bool)
	hasUnset := false

	for i, path := range filePaths {
		content, err := os.ReadFile(path)
		if err != nil {
			files[i] = SourceFile{
				Path:        path,
				Content:     nil,
				ReadErr:     err,
				SourceModel: UnsetSourceModel,
			}
			hasUnset = true
		} else {
			model := ReadSourceModel(content, srcDesc)
			files[i] = SourceFile{
				Path:        path,
				Content:     content,
				ReadErr:     nil,
				SourceModel: model,
			}
			if model == UnsetSourceModel {
				hasUnset = true
			} else {
				seen[model] = true
			}
		}
	}

	// Build distinct set: named models sorted lexicographically, UnsetSourceModel last.
	named := make([]string, 0, len(seen))
	for model := range seen {
		named = append(named, model)
	}
	sort.Strings(named)

	distinct := named
	if hasUnset {
		distinct = append(distinct, UnsetSourceModel)
	}

	return SourceModelIndex{
		Files:    files,
		Distinct: distinct,
	}
}
