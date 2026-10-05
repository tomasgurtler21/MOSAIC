package transform_test

// workflow_rownumbers_test.go covers the Row column that a rebuild writes into each
// workflow's routing table: first-position numbering 1..N, other tables untouched,
// renumbering of an existing Row column, idempotence, line-ending preservation, blocks
// without a usable table, the preserve path staying byte-for-byte, and the Runner's table
// rules (checked on the actual output bytes with mosaic-common/mdtable).

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"mosaic-common/mdtable"
	"mosaic-deploy/internal/transform"
)

const routingBlock = `<Workflow type="core" name="build-flow" version="1.0">
## Build Flow

**Use when:** Building things.

| Phase | Subagent | HITL | On Success | On Findings |
|-------|----------|:----:|------------|-------------|
| PLANNING | planner | TRUE | next | - |
| EXECUTION.Test.[StageNumber] | test-writer | FALSE | next | - |
| EXECUTION.Test.[StageNumber] | build-review | FALSE | next | test-writer |
| FINALIZATION | summarizer | FALSE | - | - |

Closing notes after the table.
</Workflow>
`

const twoTableBlock = `<Workflow type="core" name="grouped-flow" version="2.0">
## Grouped Flow

| Phase | Subagent | HITL |
|-------|----------|:----:|
| PLANNING | planner | TRUE |
| EXECUTION.Build.[StageNumber] | implementer | FALSE |

**Execution Groups:**

| Group | Approach |
|-------|----------|
| Build | sequential |
| Test | parallel |

</Workflow>
`

const existingRowBlock = `<Workflow type="core" name="renumber-flow" version="1.0">
## Renumber

| Phase | Row | Subagent |
|-------|-----|----------|
| PLANNING | 7 | planner |
| EXECUTION.A.[StageNumber] | 7 | implementer |
| FINALIZATION | 02 | summarizer |

</Workflow>
`

const raggedBlock = `<Workflow type="core" name="ragged-flow" version="1.0">
## Ragged

| Phase | Subagent | HITL |
|-------|----------|:----:|
| PLANNING | planner |
| EXECUTION.A.[StageNumber] | implementer | FALSE | extra | more |
| FINALIZATION | summarizer | FALSE |

</Workflow>
`

const headerOnlyBlock = `<Workflow type="core" name="empty-flow" version="1.0">
## Empty

| Phase | Subagent |
|-------|----------|

</Workflow>
`

const noTableBlock = `<Workflow type="core" name="prose-flow" version="1.0">
## Prose Only

Nothing tabular here. A | pipe in text is not a table.
</Workflow>
`

// mismatchedTableBlock has a header and separator with different cell counts, which
// mdtable refuses.
const mismatchedTableBlock = `<Workflow type="core" name="broken-flow" version="1.0">
## Broken

| Phase | Subagent | HITL |
|-------|----------|
| PLANNING | planner | TRUE |

</Workflow>
`

// injectBlock rebuilds one workflow block through Apply and returns the region content.
func injectBlock(t *testing.T, id, block string) []byte {
	t.Helper()
	result := applyOrchestrator(t, orchestratorWithWorkflows, func(r *transform.Request) {
		r.Workflows = []transform.WorkflowBlock{{ID: id, Block: []byte(block)}}
	})
	return regionContent(t, result.Output, workflowRegionName)
}

// asManaged is the source block as the rebuild emits it apart from the table: only the
// opening tag type changes.
func asManaged(block string) string {
	return strings.Replace(block, `type="core"`, `type="managed"`, 1)
}

// rawCellCount counts the cells of one raw pipe line.
func rawCellCount(line string) int {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	return strings.Count(s, "|") + 1
}

