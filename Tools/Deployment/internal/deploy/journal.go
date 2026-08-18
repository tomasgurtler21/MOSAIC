package deploy

import (
	"os"
)

// writeJournal records filesystem write operations performed during an atomic execution run
// so they can be reversed if the run fails at any point. Each entry captures the state of
// a target path immediately before the write that would modify it.
//
// Memory note: original file bytes are buffered in-process. This is acceptable for the
// artifact sizes involved (markdown agent and skill files, a YAML manifest, and a small
// markdown checklist); no spill-to-disk mechanism is needed.
type writeJournal struct {
	entries []journalEntry
}

// journalEntry captures the pre-write state of one filesystem path.
type journalEntry struct {
	path     string      // absolute path about to be written
	existed  bool        // whether a readable file was at this path before the write
	original []byte      // exact prior contents; meaningless when existed is false
	mode     os.FileMode // prior permission bits; meaningless when existed is false
}

// record captures the current state of path immediately before it is written. If path
// does not exist, the entry records existed=false. If path exists but cannot be read,
// record returns an error — the caller must not proceed with the write, because without a
// valid journal entry the reversal could not restore the file.
func (j *writeJournal) record(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			j.entries = append(j.entries, journalEntry{path: path, existed: false})
			return nil
		}
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	j.entries = append(j.entries, journalEntry{
		path:     path,
		existed:  true,
		original: content,
		mode:     info.Mode(),
	})
	return nil
}

// revert applies the journal in reverse order of recording, restoring the workspace to its
// state before the run began. Every entry is attempted even when an earlier one fails;
// all failures are collected and returned rather than aborting the reversal mid-way.
//
// Deliberate non-reversals:
//   - Directories the run created are not removed. Leaving an empty directory behind is
//     acceptable and avoids the risk of accidentally deleting directories that predated the run
//     (directory creation is not journaled separately, so the executor cannot distinguish a
//     run-created directory from a pre-existing one).
//   - Backup copies written under <workspace>/.mosaic/backups/ are never included in the
//     journal and therefore survive reversal. Deleting a user's backup of their own locally
//     modified file is the more dangerous choice; a redundant backup is harmless.
func (j *writeJournal) revert() []RevertFailure {
	var failures []RevertFailure
	for i := len(j.entries) - 1; i >= 0; i-- {
		e := j.entries[i]
		if !e.existed {
			// The run created this file; delete it. Treat an already-absent file as success:
			// a previous reversal step may have handled it, or the write itself may have
			// failed, leaving nothing to delete.
			if err := os.Remove(e.path); err != nil && !os.IsNotExist(err) {
				failures = append(failures, RevertFailure{Path: e.path, Err: err, Restore: false})
			}
		} else {
			// The run overwrote this file; restore the original bytes and permission bits.
			if err := os.WriteFile(e.path, e.original, e.mode); err != nil {
				failures = append(failures, RevertFailure{Path: e.path, Err: err, Restore: true})
			}
		}
	}
	return failures
}
