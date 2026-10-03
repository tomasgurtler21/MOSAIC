package engine_test

// Builders for artifact states and execution-log entries that carry the
// recorded workflow row (the Execution Log WorkflowRow column).
//
// Rows are given as the 1-based routing-table number, exactly as the
// Execution Log shows them. Phase and stage follow the format the Runner
// records: a bare "EXECUTION" phase with a group-qualified stage such as
// "Test.1" (see domain.FormatStageValue).

import (
	"mosaic-run/internal/domain"
)

// execLogEntry builds one Execution Log entry for a step that ran the given
// 1-based routing-table row.
func execLogEntry(seq int, agentID, phase, stage string, status domain.StatusCode, row int) domain.ExecutionLogEntry {
	return domain.ExecutionLogEntry{
		Seq:         seq,
		Agent:       agentID,
		Phase:       phase,
		Stage:       stage,
		WorkflowRow: domain.WorkflowRow(row),
		Status:      status,
	}
}

// stateAfterRow is stateAfter with the recorded row set on the single log
// entry: agentID (format "name#seq") just completed with the given status in
// phase/stage, running the given 1-based routing-table row.
func stateAfterRow(phase, stage, agentID string, status domain.StatusCode, seq, row int) domain.ArtifactState {
	return domain.ArtifactState{
		GlobalSequence: seq,
		CurrentState: domain.CurrentState{
			Phase:      phase,
			Stage:      stage,
			LastStatus: status,
			LastAgent:  agentID,
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			execLogEntry(seq, agentID, phase, stage, status, row),
		},
	}
}

// stateWithLog builds an ArtifactState whose log is the given entries. The
// last entry supplies the current state and GlobalSequence is set to the
// given value, so tests can model infrastructure steps that consumed
// sequence numbers without a log entry of their own.
func stateWithLog(globalSeq int, entries ...domain.ExecutionLogEntry) domain.ArtifactState {
	last := entries[len(entries)-1]
	return domain.ArtifactState{
		GlobalSequence: globalSeq,
		CurrentState: domain.CurrentState{
			Phase:      last.Phase,
			Stage:      last.Stage,
			LastStatus: last.Status,
			LastAgent:  last.Agent,
		},
		ExecutionLog: entries,
	}
}
