package cli_test

// output_format_change_test.go specifies how the CLI surfaces a recorded formatting change:
// the human summary names the target path and the change text, and the JSON summary carries
// a FormatChange field on each action (an object when recorded, null otherwise).

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"mosaic-deploy/internal/cli"
	"mosaic-deploy/internal/domain"
)

const fcTargetPath = "/ws/.claude/agents/runner.md"

// changedFormat is the recorded change used by these tests; its rendering is the documented
// "BOM removed; line endings CRLF -> LF".
func changedFormat() *domain.FormatChange {
	return &domain.FormatChange{
		BOMBefore:         true,
		LineEndingsBefore: domain.LineEndingCRLF,
		LineEndingsAfter:  domain.LineEndingLF,
	}
}

func summaryWithAction(workspace string, fc *domain.FormatChange) domain.RunSummary {
	s := successSummary(workspace)
	s.Actions = []domain.ActionRecord{{
		Ref:          domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "runner"},
		TargetPath:   fcTargetPath,
		Taken:        domain.TakenUpdated,
		FormatChange: fc,
	}}
	return s
}

func runDeployHuman(t *testing.T, summary domain.RunSummary) string {
	t.Helper()
	workspace := t.TempDir()
	svc := &spyService{deployResp: summary}
	var out bytes.Buffer
	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &out, &bytes.Buffer{})
	return out.String()
}

func TestHumanSummary_Deploy_NamesPathAndChangeForFormatChange(t *testing.T) {
	out := runDeployHuman(t, summaryWithAction(t.TempDir(), changedFormat()))

	if !strings.Contains(out, fcTargetPath) {
		t.Errorf("human summary does not contain target path %q:\n%s", fcTargetPath, out)
	}
	if !strings.Contains(out, changedFormat().String()) || changedFormat().String() == "" {
		t.Errorf("human summary does not contain change text %q:\n%s", changedFormat().String(), out)
	}
}

func TestHumanSummary_Deploy_NoFormatChangeLineWhenNil(t *testing.T) {
	out := runDeployHuman(t, summaryWithAction(t.TempDir(), nil))

	if strings.Contains(out, "CRLF") || strings.Contains(out, "BOM") {
		t.Errorf("human summary mentions formatting although none was recorded:\n%s", out)
	}
}

func TestHumanSummary_Agents_NamesPathAndChangeForFormatChange(t *testing.T) {
	workspace := t.TempDir()
	s := summaryWithAction(workspace, changedFormat())
	s.Mode = domain.ModeDeployAgents
	svc := &spyService{deployAgentsResp: s}
	var out bytes.Buffer

	cli.Run(context.Background(),
		[]string{"agents", "--harness", "stub-harness", "--workspace", workspace},
		svc, &out, &bytes.Buffer{})

	if !strings.Contains(out.String(), fcTargetPath) {
		t.Errorf("agents human summary does not contain target path %q:\n%s", fcTargetPath, out.String())
	}
	if want := changedFormat().String(); want == "" || !strings.Contains(out.String(), want) {
		t.Errorf("agents human summary does not contain change text %q:\n%s", want, out.String())
	}
}

func decodeActions(t *testing.T, raw []byte) []map[string]json.RawMessage {
	t.Helper()
	var doc struct{ Actions []map[string]json.RawMessage }
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, raw)
	}
	return doc.Actions
}

func runDeployJSON(t *testing.T, summary domain.RunSummary) []byte {
	t.Helper()
	workspace := t.TempDir()
	svc := &spyService{deployResp: summary}
	var out bytes.Buffer
	cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm", "--output", "json"},
		svc, &out, &bytes.Buffer{})
	return out.Bytes()
}

func TestJSONSummary_ActionCarriesFormatChangeObject(t *testing.T) {
	actions := decodeActions(t, runDeployJSON(t, summaryWithAction(t.TempDir(), changedFormat())))

	if len(actions) != 1 {
		t.Fatalf("got %d actions; want 1", len(actions))
	}
	raw, ok := actions[0]["FormatChange"]
	if !ok {
		t.Fatalf("action JSON has no FormatChange field: %v", actions[0])
	}
	var fc domain.FormatChange
	if err := json.Unmarshal(raw, &fc); err != nil {
		t.Fatalf("FormatChange is not a FormatChange object: %v (%s)", err, raw)
	}
	if fc != *changedFormat() {
		t.Errorf("FormatChange = %+v; want %+v", fc, *changedFormat())
	}
}

func TestJSONSummary_ActionFormatChangeIsNullWhenNoneRecorded(t *testing.T) {
	actions := decodeActions(t, runDeployJSON(t, summaryWithAction(t.TempDir(), nil)))

	if len(actions) != 1 {
		t.Fatalf("got %d actions; want 1", len(actions))
	}
	raw, ok := actions[0]["FormatChange"]
	if !ok {
		t.Fatalf("action JSON has no FormatChange field; it must be present as null: %v", actions[0])
	}
	if string(raw) != "null" {
		t.Errorf("FormatChange = %s; want null", raw)
	}
}

func TestJSONSummary_ExistingActionFieldsUnchanged(t *testing.T) {
	actions := decodeActions(t, runDeployJSON(t, summaryWithAction(t.TempDir(), changedFormat())))

	for _, field := range []string{"Ref", "TargetPath", "Taken", "Stale", "BackupPath", "Err", "SourceVersion"} {
		if _, ok := actions[0][field]; !ok {
			t.Errorf("existing action field %q is missing from JSON", field)
		}
	}
}
