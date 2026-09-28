package session

// maxConsecutiveSameAgentDispatches is the upper bound on consecutive
// dispatches of the same agent for the same workflow step. When the count
// reaches this value, the next would-be dispatch is blocked and the session
// escalates to the routing consultant instead.
const maxConsecutiveSameAgentDispatches = 4

// antiLoopState tracks consecutive same-agent dispatches for the current
// workflow step. All fields are updated by recordDispatch. The zero value is
// safe to use: rowIndex 0 is a valid row index so callers must initialize
// rowIndex to -1 to indicate "no step tracked yet".
type antiLoopState struct {
	rowIndex  int    // -1 = no step tracked yet
	count     int    // consecutive same-agent dispatch count for current step
	lastAgent string // identifier of the last dispatched agent
}

// recordDispatch updates the anti-loop state for a dispatch of agentID at
// rowIndex and reports whether the dispatch is allowed. A dispatch is blocked
// (returns false) only when the same agent at the same row has already been
// dispatched maxConsecutiveSameAgentDispatches times. Any change in row index
// or agent identifier resets the counter and always permits the dispatch.
func (a *antiLoopState) recordDispatch(rowIndex int, agentID string) bool {
	if rowIndex != a.rowIndex || agentID != a.lastAgent {
		// New step or different agent: reset counter.
		a.rowIndex = rowIndex
		a.lastAgent = agentID
		a.count = 1
		return true
	}
	if a.count >= maxConsecutiveSameAgentDispatches {
		return false // 5th (or more) consecutive same-agent dispatch for this step
	}
	a.count++
	return true
}
