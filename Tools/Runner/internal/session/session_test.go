package session_test

// This file has been retired. All session integration tests have been split
// into focused, behavior-named files:
//
//   session_harness_fixtures_test.go      - shared fixture helpers (CLI, OpenCode, GHCP-CLI)
//   session_cliharness_snapshot_test.go   - CLI-harness copy-and-invoke snapshot integration
//   session_harness_strategy_test.go      - harness strategy selection and FR-21 cleanup
//   session_opencode_backup_test.go       - OpenCode backup-and-transform cleanup
//   session_opencode_recovery_test.go     - OpenCode recovery check ordering
//   session_antiloop_test.go              - anti-loop guard and ConsultStep phase field
//   session_hitl_rejection_test.go        - HITL rejected-dispatch persistence
//   session_consult_route_stage_test.go   - ConsultRoute stage context preservation
//   session_consult_route_seq_test.go     - ConsultRoute cross-stage deviation and Seq monotonicity
//   session_consult_route_fallback_test.go - ConsultRoute fallback artifact resolution and D4
//
// Tests added by earlier stages are in their own files; see the session package
// directory for the full list.
