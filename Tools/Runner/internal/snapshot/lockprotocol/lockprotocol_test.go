// Lock-protocol test suite. Tests have been split into behavior files:
//
//	lockprotocol_helpers_test.go       - shared test helpers and state builders
//	lockprotocol_state_test.go         - LockProtocolState construction and per-run lock lifecycle
//	lockprotocol_lastout_test.go       - last-out check: restore, teardown, Windows-safe ordering
//	lockprotocol_joiner_test.go        - joiner path: manifest-wait, setup-complete-wait, re-verify
//	lockprotocol_joiner_race_test.go   - joiner race scenarios and concurrent callers
//	lockprotocol_creator_test.go       - creator path and bounded retry dispatch loop
//	lockprotocol_recovery_test.go      - RecoveryCheck: crash recovery and .restoring serialization
//	lockprotocol_recovery_edge_test.go - RecoveryCheck edge cases: partial-backup races, temp file
//	lockprotocol_setup_test.go         - SetupBackupAndTransform and BackupState.Cleanup public API
package lockprotocol