// rawTableLines returns the raw lines of the first table in block (header, separator, data rows).
func rawTableLines(t *testing.T, block []byte) []string {
	t.Helper()
	_, start, end, err := mdtable.ParseAt(block, 0)
	if err != nil {
		t.Fatalf("no table in output: %v", err)
	}
	text := strings.ReplaceAll(string(block[start:end]), "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// expectedOtherColumns returns the header and normalised rows of the source routing table
// without any Row column, i.e. what every non-Row column must still hold after injection.
func expectedOtherColumns(t *testing.T, source string) ([]string, [][]string) {
	t.Helper()
	src, err := mdtable.Parse([]byte(source))
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	drop := src.Column("Row")
	strip := func(cells []string) []string {
		var out []string
		for i, c := range cells {
			if i != drop {
				out = append(out, c)
			}
		}
		return out
	}
	rows := make([][]string, len(src.Rows))
	for i, r := range src.Rows {
		rows[i] = strip(r)
	}
	return strip(src.Header), rows
}

// assertRunnerRules checks the injected block against the rules the Runner applies to a
// routing table: it is the first table mdtable returns, Row is the first header cell, all
// raw lines have equal cell counts, Row values are 1..N, and other columns are unchanged.
func assertRunnerRules(t *testing.T, source string, out []byte) {
	t.Helper()
	got, err := mdtable.Parse(out)
	if err != nil {
		t.Fatalf("mdtable.Parse of output: %v\n%s", err, out)
	}
	if len(got.Header) == 0 || got.Header[0] != "Row" {
		t.Fatalf("first header cell is not Row: %v", got.Header)
	}
	if got.Column("Row") != 0 {
		t.Errorf("Column(Row) = %d, want 0", got.Column("Row"))
	}
	wantHeader, wantRows := expectedOtherColumns(t, source)
	if !sameStrings(got.Header[1:], wantHeader) {
		t.Errorf("other headers = %v, want %v", got.Header[1:], wantHeader)
	}
	if len(got.Rows) != len(wantRows) {
		t.Fatalf("data rows = %d, want %d", len(got.Rows), len(wantRows))
	}
	for k, row := range got.Rows {
		if row[0] != strconv.Itoa(k+1) {
			t.Errorf("data row %d Row value = %q, want %q", k+1, row[0], strconv.Itoa(k+1))
		}
		if !sameStrings(row[1:], wantRows[k]) {
			t.Errorf("data row %d cells = %v, want %v", k+1, row[1:], wantRows[k])
		}
	}
	lines := rawTableLines(t, out)
	if len(lines) != len(wantRows)+2 {
		t.Fatalf("raw table lines = %d, want %d", len(lines), len(wantRows)+2)
	}
	want := len(wantHeader) + 1
	for i, line := range lines {
		if n := rawCellCount(line); n != want {
			t.Errorf("raw line %d has %d cells, want %d: %q", i, n, want, line)
		}
	}
}

// assertOutsideTableVerbatim checks that every byte outside the routing table's lines equals
// the retyped source.
func assertOutsideTableVerbatim(t *testing.T, source string, out []byte) {
	t.Helper()
	want := []byte(asManaged(source))
	_, ws, we, err := mdtable.ParseAt(want, 0)
	if err != nil {
		t.Fatalf("parse source table: %v", err)
	}
	_, gs, ge, err := mdtable.ParseAt(out, 0)
	if err != nil {
		t.Fatalf("parse output table: %v", err)
	}
	if !bytes.Equal(out[:gs], want[:ws]) {
		t.Errorf("bytes before the table changed:\ngot:  %q\nwant: %q", out[:gs], want[:ws])
	}
	if !bytes.Equal(out[ge:], want[we:]) {
		t.Errorf("bytes after the table changed:\ngot:  %q\nwant: %q", out[ge:], want[we:])
	}
}

func TestApply_RebuildInjectsRowColumnFirstNumberedFromOne(t *testing.T) {
	out := injectBlock(t, "build-flow", routingBlock)

	assertRunnerRules(t, routingBlock, out)
	assertOutsideTableVerbatim(t, routingBlock, out)
	table, _ := mdtable.Parse(out)
	if len(table.Rows) != 4 {
		t.Errorf("data rows = %d, want 4", len(table.Rows))
	}
}

func TestApply_RebuildNumbersOnlyRoutingTableWhenBlockHasTwoTables(t *testing.T) {
	out := injectBlock(t, "grouped-flow", twoTableBlock)

	assertRunnerRules(t, twoTableBlock, out)
	assertOutsideTableVerbatim(t, twoTableBlock, out)
	approach := "| Group | Approach |\n|-------|----------|\n| Build | sequential |\n| Test | parallel |\n"
	if !strings.Contains(string(out), approach) {
		t.Errorf("the second table was modified:\n%s", out)
	}
}

func TestApply_RebuildRenumbersExistingRowColumnInsteadOfDuplicating(t *testing.T) {
	out := injectBlock(t, "renumber-flow", existingRowBlock)

	assertRunnerRules(t, existingRowBlock, out)
	table, _ := mdtable.Parse(out)
	if n := strings.Count(strings.Join(table.Header, "|"), "Row"); n != 1 {
		t.Errorf("header has %d Row columns, want 1: %v", n, table.Header)
	}
}

func TestApply_RebuildRowInjectionIsIdempotent(t *testing.T) {
	cases := map[string]string{
		"plain":        routingBlock,
		"two tables":   twoTableBlock,
		"existing row": existingRowBlock,
		"ragged":       raggedBlock,
		"crlf":         toCRLF(routingBlock),
	}
	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			first := injectBlock(t, "flow", block)
			assertRunnerRules(t, block, first)

			second := injectBlock(t, "flow", string(first))

			if !bytes.Equal(first, second) {
				t.Errorf("second injection differs:\nfirst:  %q\nsecond: %q", first, second)
			}
		})
	}
}

