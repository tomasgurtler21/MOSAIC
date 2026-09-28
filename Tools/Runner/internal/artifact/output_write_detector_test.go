package artifact_test

// Tests for the filesystem OutputWriteDetector: which declared outputs an
// invocation created or modified, judged by content, with wildcard entries
// expanded against the filesystem when the baseline is taken and again when
// the written set is computed.

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

const detRunDir = "Orchestration-20260928T103906Z-d408"

// detWrite writes content to root/rel, creating parent directories.
func detWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func detQuery(root string, declared ...string) domain.OutputQuery {
	return domain.OutputQuery{Root: root, Declared: declared}
}

func TestOutputWriteDetector_DeclaredButNeverWritten_NotReported(t *testing.T) {
	root := t.TempDir()
	d := artifact.NewOutputWriteDetector()
	ctx := context.Background()

	base := d.Baseline(ctx, detQuery(root, detRunDir+"/Design.md"))
	got := d.Written(ctx, base)

	if len(got) != 0 {
		t.Errorf("declared output that was never written: want nothing reported, got %v", got)
	}
}

func TestOutputWriteDetector_CreatedDuringInvocation_Reported(t *testing.T) {
	root := t.TempDir()
	d := artifact.NewOutputWriteDetector()
	ctx := context.Background()
	path := detRunDir + "/Design.md"

	base := d.Baseline(ctx, detQuery(root, path))
	detWrite(t, root, path, "---\nhuman_approved: false\n---\n# Design\n")
	got := d.Written(ctx, base)

	if !reflect.DeepEqual(got, []string{path}) {
		t.Errorf("created output: want [%s], got %v", path, got)
	}
}

func TestOutputWriteDetector_ModifiedDuringInvocation_Reported(t *testing.T) {
	root := t.TempDir()
	d := artifact.NewOutputWriteDetector()
	ctx := context.Background()
	path := detRunDir + "/Design.md"
	detWrite(t, root, path, "version one\n")

	base := d.Baseline(ctx, detQuery(root, path))
	detWrite(t, root, path, "version two\n")
	got := d.Written(ctx, base)

	if !reflect.DeepEqual(got, []string{path}) {
		t.Errorf("modified output: want [%s], got %v", path, got)
	}
}

func TestOutputWriteDetector_UnchangedPreExisting_NotReported(t *testing.T) {
	root := t.TempDir()
	d := artifact.NewOutputWriteDetector()
	ctx := context.Background()
	path := detRunDir + "/Design.md"
	detWrite(t, root, path, "stale\n")

	base := d.Baseline(ctx, detQuery(root, path))
	got := d.Written(ctx, base)

	if len(got) != 0 {
		t.Errorf("unchanged pre-existing output: want nothing reported, got %v", got)
	}
}

func TestOutputWriteDetector_RewrittenWithIdenticalContent_NotReported(t *testing.T) {
	// Detection is content-sensitive: a rewrite that leaves the bytes identical
	// is not a modification.
	root := t.TempDir()
	d := artifact.NewOutputWriteDetector()
	ctx := context.Background()
	path := detRunDir + "/Design.md"
	detWrite(t, root, path, "same bytes\n")

	base := d.Baseline(ctx, detQuery(root, path))
	detWrite(t, root, path, "same bytes\n")
	got := d.Written(ctx, base)

	if len(got) != 0 {
		t.Errorf("identical rewrite: want nothing reported, got %v", got)
	}
}

func TestOutputWriteDetector_ModifiedWithSameLength_Reported(t *testing.T) {
	// A change that keeps the size (and possibly the timestamp second) is still
	// detected because the comparison is by content.
	root := t.TempDir()
	d := artifact.NewOutputWriteDetector()
	ctx := context.Background()
	path := detRunDir + "/Design.md"
	detWrite(t, root, path, "human_approved: false\n")

	base := d.Baseline(ctx, detQuery(root, path))
	detWrite(t, root, path, "human_approved: true!\n")
	got := d.Written(ctx, base)

	if !reflect.DeepEqual(got, []string{path}) {
		t.Errorf("same-length modification: want [%s], got %v", path, got)
	}
}

func TestOutputWriteDetector_RemovedDuringInvocation_NotReported(t *testing.T) {
	root := t.TempDir()
	d := artifact.NewOutputWriteDetector()
	ctx := context.Background()
	path := detRunDir + "/Design.md"
	detWrite(t, root, path, "content\n")

	base := d.Baseline(ctx, detQuery(root, path))
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(path))); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got := d.Written(ctx, base)

	if len(got) != 0 {
		t.Errorf("path absent after the invocation: want nothing reported, got %v", got)
	}
}

func TestOutputWriteDetector_MixedOutputs_ReportedInDeclaredOrder(t *testing.T) {
	root := t.TempDir()
	d := artifact.NewOutputWriteDetector()
	ctx := context.Background()
	a, b, c, e := detRunDir+"/a.md", detRunDir+"/b.md", detRunDir+"/c.md", detRunDir+"/e.md"
	detWrite(t, root, b, "old b\n")
	detWrite(t, root, c, "old c\n")

	base := d.Baseline(ctx, detQuery(root, e, c, b, a))
	detWrite(t, root, a, "new a\n") // created
	detWrite(t, root, b, "new b\n") // modified
	// c unchanged, e never written
	got := d.Written(ctx, base)

	want := []string{b, a} // declared order is e, c, b, a
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mixed outputs: want %v, got %v", want, got)
	}
}

