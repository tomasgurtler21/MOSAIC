// Package sideeffects materialises a stub's declared file effects beneath a
// subject directory and records exactly what it created, so teardown
// removes exactly that and nothing else.
//
// This package performs I/O and therefore sits outside the pure decision
// core.
package sideeffects

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
)

// Applier materialises a stub's declared file effects and records exactly
// what it created so teardown removes exactly that and nothing else.
type Applier interface {
	// Apply writes each effect beneath subjectDir. Paths are resolved
	// relative to subjectDir; an effect resolving outside it is refused.
	// The returned ledger is cumulative-safe: it may be merged with a
	// previously persisted ledger.
	//
	// runID is expanded in place of domain.RunIDPlaceholder in every
	// effect's content (both inline Content and $ref-resolved bytes)
	// before writing. An empty runID disables content expansion (paths
	// are the caller's responsibility and are NOT expanded here).
	Apply(subjectDir string, effects []domain.FileEffect, runID string) (domain.EffectLedger, error)
}

// NewApplier constructs an Applier that resolves $ref effects through res.
func NewApplier(res fixtures.Resolver) Applier {
	return &applier{resolver: res}
}

// NewContextualApplier constructs an Applier whose $ref resolution is
// document-relative. On each Apply call, the factory is invoked with subjectDir
// to produce a per-call resolver, so $ref paths are searched starting from the
// call's subject directory rather than a single process-wide root.
func NewContextualApplier(factory fixtures.ResolverFactory) Applier {
	return &contextualApplier{factory: factory}
}

type contextualApplier struct {
	factory fixtures.ResolverFactory
}

func (a *contextualApplier) Apply(subjectDir string, effects []domain.FileEffect, runID string) (domain.EffectLedger, error) {
	resolver, err := a.factory(subjectDir)
	if err != nil {
		return domain.EffectLedger{}, fmt.Errorf("sideeffects: constructing resolver for %q: %w", subjectDir, err)
	}
	inner := &applier{resolver: resolver}
	return inner.Apply(subjectDir, effects, runID)
}

type applier struct {
	resolver fixtures.Resolver
}

func (a *applier) Apply(subjectDir string, effects []domain.FileEffect, runID string) (domain.EffectLedger, error) {
	var ledger domain.EffectLedger

	absSubject, err := filepath.Abs(subjectDir)
	if err != nil {
		return ledger, fmt.Errorf("sideeffects: resolving subject directory %q: %w", subjectDir, err)
	}

	createdDirs := make(map[string]bool)

	for _, effect := range effects {
		rel, fullPath, err := resolveEffectPath(absSubject, effect.Path)
		if err != nil {
			return ledger, err
		}

		var content []byte
		if effect.Ref != "" {
			content, err = a.resolver.Resolve(effect.Ref)
			if err != nil {
				return ledger, fmt.Errorf("sideeffects: resolving %q: %w", effect.Ref, err)
			}
		} else {
			content = []byte(effect.Content)
		}

		if runID != "" {
			content = []byte(strings.ReplaceAll(string(content), domain.RunIDPlaceholder, runID))
		}

		newDirs, err := mkdirAllTracked(absSubject, filepath.Dir(fullPath))
		if err != nil {
			return ledger, fmt.Errorf("sideeffects: creating directory for %q: %w", effect.Path, err)
		}
		for _, d := range newDirs {
			if !createdDirs[d] {
				createdDirs[d] = true
				ledger.Dirs = append(ledger.Dirs, d)
			}
		}

		if err := os.WriteFile(fullPath, content, 0o644); err != nil {
			return ledger, fmt.Errorf("sideeffects: writing %q: %w", effect.Path, err)
		}
		ledger.Files = append(ledger.Files, rel)
	}

	return ledger, nil
}

// resolveEffectPath resolves an effect's declared path against absSubject
// and refuses anything that would escape it.
func resolveEffectPath(absSubject, path string) (rel string, full string, err error) {
	cleaned := filepath.Clean(filepath.FromSlash(path))
	full = filepath.Join(absSubject, cleaned)

	rel, err = filepath.Rel(absSubject, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("sideeffects: effect path %q escapes the subject directory", path)
	}
	return rel, full, nil
}

// mkdirAllTracked creates dir (and any missing ancestors beneath root),
// returning the directories it newly created, relative to root, in
// creation order (outermost first).
func mkdirAllTracked(root, dir string) ([]string, error) {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		return nil, nil
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	var created []string
	cur := root
	accum := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		if accum == "" {
			accum = part
		} else {
			accum = filepath.Join(accum, part)
		}
		if _, statErr := os.Stat(cur); os.IsNotExist(statErr) {
			if err := os.Mkdir(cur, 0o755); err != nil {
				return created, err
			}
			created = append(created, accum)
		}
	}
	return created, nil
}

// SaveLedger persists a ledger, so a driver process can undo what an
// interceptor process created.
func SaveLedger(path string, l domain.EffectLedger) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("sideeffects: marshaling ledger: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("sideeffects: creating ledger directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("sideeffects: writing ledger %q: %w", path, err)
	}
	return nil
}

// LoadLedger reads a previously persisted ledger.
func LoadLedger(path string) (domain.EffectLedger, error) {
	var l domain.EffectLedger
	data, err := os.ReadFile(path)
	if err != nil {
		return l, fmt.Errorf("sideeffects: reading ledger %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &l); err != nil {
		return domain.EffectLedger{}, fmt.Errorf("sideeffects: parsing ledger %q: %w", path, err)
	}
	return l, nil
}

// Remove deletes exactly the files the ledger records, then removes the
// directories the ledger records as having been created (deepest first,
// and only when empty). A path absent from the ledger is never touched.
func Remove(subjectDir string, l domain.EffectLedger) error {
	absSubject, err := filepath.Abs(subjectDir)
	if err != nil {
		return fmt.Errorf("sideeffects: resolving subject directory %q: %w", subjectDir, err)
	}

	for _, f := range l.Files {
		full := filepath.Join(absSubject, filepath.FromSlash(f))
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("sideeffects: removing %q: %w", f, err)
		}
	}

	dirs := append([]string{}, l.Dirs...)
	sort.Slice(dirs, func(i, j int) bool {
		return depth(dirs[i]) > depth(dirs[j])
	})
	for _, d := range dirs {
		full := filepath.Join(absSubject, filepath.FromSlash(d))
		entries, err := os.ReadDir(full)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("sideeffects: reading directory %q: %w", d, err)
		}
		if len(entries) == 0 {
			if err := os.Remove(full); err != nil {
				return fmt.Errorf("sideeffects: removing directory %q: %w", d, err)
			}
		}
	}
	return nil
}

// depth reports how many path segments a cleaned relative path has, so
// deeper directories can be removed before their ancestors.
func depth(path string) int {
	return strings.Count(filepath.ToSlash(filepath.Clean(path)), "/")
}
