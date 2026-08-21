package catalog

import (
	"os"
	"path/filepath"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/catalog/catalogpaths"
	"mosaic-deploy/internal/domain"
)

// loadSkills scans skill directories for a given catalogue root and populates skills,
// skillIdx, and sourcePaths on the receiver. The new target layout path (Skills/) is
// scanned first; the legacy path (Agents/Generic/Skills/) is scanned second. The first
// occurrence of any given key wins; duplicates from the fallback path are skipped.
func (c *catalogImpl) loadSkills(root string) []Issue {
	skillsDirCandidates := []string{
		catalogpaths.SkillsDir(root),
		filepath.Join(root, "Agents", "Generic", "Skills"),
	}
	for _, skillsDir := range skillsDirCandidates {
		entries, err := os.ReadDir(skillsDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			key := entry.Name()
			if _, exists := c.skillIdx[key]; exists {
				continue // Already loaded from a higher-priority path.
			}
			skillDir := filepath.Join(skillsDir, key)
			skill, err := parseSkillDir(skillDir, key)
			if err != nil {
				continue
			}
			c.skills = append(c.skills, skill)
			c.skillIdx[key] = skill

			// Register all skill file paths so ReadSource can serve them.
			for _, f := range skill.Files {
				c.sourcePaths[filepath.Join(skill.SourceDir, f)] = true
			}
		}
	}
	return nil
}

// loadSkillsMerged merges skills from defaultCatalogRoot and catalogRoot into the receiver.
// Skills are first loaded from defaultCatalogRoot (building the base set), then from
// catalogRoot. On key collision, the catalogRoot version replaces the default one silently —
// no Issue is produced for shadowing. When both roots are the same path, only one load pass
// is performed to prevent duplicate entries.
//
// Each root is scanned at two locations: the new target layout path first, then the legacy
// path. Within a single root, the first occurrence of a key wins.
func (c *catalogImpl) loadSkillsMerged(defaultCatalogRoot, catalogRoot string) []Issue {
	// Identical-roots case: load once to avoid doubling the result.
	if defaultCatalogRoot == catalogRoot {
		return c.loadSkills(defaultCatalogRoot)
	}

	// First pass: load from the default catalogue root (builds the base set).
	c.loadSkills(defaultCatalogRoot)

	// Second pass: scan the catalogue root and merge, with catalogue root winning on collision.
	// Track keys already processed in this pass so that within catalogRoot the new layout
	// path wins over the legacy path (first-wins within this root).
	seenInPass := make(map[string]bool)
	catalogRootDirs := []string{
		catalogpaths.SkillsDir(catalogRoot),
		filepath.Join(catalogRoot, "Agents", "Generic", "Skills"),
	}
	for _, skillsDir := range catalogRootDirs {
		entries, err := os.ReadDir(skillsDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			key := entry.Name()
			if seenInPass[key] {
				continue // Already processed this key from a higher-priority path in this pass.
			}
			seenInPass[key] = true
			skillDir := filepath.Join(skillsDir, key)
			skill, err := parseSkillDir(skillDir, key)
			if err != nil {
				continue
			}
			if _, exists := c.skillIdx[key]; exists {
				// Key collision: catalogue root wins, replace the existing entry in the slice.
				for i, s := range c.skills {
					if s.Key == key {
						c.skills[i] = skill
						break
					}
				}
			} else {
				// New key: append.
				c.skills = append(c.skills, skill)
			}
			c.skillIdx[key] = skill

			// Register all skill file paths so ReadSource can serve them.
			for _, f := range skill.Files {
				c.sourcePaths[filepath.Join(skill.SourceDir, f)] = true
			}
		}
	}
	return nil
}

// parseSkillDir reads SKILL.md from the given directory and enumerates every file in the
// directory as part of the skill's file set.
func parseSkillDir(dir, key string) (domain.Skill, error) {
	entryFile := "SKILL.md"
	skillMd := filepath.Join(dir, entryFile)
	data, err := os.ReadFile(skillMd)
	if err != nil {
		return domain.Skill{}, err
	}
	doc, err := docformat.Parse(data)
	if err != nil {
		return domain.Skill{}, err
	}
	fm := doc.Frontmatter()

	skill := domain.Skill{
		Key:       key,
		SourceDir: dir,
		EntryFile: entryFile,
	}

	if v, ok := fm.Get("name"); ok && v.Kind == domain.KindScalar {
		skill.Name = v.Scalar
	}
	if v, ok := fm.Get("description"); ok && v.Kind == domain.KindScalar {
		skill.Description = v.Scalar
	}
	if v, ok := fm.Get("version"); ok && v.Kind == domain.KindScalar {
		skill.Version = v.Scalar
	}

	// Enumerate all files recursively (relative paths, lexicographic depth-first order).
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil // descend into subdirectories but do not add them to Files
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		skill.Files = append(skill.Files, rel)
		return nil
	})
	if err != nil {
		return domain.Skill{}, err
	}

	return skill, nil
}
