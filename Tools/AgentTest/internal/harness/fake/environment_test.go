package fake_test

// Tests for the scripted adapter's environment check: the fake reports a
// trivially successful environment, per Stage 3's per-adapter obligation
// table (fake: "Trivially successful").

import (
	"context"
	"testing"

	"mosaic-agent-test/internal/harness/fake"
)

// TestCheckEnvironment_TriviallySucceeds asserts the fake adapter's
// environment check never fails and never reports a problem: it exists so a
// whole-pipeline run can be driven through it with no LLM and no special
// case, and a pre-flight refusal from the scripted adapter itself would
// defeat that purpose.
func TestCheckEnvironment_TriviallySucceeds(t *testing.T) {
	a := fake.New(fake.Options{})

	report, err := a.CheckEnvironment(context.Background())
	if err != nil {
		t.Fatalf("CheckEnvironment: %v", err)
	}
	if !report.OK() {
		t.Errorf("CheckEnvironment: OK() = false, want true; Problems=%+v", report.Problems)
	}
}
