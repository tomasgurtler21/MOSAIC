package transform

// skill_version_rename.go provides RenameSkillVersionField, a pure function that
// renames the version frontmatter field to the mosaic_-prefixed form in skill source
// bytes. It is called from buildContent for skill artifacts only; agent artifacts are
// not affected.

import (
	"mosaic-common/docformat"
	"mosaic-deploy/internal/agentfields"
)

// RenameSkillVersionField reads the source bytes, renames the skill version frontmatter
// field from its generic source name to its deployed prefixed name (using the agentfields
// registry), and returns the modified bytes. If the source has no version field or
// cannot be parsed, the source is returned unchanged with a nil error (graceful
// degradation). This function is pure: no filesystem, network, or clock access.
//
// Field ordering: the rename preserves the original position of the version field in
// the frontmatter key order. Unlike applyFrontmatter's id/role rename (which appends
// and relies on a subsequent Reorder step), this function uses a positional Insert so
// the renamed field stays where it was.
func RenameSkillVersionField(source []byte) ([]byte, error) {
	doc, err := docformat.Parse(source)
	if err != nil {
		// Unparseable frontmatter: return source unchanged, nil error (graceful degradation).
		return source, nil
	}

	fm := doc.Frontmatter()

	// Look up the registry entry for "version" to get the deployed (prefixed) name.
	entry, ok := agentfields.ByGeneric("version")
	if !ok {
		// Registry entry missing — programming error, but degrade gracefully.
		return source, nil
	}

	// Capture the current key order to determine the position of "version".
	keys := fm.Keys()
	versionIdx := -1
	for i, k := range keys {
		if k == "version" {
			versionIdx = i
			break
		}
	}
	if versionIdx < 0 {
		// No "version" field present: return source unchanged.
		return source, nil
	}

	// Capture the value before removing.
	val, _ := fm.Get("version")

	// Determine the positional anchor: the key immediately after "version" in the list.
	var nextKey string
	if versionIdx+1 < len(keys) {
		nextKey = keys[versionIdx+1]
	}

	// Remove the old key.
	fm.Remove("version")

	// Insert the new key at the original position.
	var insertErr error
	if nextKey != "" {
		insertErr = fm.Insert(entry.Deployed, val, docformat.Position{Before: nextKey})
	} else {
		// "version" was the last field — append to end.
		insertErr = fm.Insert(entry.Deployed, val, docformat.Position{})
	}
	if insertErr != nil {
		// Insert failed (should not happen in practice): return source unchanged.
		return source, nil
	}

	return doc.Bytes(), nil
}
