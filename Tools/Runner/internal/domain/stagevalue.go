package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// FormatStageValue renders the recorded stage value for a staged row:
// "{group}.{n}" when group is non-empty (for example "Test.1"), and "{n}"
// when it is empty. n must be >= 1; a zero n yields "".
func FormatStageValue(group GroupName, n StageNumber) string {
	if n <= 0 {
		return ""
	}
	if group != "" {
		return fmt.Sprintf("%s.%d", group, n)
	}
	return fmt.Sprintf("%d", n)
}

// ParseStageValue reads a recorded stage value back.
//
// Accepted forms:
//
//	"Test.1"   -> ("Test", 1, true)   -- target form, grouped
//	"1"        -> ("", 1, true)       -- target form, ungrouped
//	"Stage-1"  -> ("", 1, true)       -- legacy form written by earlier
//	                                     runner versions
//	""         -> ("", 0, false)
//
// Anything else yields ok == false. Parsing is total: it never panics and
// never returns a partially-filled result with ok == true.
func ParseStageValue(s string) (group GroupName, n StageNumber, ok bool) {
	if s == "" {
		return "", 0, false
	}

	// Legacy form: "Stage-N".
	if strings.HasPrefix(s, "Stage-") {
		numStr := strings.TrimPrefix(s, "Stage-")
		v, err := strconv.Atoi(numStr)
		if err != nil || v <= 0 {
			return "", 0, false
		}
		return "", StageNumber(v), true
	}

	// Target form: "{group}.{n}" or "{n}".
	if idx := strings.LastIndex(s, "."); idx >= 0 {
		groupPart := s[:idx]
		numPart := s[idx+1:]
		v, err := strconv.Atoi(numPart)
		if err != nil || v <= 0 || strings.TrimSpace(groupPart) == "" {
			return "", 0, false
		}
		return GroupName(groupPart), StageNumber(v), true
	}

	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return "", 0, false
	}
	return "", StageNumber(v), true
}

// RecordedPhaseName normalises a phase value read from an artifact to the
// bare phase name.
//
//	"EXECUTION"                    -> "EXECUTION"   (target form)
//	"EXECUTION.Test.[StageNumber]" -> "EXECUTION"   (legacy form)
//	"EXECUTION.[StageNumber]"      -> "EXECUTION"   (legacy form)
//	""                              -> ""
//
// It is applied on read only. What the runner writes is always already the
// bare name, taken from RoutingRow.PhaseParsed.Name.
func RecordedPhaseName(recorded string) string {
	if recorded == "" {
		return ""
	}
	if idx := strings.Index(recorded, "."); idx >= 0 {
		return recorded[:idx]
	}
	return recorded
}
