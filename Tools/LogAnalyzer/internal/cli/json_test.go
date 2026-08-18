package cli_test

// Tests for the JSON encoders: EncodeRunTotal (basic total-per-run stable
// contract) and EncodeReport (full report JSON).
//
// Coverage:
//   - The basic total-per-run shape contains all committed fields
//   - Output is valid, parseable JSON
//   - Tokens and money are present for a single run
//   - Absent token categories encode as JSON null (not 0)
//   - Present-zero token categories encode as 0 (not null)
//   - Unpriced money encodes as state="unpriced" with no amount (not state="known", amount="0")
//   - Known-zero money encodes as state="known", amount="0" (not state="unpriced")
//   - Full report includes per-run, per-actor, per-category figures
//   - Full report includes the data-quality summary
//   - Full report represents provisional run flags
//   - Unattributable bucket appears as a sibling of runs, not inside runs

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/cli"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// EncodeRunTotal: field presence and validity
// ---------------------------------------------------------------------------

func TestEncodeRunTotal_ValidJSON(t *testing.T) {
	total := app.RunTotal{
		RunID:       cliTestRunID,
		Provisional: false,
		Tokens: domain.TokenUsage{
			Input:  domain.Tokens(1000),
			Output: domain.Tokens(500),
		},
		Money:    domain.KnownMoney(domain.Money(1_230_000_000)), // $1.23
		Complete: true,
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
}

func TestEncodeRunTotal_ContainsAllCommittedFields(t *testing.T) {
	total := app.RunTotal{
		RunID:       cliTestRunID,
		Provisional: false,
		Tokens: domain.TokenUsage{
			Input:  domain.Tokens(1000),
			Output: domain.Tokens(500),
		},
		Money:    domain.KnownMoney(domain.Money(1_230_000_000)),
		Complete: true,
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	required := []string{
		"schema_version",
		"run_id",
		"provisional",
		"currency",
		"tokens",
		"money",
		"complete",
	}
	for _, field := range required {
		if _, ok := m[field]; !ok {
			t.Errorf("required field %q is absent from EncodeRunTotal output", field)
		}
	}
}

func TestEncodeRunTotal_SchemaVersionIsOne(t *testing.T) {
	total := app.RunTotal{RunID: cliTestRunID}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if got, _ := m["schema_version"].(string); got != "1" {
		t.Errorf("schema_version = %q, want %q", got, "1")
	}
}

func TestEncodeRunTotal_CurrencyIsUSD(t *testing.T) {
	total := app.RunTotal{RunID: cliTestRunID}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if got, _ := m["currency"].(string); got != domain.Currency {
		t.Errorf("currency = %q, want %q", got, domain.Currency)
	}
}

func TestEncodeRunTotal_RunIDAndProvisionalPresent(t *testing.T) {
	total := app.RunTotal{
		RunID:       cliTestRunID,
		Provisional: true,
		Money:       domain.NoMoneyData(),
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if got, _ := m["run_id"].(string); got != cliTestRunID {
		t.Errorf("run_id = %q, want %q", got, cliTestRunID)
	}
	if got, _ := m["provisional"].(bool); !got {
		t.Errorf("provisional = %v, want true", got)
	}
}

func TestEncodeRunTotal_TokensIncludedForSingleRun(t *testing.T) {
	total := app.RunTotal{
		RunID: cliTestRunID,
		Tokens: domain.TokenUsage{
			Input:  domain.Tokens(12345),
			Output: domain.Tokens(6789),
		},
		Money: domain.NoMoneyData(),
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	tokens, ok := m["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("tokens field is missing or not an object")
	}

	inputVal, hasInput := tokens["input"]
	if !hasInput {
		t.Errorf("tokens.input is absent")
	}
	if inputVal == nil {
		t.Errorf("tokens.input is null for a present value, want a number")
	}

	outputVal, hasOutput := tokens["output"]
	if !hasOutput {
		t.Errorf("tokens.output is absent")
	}
	if outputVal == nil {
		t.Errorf("tokens.output is null for a present value, want a number")
	}
}

func TestEncodeRunTotal_MoneyIncludedForSingleRun(t *testing.T) {
	total := app.RunTotal{
		RunID:    cliTestRunID,
		Tokens:   domain.TokenUsage{Input: domain.Tokens(1000)},
		Money:    domain.KnownMoney(domain.Money(3_000_000_000)), // $3.00
		Complete: true,
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	money, ok := m["money"].(map[string]any)
	if !ok {
		t.Fatalf("money field is missing or not an object")
	}

	if state, _ := money["state"].(string); state != "known" {
		t.Errorf("money.state = %q, want %q", state, "known")
	}
	if _, hasAmount := money["amount"]; !hasAmount {
		t.Errorf("money.amount is absent for a known money value")
	}
}

// ---------------------------------------------------------------------------
// EncodeRunTotal: absent-vs-zero token encoding (T9.3)
// ---------------------------------------------------------------------------

func TestEncodeRunTotal_AbsentTokenCategoryEncodesAsNull(t *testing.T) {
	// cache_read and cache_creation are absent; they must encode as null, not 0.
	total := app.RunTotal{
		RunID: cliTestRunID,
		Tokens: domain.TokenUsage{
			Input:         domain.Tokens(1000),
			CacheRead:     domain.AbsentTokens(), // absent
			CacheCreation: domain.AbsentTokens(), // absent
			Output:        domain.Tokens(500),
		},
		Money: domain.NoMoneyData(),
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	tokens, ok := m["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("tokens field missing or not an object")
	}

	cacheRead, hasCacheRead := tokens["cache_read"]
	if !hasCacheRead {
		t.Errorf("tokens.cache_read is absent; want it present as null")
	} else if cacheRead != nil {
		t.Errorf("tokens.cache_read = %v, want null for an absent token category", cacheRead)
	}

	cacheCreation, hasCacheCreation := tokens["cache_creation"]
	if !hasCacheCreation {
		t.Errorf("tokens.cache_creation is absent; want it present as null")
	} else if cacheCreation != nil {
		t.Errorf("tokens.cache_creation = %v, want null for an absent token category", cacheCreation)
	}
}

func TestEncodeRunTotal_PresentZeroTokenEncodesAsZero(t *testing.T) {
	// cache_creation is present with a value of zero; it must encode as 0, not null.
	total := app.RunTotal{
		RunID: cliTestRunID,
		Tokens: domain.TokenUsage{
			Input:         domain.Tokens(1000),
			CacheCreation: domain.Tokens(0), // present zero
			Output:        domain.Tokens(500),
		},
		Money: domain.NoMoneyData(),
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	tokens, ok := m["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("tokens field missing or not an object")
	}

	cacheCreation, hasCacheCreation := tokens["cache_creation"]
	if !hasCacheCreation {
		t.Errorf("tokens.cache_creation is absent; want it present as 0")
	} else if cacheCreation == nil {
		t.Errorf("tokens.cache_creation is null for a present-zero category, want 0")
	} else if num, ok := cacheCreation.(float64); !ok || num != 0 {
		t.Errorf("tokens.cache_creation = %v, want 0", cacheCreation)
	}
}

// ---------------------------------------------------------------------------
// EncodeRunTotal: money state encoding (T9.3)
// ---------------------------------------------------------------------------

func TestEncodeRunTotal_UnpricedMoneyEncodesAsUnpricedState(t *testing.T) {
	// Unpriced money must not encode as state="known", amount="0".
	total := app.RunTotal{
		RunID:    cliTestRunID,
		Tokens:   domain.TokenUsage{Input: domain.Tokens(1000)},
		Money:    domain.UnpricedMoney(),
		Complete: false,
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	money, ok := m["money"].(map[string]any)
	if !ok {
		t.Fatalf("money field missing or not an object")
	}

	state, _ := money["state"].(string)
	if state != "unpriced" {
		t.Errorf("money.state = %q, want %q for unpriced money", state, "unpriced")
	}
	if _, hasAmount := money["amount"]; hasAmount {
		t.Errorf("money.amount must be absent when state is unpriced")
	}
}

func TestEncodeRunTotal_KnownZeroMoneyDistinctFromUnpriced(t *testing.T) {
	// A known $0 total must encode as state="known", amount="0", not state="unpriced".
	total := app.RunTotal{
		RunID:    cliTestRunID,
		Money:    domain.KnownMoney(domain.Money(0)),
		Complete: true,
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	money, ok := m["money"].(map[string]any)
	if !ok {
		t.Fatalf("money field missing or not an object")
	}

	state, _ := money["state"].(string)
	if state != "known" {
		t.Errorf("money.state = %q, want %q for a known-zero amount", state, "known")
	}
	amount, hasAmount := money["amount"]
	if !hasAmount {
		t.Errorf("money.amount must be present when state is known")
	} else if amount == nil {
		t.Errorf("money.amount is null for known-zero, want a string amount")
	}
}

func TestEncodeRunTotal_NoDataMoneyEncodesAsNoDataState(t *testing.T) {
	total := app.RunTotal{
		RunID: cliTestRunID,
		Money: domain.NoMoneyData(),
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	money, ok := m["money"].(map[string]any)
	if !ok {
		t.Fatalf("money field missing or not an object")
	}

	state, _ := money["state"].(string)
	if state != "no_data" {
		t.Errorf("money.state = %q, want %q for no-data money", state, "no_data")
	}
}

func TestEncodeRunTotal_MoneyAmountIsStringNotNumber(t *testing.T) {
	// money.amount must be a JSON string, never a JSON number, to preserve exact
	// decimal representation (the stable contract says "never a JSON number").
	total := app.RunTotal{
		RunID:    cliTestRunID,
		Money:    domain.KnownMoney(domain.Money(1_230_000_000)), // $1.23
		Complete: true,
	}

	var buf bytes.Buffer
	if err := cli.EncodeRunTotal(&buf, total); err != nil {
		t.Fatalf("EncodeRunTotal returned error: %v", err)
	}

	// Check the raw output to confirm money.amount is a JSON string.
	output := buf.String()
	if !strings.Contains(output, `"amount"`) {
		t.Errorf("output does not contain money.amount field; output: %s", output)
	}
	// A JSON number would appear as a bare numeric token; a string as "...".
	// We verify that the amount value is quoted.
	if !strings.Contains(output, `"amount":"`) && !strings.Contains(output, `"amount": "`) {
		t.Errorf("money.amount appears to be a JSON number rather than a quoted string; output: %s", output)
	}
}

// ---------------------------------------------------------------------------
// EncodeReport: field presence
// ---------------------------------------------------------------------------

func TestEncodeReport_ValidJSON(t *testing.T) {
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
	}

	var buf bytes.Buffer
	if err := cli.EncodeReport(&buf, report); err != nil {
		t.Fatalf("EncodeReport returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
}

func TestEncodeReport_ContainsRequiredTopLevelFields(t *testing.T) {
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
	}

	var buf bytes.Buffer
	if err := cli.EncodeReport(&buf, report); err != nil {
		t.Fatalf("EncodeReport returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	required := []string{
		"schema_version",
		"currency",
		"source",
		"all_runs",
		"runs",
		"unpriced_models",
		"quality",
	}
	for _, field := range required {
		if _, ok := m[field]; !ok {
			t.Errorf("required field %q is absent from EncodeReport output", field)
		}
	}
}

func TestEncodeReport_PerRunFiguresPresent(t *testing.T) {
	// A report with one named run must include that run's data under "runs".
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{
				Run:         domain.NamedRun(cliTestRunID),
				Provisional: false,
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeReport(&buf, report); err != nil {
		t.Fatalf("EncodeReport returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	runs, ok := m["runs"].([]any)
	if !ok {
		t.Fatalf("runs field missing or not an array")
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(runs))
	}

	run, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatalf("runs[0] is not an object")
	}
	if id, _ := run["run_id"].(string); id != cliTestRunID {
		t.Errorf("runs[0].run_id = %q, want %q", id, cliTestRunID)
	}
	if _, hasTotals := run["totals"]; !hasTotals {
		t.Errorf("runs[0].totals is absent")
	}
}

func TestEncodeReport_PerAgentInstanceFiguresPresent(t *testing.T) {
	const agentID = domain.AgentInstanceID("Implementer#1")

	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{
				Run: domain.NamedRun(cliTestRunID),
				Agents: []domain.ActorReport{
					{
						Actor: domain.AgentInstance(agentID),
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeReport(&buf, report); err != nil {
		t.Fatalf("EncodeReport returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	runs, _ := m["runs"].([]any)
	if len(runs) == 0 {
		t.Fatalf("runs array is empty")
	}
	run, _ := runs[0].(map[string]any)
	agents, ok := run["agents"].([]any)
	if !ok {
		t.Fatalf("runs[0].agents missing or not an array")
	}
	if len(agents) == 0 {
		t.Errorf("runs[0].agents is empty; want agent-instance figures")
	}
}

func TestEncodeReport_TokensAndMoneyPresentSideBySideInTotals(t *testing.T) {
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{
				Run: domain.NamedRun(cliTestRunID),
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{Input: domain.Tokens(100)},
					Money: domain.CategoryMoney{
						Input:    domain.KnownMoney(domain.Money(300_000_000)),
						Complete: true,
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeReport(&buf, report); err != nil {
		t.Fatalf("EncodeReport returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	runs, _ := m["runs"].([]any)
	if len(runs) == 0 {
		t.Fatalf("runs array is empty")
	}
	run, _ := runs[0].(map[string]any)
	totals, ok := run["totals"].(map[string]any)
	if !ok {
		t.Fatalf("runs[0].totals missing or not an object")
	}
	if _, hasTokens := totals["tokens"]; !hasTokens {
		t.Errorf("runs[0].totals.tokens is absent")
	}
	if _, hasMoney := totals["money"]; !hasMoney {
		t.Errorf("runs[0].totals.money is absent")
	}
}

func TestEncodeReport_DataQualitySummaryPresent(t *testing.T) {
	finding := domain.Finding{
		Kind:     domain.FindingMalformedLine,
		Severity: domain.SeverityWarning,
		Path:     "/logs/run/events.jsonl",
		Line:     42,
		Run:      domain.NamedRun(cliTestRunID),
		Detail:   "invalid JSON",
	}
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Quality:  domain.NewQualitySummary([]domain.Finding{finding}),
	}

	var buf bytes.Buffer
	if err := cli.EncodeReport(&buf, report); err != nil {
		t.Fatalf("EncodeReport returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	quality, ok := m["quality"].(map[string]any)
	if !ok {
		t.Fatalf("quality field missing or not an object")
	}
	if _, hasIncomplete := quality["incomplete"]; !hasIncomplete {
		t.Errorf("quality.incomplete field is absent")
	}
	findings, ok := quality["findings"].([]any)
	if !ok {
		t.Errorf("quality.findings missing or not an array")
	} else if len(findings) == 0 {
		t.Errorf("quality.findings is empty; want the malformed_line finding")
	}
}

func TestEncodeReport_ProvisionalFlagRepresented(t *testing.T) {
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{
				Run:         domain.NamedRun(cliTestRunID),
				Provisional: true,
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeReport(&buf, report); err != nil {
		t.Fatalf("EncodeReport returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	runs, _ := m["runs"].([]any)
	if len(runs) == 0 {
		t.Fatalf("runs array is empty")
	}
	run, _ := runs[0].(map[string]any)
	provisional, ok := run["provisional"].(bool)
	if !ok {
		t.Fatalf("runs[0].provisional missing or not a bool")
	}
	if !provisional {
		t.Errorf("runs[0].provisional = false, want true")
	}
}

func TestEncodeReport_UnattributableBucketIsSiblingOfRuns(t *testing.T) {
	// The unattributable bucket must appear as "unattributable" at the top level,
	// never inside the "runs" array. Its totals must not be folded into "all_runs".
	unattributableRun := &domain.RunReport{
		Run: domain.UnattributableRun(),
	}
	report := domain.Report{
		Source:         domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency:       domain.Currency,
		Runs:           []domain.RunReport{},
		Unattributable: unattributableRun,
	}

	var buf bytes.Buffer
	if err := cli.EncodeReport(&buf, report); err != nil {
		t.Fatalf("EncodeReport returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	// unattributable must be a top-level sibling field.
	if _, hasUnattributable := m["unattributable"]; !hasUnattributable {
		t.Errorf("unattributable field is absent from top-level JSON; it must be a sibling of runs")
	}

	// runs must not include the unattributable bucket.
	runs, ok := m["runs"].([]any)
	if !ok {
		t.Fatalf("runs field missing or not an array")
	}
	if len(runs) != 0 {
		t.Errorf("runs array has %d elements; the unattributable bucket must not appear inside runs", len(runs))
	}
}

// ---------------------------------------------------------------------------
// EncodeReport: money-state encoding in the full report (mirrors EncodeRunTotal)
// ---------------------------------------------------------------------------

func TestEncodeReport_UnpricedMoneyInAllRunsTotalsEncodesAsUnpricedState(t *testing.T) {
	// The absent/unpriced encoding rules apply identically to the full report's
	// all_runs.money cells. An unpriced money cell must encode as state="unpriced"
	// with no amount — never state="known", amount="0". This mirrors
	// TestEncodeRunTotal_UnpricedMoneyEncodesAsUnpricedState at the full-report level.
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		AllRuns: domain.Totals{
			Tokens: domain.TokenUsage{Input: domain.Tokens(500)},
			Money: domain.CategoryMoney{
				Input:    domain.UnpricedMoney(),
				Complete: false,
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeReport(&buf, report); err != nil {
		t.Fatalf("EncodeReport returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	allRuns, ok := m["all_runs"].(map[string]any)
	if !ok {
		t.Fatalf("all_runs field missing or not an object")
	}
	money, ok := allRuns["money"].(map[string]any)
	if !ok {
		t.Fatalf("all_runs.money missing or not an object")
	}
	inputMoney, ok := money["input"].(map[string]any)
	if !ok {
		t.Fatalf("all_runs.money.input missing or not an object")
	}
	state, _ := inputMoney["state"].(string)
	if state != "unpriced" {
		t.Errorf("all_runs.money.input.state = %q, want %q for unpriced money in full report", state, "unpriced")
	}
	if _, hasAmount := inputMoney["amount"]; hasAmount {
		t.Errorf("all_runs.money.input.amount must be absent when state is unpriced")
	}
}

func TestEncodeReport_KnownZeroMoneyInAllRunsTotalsDistinctFromUnpriced(t *testing.T) {
	// A known $0 money cell in the full report must encode as state="known",
	// amount="0" — never state="unpriced". This mirrors
	// TestEncodeRunTotal_KnownZeroMoneyDistinctFromUnpriced at the full-report level.
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		AllRuns: domain.Totals{
			Money: domain.CategoryMoney{
				Input:    domain.KnownMoney(domain.Money(0)),
				Complete: true,
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeReport(&buf, report); err != nil {
		t.Fatalf("EncodeReport returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	allRuns, ok := m["all_runs"].(map[string]any)
	if !ok {
		t.Fatalf("all_runs field missing or not an object")
	}
	money, ok := allRuns["money"].(map[string]any)
	if !ok {
		t.Fatalf("all_runs.money missing or not an object")
	}
	inputMoney, ok := money["input"].(map[string]any)
	if !ok {
		t.Fatalf("all_runs.money.input missing or not an object")
	}
	state, _ := inputMoney["state"].(string)
	if state != "known" {
		t.Errorf("all_runs.money.input.state = %q, want %q for known-zero money in full report", state, "known")
	}
	amount, hasAmount := inputMoney["amount"]
	if !hasAmount {
		t.Errorf("all_runs.money.input.amount must be present when state is known")
	} else if amount == nil {
		t.Errorf("all_runs.money.input.amount is null for known-zero, want a string amount")
	}
}

func TestEncodeReport_NoDataMoneyInAllRunsTotalsEncodesAsNoDataState(t *testing.T) {
	// A no-data money cell in the full report must encode as state="no_data".
	// This mirrors TestEncodeRunTotal_NoDataMoneyEncodesAsNoDataState at the
	// full-report level.
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		AllRuns: domain.Totals{
			Money: domain.CategoryMoney{
				Input: domain.NoMoneyData(),
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeReport(&buf, report); err != nil {
		t.Fatalf("EncodeReport returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	allRuns, ok := m["all_runs"].(map[string]any)
	if !ok {
		t.Fatalf("all_runs field missing or not an object")
	}
	money, ok := allRuns["money"].(map[string]any)
	if !ok {
		t.Fatalf("all_runs.money missing or not an object")
	}
	inputMoney, ok := money["input"].(map[string]any)
	if !ok {
		t.Fatalf("all_runs.money.input missing or not an object")
	}
	state, _ := inputMoney["state"].(string)
	if state != "no_data" {
		t.Errorf("all_runs.money.input.state = %q, want %q for no-data money in full report", state, "no_data")
	}
}

func TestEncodeReport_AbsentTokenCategoryInFullReportEncodesAsNull(t *testing.T) {
	// The same absent-vs-zero rule applies to per-run figures in the full report.
	report := domain.Report{
		Source:   domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"},
		Currency: domain.Currency,
		Runs: []domain.RunReport{
			{
				Run: domain.NamedRun(cliTestRunID),
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{
						Input:         domain.Tokens(500),
						CacheRead:     domain.AbsentTokens(), // absent
						CacheCreation: domain.AbsentTokens(), // absent
						Output:        domain.Tokens(200),
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := cli.EncodeReport(&buf, report); err != nil {
		t.Fatalf("EncodeReport returned error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	runs, _ := m["runs"].([]any)
	if len(runs) == 0 {
		t.Fatalf("runs array is empty")
	}
	run, _ := runs[0].(map[string]any)
	totals, _ := run["totals"].(map[string]any)
	tokens, ok := totals["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("runs[0].totals.tokens missing or not an object")
	}

	cacheRead, hasCacheRead := tokens["cache_read"]
	if !hasCacheRead {
		t.Errorf("tokens.cache_read absent from full report; want null for absent category")
	} else if cacheRead != nil {
		t.Errorf("tokens.cache_read = %v in full report, want null for absent category", cacheRead)
	}
}
