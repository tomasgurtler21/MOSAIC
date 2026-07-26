package catalog

import (
	"os"
	"path/filepath"

	"mosaic-deploy/internal/docformat"
	"mosaic-deploy/internal/domain"
)

// loadSkills scans Agents/Generic/Skills/ for skill directories and populates skills,
// skillIdx, and sourcePaths on the receiver.
func (c *catalogImpl) loadSkills(root string) []Issue {
	skillsDir := filepath.Join(root, "Agents", "Generic", "Skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		// Missing skills directory is not a hard error.
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		key := entry.Name()
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

	// Enumerate all files in the directory (relative paths).
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return domain.Skill{}, err
	}
	for _, e := range dirEntries {
		if !e.IsDir() {
			skill.Files = append(skill.Files, e.Name())
		}
	}

	return skill, nil
}
