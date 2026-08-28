package preflight

// TotalRuns returns the total number of runs this plan implies. Each test
// contributes its Settings.Repetitions when non-nil, or 1 otherwise. A plan
// with no tests returns 0. This is the single derivation of a run count from
// a plan; the suite runner and every frontend must use it rather than
// computing their own.
func (p Plan) TotalRuns() int {
	total := 0
	for _, rt := range p.Tests {
		reps := 1
		if rt.Settings.Repetitions != nil {
			reps = *rt.Settings.Repetitions
		}
		total += reps
	}
	return total
}