func TestOutputWriteDetector_DuplicateDeclarations_ReportedOnce(t *testing.T) {
	root := t.TempDir()
	d := artifact.NewOutputWriteDetector()
	ctx := context.Background()
	path := detRunDir + "/Design.md"

	base := d.Baseline(ctx, detQuery(root, path, path))
	detWrite(t, root, path, "x\n")
	got := d.Written(ctx, base)

	if !reflect.DeepEqual(got, []string{path}) {
		t.Errorf("duplicate declarations: want single entry [%s], got %v", path, got)
	}
}

func TestOutputWriteDetector_Wildcard_EachWrittenMatchReported_Sorted(t *testing.T) {
	root := t.TempDir()
	d := artifact.NewOutputWriteDetector()
	ctx := context.Background()
	glob := detRunDir + "/Stage-*/Plan.md"
	s1 := detRunDir + "/Stage-1/Plan.md"
	s2 := detRunDir + "/Stage-2/Plan.md"
	s3 := detRunDir + "/Stage-3/Plan.md"
	detWrite(t, root, s1, "old 1\n")
	detWrite(t, root, s2, "old 2\n")
	detWrite(t, root, s3, "old 3\n")

	base := d.Baseline(ctx, detQuery(root, glob))
	detWrite(t, root, s3, "new 3\n")
	detWrite(t, root, s1, "new 1\n")
	// Stage-2 untouched
	got := d.Written(ctx, base)

	want := []string{s1, s3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("wildcard: want only written matches %v in lexical order, got %v", want, got)
	}
}

func TestOutputWriteDetector_Wildcard_MatchAppearingDuringInvocation_Reported(t *testing.T) {
	root := t.TempDir()
	d := artifact.NewOutputWriteDetector()
	ctx := context.Background()
	glob := detRunDir + "/Stage-*/Plan.md"
	s1 := detRunDir + "/Stage-1/Plan.md"
	s2 := detRunDir + "/Stage-2/Plan.md"
	detWrite(t, root, s1, "old 1\n")

	base := d.Baseline(ctx, detQuery(root, glob))
	detWrite(t, root, s2, "brand new\n") // no such match when the baseline was taken
	got := d.Written(ctx, base)

	if !reflect.DeepEqual(got, []string{s2}) {
		t.Errorf("wildcard gaining a match: want [%s], got %v", s2, got)
	}
}

func TestOutputWriteDetector_Wildcard_NoMatchesAnywhere_NothingReported(t *testing.T) {
	root := t.TempDir()
	d := artifact.NewOutputWriteDetector()
	ctx := context.Background()

	base := d.Baseline(ctx, detQuery(root, detRunDir+"/Stage-*/Plan.md"))
	got := d.Written(ctx, base)

	if len(got) != 0 {
		t.Errorf("wildcard with no matches: want nothing reported, got %v", got)
	}
}

func TestOutputWriteDetector_WildcardAndConcreteEntries_KeepDeclaredOrder(t *testing.T) {
	root := t.TempDir()
	d := artifact.NewOutputWriteDetector()
	ctx := context.Background()
	glob := detRunDir + "/Stage-*/Plan.md"
	s1 := detRunDir + "/Stage-1/Plan.md"
	s2 := detRunDir + "/Stage-2/Plan.md"
	summary := detRunDir + "/Summary.md"

	base := d.Baseline(ctx, detQuery(root, summary, glob))
	detWrite(t, root, s2, "2\n")
	detWrite(t, root, s1, "1\n")
	detWrite(t, root, summary, "s\n")
	got := d.Written(ctx, base)

	want := []string{summary, s1, s2}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("declared order with wildcard: want %v, got %v", want, got)
	}
}

func TestOutputWriteDetector_UnreadableAfterInvocation_ReportedAsWritten(t *testing.T) {
	// A path that exists but whose content cannot be read afterwards is treated
	// as written, so the gate fails closed instead of skipping it. A directory
	// standing where the file was is an unreadable "file" on every platform.
	root := t.TempDir()
	d := artifact.NewOutputWriteDetector()
	ctx := context.Background()
	path := detRunDir + "/Design.md"

	base := d.Baseline(ctx, detQuery(root, path))
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(path)), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got := d.Written(ctx, base)

	if !reflect.DeepEqual(got, []string{path}) {
		t.Errorf("unreadable post-invocation content: want reported as written [%s], got %v", path, got)
	}
}

func TestOutputWriteDetector_BaselineReusedAcrossCalls_ReportsEverythingSinceBaseline(t *testing.T) {
	// The same baseline is used for the first attempt and a re-dispatch, so a
	// second Written call still covers what the first attempt wrote.
	root := t.TempDir()
	d := artifact.NewOutputWriteDetector()
	ctx := context.Background()
	a, b := detRunDir+"/a.md", detRunDir+"/b.md"

	base := d.Baseline(ctx, detQuery(root, a, b))
	detWrite(t, root, a, "first attempt\n")
	first := d.Written(ctx, base)
	detWrite(t, root, b, "re-dispatch\n")
	second := d.Written(ctx, base)

	if !reflect.DeepEqual(first, []string{a}) {
		t.Errorf("first Written: want [%s], got %v", a, first)
	}
	if !reflect.DeepEqual(second, []string{a, b}) {
		t.Errorf("second Written with the same baseline: want [%s %s], got %v", a, b, second)
	}
}
