// Package-level declarations formerly in this file have been split into
// concern-specific files:
//   - lockprotocol_options.go   (Option, With*, defaultOptions, applyOptions)
//   - lockprotocol_backupstate.go (BackupState, Cleanup, probe helpers)
//   - lockprotocol_recovery.go (RecoveryCheck and helpers, validateRunID)
//   - lockprotocol_setup.go (SetupBackupAndTransform)
package lockprotocol
