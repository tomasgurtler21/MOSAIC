package deviation_test

// Tests for the preselected answers of the row and stage questions, derived
// from the deviation that triggered the consultation.

import (
	"testing"

	"mosaic-run/internal/domain"
)

func TestManualDialogue_RowQuestion_PreselectsTheRowThatDeviated(t *testing.T) {
	cases := []struct {
		name      string
		table     domain.RoutingTable
		deviation *domain.DeviationInfo
		want      string
	}{
		{"zero-based index 11 is shown as 12", twelveRowTable(), &domain.DeviationInfo{CurrentRow: 11}, "12"},
		{"zero-based index 2 is shown as 3", stagedRoutingTable(), &domain.DeviationInfo{CurrentRow: 2}, "3"},
		{"no current row", stagedRoutingTable(), &domain.DeviationInfo{CurrentRow: -1}, ""},
		{"current row not in the table", stagedRoutingTable(), &domain.DeviationInfo{CurrentRow: 40}, ""},
		{"no deviation", stagedRoutingTable(), nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDialogue(t).standard("4", "")
			d.one[domain.QuestionManualRow] = []oneFn{pick(domain.ManualStopOptionID)}
			req := manualRequest(stagesUpTo(3))
			req.Deviation = tc.deviation

			consultManual(t, d, tc.table, req) //nolint:errcheck

			got := d.oneQ(domain.QuestionManualRow, 0).DefaultOptionID
			if got != tc.want {
				t.Errorf("want row preselection %q, got %q", tc.want, got)
			}
		})
	}
}

func TestManualDialogue_StageQuestion_PreselectsTheDeviatingStageOfTheSameGroup(t *testing.T) {
	cases := []struct {
		name         string
		currentStage string
		want         string
	}{
		{"same group, stage in the set", "Implementation.3", "3"},
		{"other group", "Test.2", ""},
		{"stage not in the set", "Implementation.9", ""},
		{"unparsable value", "garbage", ""},
		{"empty value", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDialogue(t).standard("3", "1")
			req := manualRequest(stagesUpTo(4))
			req.Deviation = &domain.DeviationInfo{CurrentRow: 2, CurrentStage: tc.currentStage}

			consultManual(t, d, stagedRoutingTable(), req) //nolint:errcheck

			got := d.oneQ(domain.QuestionManualStage, 0).DefaultOptionID
			if got != tc.want {
				t.Errorf("want stage preselection %q, got %q", tc.want, got)
			}
		})
	}
}

func TestManualDialogue_StageQuestion_OffersTheCurrentStageSetAndFreeFormEntry(t *testing.T) {
	d := newDialogue(t).standard("3", "1")

	consultStaged(t, d, 4) //nolint:errcheck

	q := d.oneQ(domain.QuestionManualStage, 0)
	want := []string{"1", "2", "3", "4"}
	got := optionIDs(q)
	if len(got) != len(want) {
		t.Fatalf("want one option per current stage %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("stage option %d: want %q, got %q", i, want[i], got[i])
		}
	}
	if !q.AllowCustom {
		t.Error("want free-form stage entry allowed")
	}
}
