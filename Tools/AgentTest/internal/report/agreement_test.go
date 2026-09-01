package report_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestReportsAgree renders both the terminal report and the machine-readable
// report from the identical result model and asserts they carry the same
// per-test verdicts, the same verdict counts, the same infrastructure-
// failure count and the same total cost.
//
// This is the test that keeps the single-result-model rule honest once
// someone is tempted to add a field, or a computation, to just one
// renderer: two reports built from two traversals of the evidence
// eventually disagree about what happened, and this test is what makes that
// drift a build failure instead of a support ticket.
func TestReportsAgree(t *testing.T) {
	// Arrange
	result := fixtureFullResult()

	// Act
	text, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}
	jsonOut, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: per-test verdicts agree.
	jsonVerdicts := decodeJSONVerdicts(t, jsonOut)
	textVerdicts := parseTextVerdicts(t, text)
	if len(jsonVerdicts) == 0 {
		t.Fatalf("fixture produced no tests to compare")
	}
	for testID, jsonVerdict := range jsonVerdicts {
		textVerdict, ok := textVerdicts[testID]
		if !ok {
			t.Errorf("terminal report has no line for test %q, present in the machine-readable report as %q", testID, jsonVerdict)
			continue
		}
		if textVerdict != jsonVerdict {
			t.Errorf("test %q: terminal report says %q, machine-readable report says %q", testID, textVerdict, jsonVerdict)
		}
	}

	// Assert: verdict counts agree.
	jsonCounts := decodeJSONCounts(t, jsonOut)
	textCounts := parseTextCounts(t, text)
	if len(jsonCounts) == 0 {
		t.Fatalf("fixture produced no verdict counts to compare")
	}
	for verdict, jsonCount := range jsonCounts {
		if textCounts[verdict] != jsonCount {
			t.Errorf("verdict %q: terminal report counts %d, machine-readable report counts %d", verdict, textCounts[verdict], jsonCount)
		}
	}

	// Assert: infrastructure-failure counts agree.
	jsonInfra := decodeJSONInfrastructureFailures(t, jsonOut)
	textInfra := parseTextInfrastructureFailures(t, text)
	if jsonInfra != textInfra {
		t.Errorf("infrastructure failures: terminal report says %d, machine-readable report says %d", textInfra, jsonInfra)
	}

	// Assert: total cost agrees.
	jsonCost := decodeJSONTotalCost(t, jsonOut)
	textCost := parseTextTotalCost(t, text)
	if fmt.Sprintf("%.2f", jsonCost) != fmt.Sprintf("%.2f", textCost) {
		t.Errorf("total cost: terminal report says $%.2f, machine-readable report says $%.2f", textCost, jsonCost)
	}
}

func decodeJSONVerdicts(t *testing.T, data []byte) map[string]string {
	t.Helper()
	var doc struct {
		Tests []struct {
			TestName  string `json:"test_name"`
			Aggregate struct {
				Verdict string `json:"verdict"`
			} `json:"aggregate"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decoding machine-readable report: %v", err)
	}
	out := map[string]string{}
	for _, test := range doc.Tests {
		out[test.TestName] = test.Aggregate.Verdict
	}
	return out
}

func decodeJSONCounts(t *testing.T, data []byte) map[string]int {
	t.Helper()
	var doc struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decoding machine-readable report: %v", err)
	}
	return doc.Counts
}

func decodeJSONInfrastructureFailures(t *testing.T, data []byte) int {
	t.Helper()
	var doc struct {
		InfrastructureFailures int `json:"infrastructure_failures"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decoding machine-readable report: %v", err)
	}
	return doc.InfrastructureFailures
}

func decodeJSONTotalCost(t *testing.T, data []byte) float64 {
	t.Helper()
	var doc struct {
		TotalCost struct {
			TotalUSD float64 `json:"total_usd"`
		} `json:"total_cost"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decoding machine-readable report: %v", err)
	}
	return doc.TotalCost.TotalUSD
}

// testLinePattern matches a per-test summary line: "<test-id>: <VERDICT> ..."
var testLinePattern = regexp.MustCompile(`(?m)^(\S+): (PASS|FAIL|TIMEOUT)\b`)

func parseTextVerdicts(t *testing.T, text string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, m := range testLinePattern.FindAllStringSubmatch(text, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// totalsLinePattern matches the "Totals: FAIL=2 PASS=2 TIMEOUT=1" line.
var totalsLinePattern = regexp.MustCompile(`^Totals: (.*)$`)
var totalsEntryPattern = regexp.MustCompile(`(PASS|FAIL|TIMEOUT)=(\d+)`)

func parseTextCounts(t *testing.T, text string) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, line := range strings.Split(text, "\n") {
		m := totalsLinePattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		for _, entry := range totalsEntryPattern.FindAllStringSubmatch(m[1], -1) {
			n, err := strconv.Atoi(entry[2])
			if err != nil {
				t.Fatalf("parsing totals line %q: %v", line, err)
			}
			out[entry[1]] = n
		}
		return out
	}
	t.Fatalf("no Totals line found in terminal report:\n%s", text)
	return nil
}

var infraLinePattern = regexp.MustCompile(`^Infrastructure failures: (\d+)$`)

func parseTextInfrastructureFailures(t *testing.T, text string) int {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		m := infraLinePattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("parsing infrastructure-failures line %q: %v", line, err)
		}
		return n
	}
	t.Fatalf("no infrastructure-failures line found in terminal report:\n%s", text)
	return 0
}

var costLinePattern = regexp.MustCompile(`^Total cost: \$([0-9.]+)`)

func parseTextTotalCost(t *testing.T, text string) float64 {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		m := costLinePattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			t.Fatalf("parsing total-cost line %q: %v", line, err)
		}
		return v
	}
	t.Fatalf("no total-cost line found in terminal report:\n%s", text)
	return 0
}