func TestApply_RebuildPreservesCRLFLineEndings(t *testing.T) {
	block := toCRLF(twoTableBlock)

	// The source is CRLF: the output follows the source's line ending.
	result := applyOrchestrator(t, toCRLF(orchestratorWithWorkflows), func(r *transform.Request) {
		r.Workflows = []transform.WorkflowBlock{{ID: "grouped-flow", Block: []byte(block)}}
	})
	out := regionContent(t, result.Output, workflowRegionName)

	if bytes.Contains(bytes.ReplaceAll(out, []byte("\r\n"), nil), []byte("\n")) {
		t.Errorf("output contains a bare LF:\n%q", out)
	}
	if !bytes.Contains(out, []byte("|\r\n")) {
		t.Errorf("output lost its CRLF line endings: %q", out)
	}
	assertRunnerRules(t, block, out)
	assertOutsideTableVerbatim(t, block, out)
}

func TestApply_RebuildPreservesLFLineEndings(t *testing.T) {
	out := injectBlock(t, "build-flow", routingBlock)

	if bytes.Contains(out, []byte("\r")) {
		t.Errorf("output contains CR in an LF block: %q", out)
	}
	assertRunnerRules(t, routingBlock, out)
}

func TestApply_RebuildNormalisesRaggedDataRowsToHeaderWidth(t *testing.T) {
	out := injectBlock(t, "ragged-flow", raggedBlock)

	assertRunnerRules(t, raggedBlock, out)
	assertOutsideTableVerbatim(t, raggedBlock, out)
	table, _ := mdtable.Parse(out)
	if len(table.Rows) != 3 {
		t.Errorf("data rows = %d, want 3 (ragged rows are numbered like any other)", len(table.Rows))
	}
}

func TestApply_RebuildNumbersTableWithoutDataRows(t *testing.T) {
	out := injectBlock(t, "empty-flow", headerOnlyBlock)

	assertRunnerRules(t, headerOnlyBlock, out)
	assertOutsideTableVerbatim(t, headerOnlyBlock, out)
}

func TestApply_RebuildEmitsBlockWithoutTableUnchanged(t *testing.T) {
	out := injectBlock(t, "prose-flow", noTableBlock)

	if want := asManaged(noTableBlock); string(out) != want {
		t.Errorf("block without a table changed:\ngot:  %q\nwant: %q", out, want)
	}
}

func TestApply_RebuildEmitsTableMdtableRefusesUnchanged(t *testing.T) {
	out := injectBlock(t, "broken-flow", mismatchedTableBlock)

	if want := asManaged(mismatchedTableBlock); string(out) != want {
		t.Errorf("refused table changed:\ngot:  %q\nwant: %q", out, want)
	}
}

func TestApply_RebuildNumbersEachWorkflowBlockIndependently(t *testing.T) {
	result := applyOrchestrator(t, orchestratorWithWorkflows, func(r *transform.Request) {
		r.Workflows = []transform.WorkflowBlock{
			{ID: "build-flow", Block: []byte(routingBlock)},
			{ID: "grouped-flow", Block: []byte(twoTableBlock)},
		}
	})

	out := regionContent(t, result.Output, workflowRegionName)

	idx := bytes.Index(out, []byte(`<Workflow type="managed" name="grouped-flow"`))
	if idx < 0 {
		t.Fatalf("second workflow missing from output:\n%s", out)
	}
	assertRunnerRules(t, routingBlock, out[:idx])
	assertRunnerRules(t, twoTableBlock, out[idx:])
}

// deployedTableWithoutRow is a deployed workflow region whose routing table has no Row column.
const deployedTableWithoutRow = `<Workflow type="managed" name="old-flow" version="0.9">
## Old Flow

| Phase | Subagent | HITL |
|-------|----------|:----:|
| PLANNING | planner | TRUE |
| EXECUTION.A.[StageNumber] | implementer | FALSE |

</Workflow>
`

func TestApply_PreserveDeployedWorkflows_LeavesTableWithoutRowColumnUntouched(t *testing.T) {
	deployed := deployedOrchestrator("", deployedTableWithoutRow)

	result := applyOrchestrator(t, orchestratorWithWorkflows, func(r *transform.Request) {
		r.Deployed = deployed
		r.PreserveDeployedWorkflows = true
	})

	got := regionContent(t, result.Output, workflowRegionName)
	if string(got) != deployedTableWithoutRow {
		t.Errorf("preserved region changed:\ngot:  %q\nwant: %q", got, deployedTableWithoutRow)
	}
	if bytes.Contains(got, []byte("| Row")) {
		t.Error("a Row column was injected on the preserve path")
	}
}
