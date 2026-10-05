package deploy_test

// format_change_test.go specifies DetectFormatChange: the pure comparison of an existing
// file's bytes with the bytes about to replace it (UTF-8 BOM presence and line-ending class).

import (
	"testing"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// withBOM returns the UTF-8 BOM followed by s.
func withBOM(s string) []byte {
	return append(append([]byte{}, utf8BOM...), s...)
}

func TestDetectFormatChange_Table(t *testing.T) {
	tests := []struct {
		name     string
		existing []byte
		written  []byte
		want     *domain.FormatChange // nil means no change reported
	}{
		{
			name:     "BOM and CRLF replaced by BOM-less LF reports both",
			existing: withBOM("a\r\nb\r\n"),
			written:  []byte("a\nb\n"),
			want: &domain.FormatChange{
				BOMBefore: true, BOMAfter: false,
				LineEndingsBefore: domain.LineEndingCRLF, LineEndingsAfter: domain.LineEndingLF,
			},
		},
		{
			name:     "BOM-only difference (BOM removed)",
			existing: withBOM("a\nb\n"),
			written:  []byte("a\nb\n"),
			want: &domain.FormatChange{
				BOMBefore: true, BOMAfter: false,
				LineEndingsBefore: domain.LineEndingLF, LineEndingsAfter: domain.LineEndingLF,
			},
		},
		{
			name:     "BOM-only difference (BOM added)",
			existing: []byte("a\nb\n"),
			written:  withBOM("a\nb\n"),
			want: &domain.FormatChange{
				BOMBefore: false, BOMAfter: true,
				LineEndingsBefore: domain.LineEndingLF, LineEndingsAfter: domain.LineEndingLF,
			},
		},
		{
			name:     "line-ending-only difference CRLF to LF",
			existing: []byte("a\r\nb\r\n"),
			written:  []byte("a\nb\n"),
			want: &domain.FormatChange{
				LineEndingsBefore: domain.LineEndingCRLF, LineEndingsAfter: domain.LineEndingLF,
			},
		},
		{
			name:     "line-ending-only difference LF to CRLF",
			existing: []byte("a\nb\n"),
			written:  []byte("a\r\nb\r\n"),
			want: &domain.FormatChange{
				LineEndingsBefore: domain.LineEndingLF, LineEndingsAfter: domain.LineEndingCRLF,
			},
		},
		{
			name:     "one CRLF among many LF is mixed, differs from pure CRLF",
			existing: []byte("a\nb\nc\r\nd\n"),
			written:  []byte("a\r\nb\r\n"),
			want: &domain.FormatChange{
				LineEndingsBefore: domain.LineEndingMixed, LineEndingsAfter: domain.LineEndingCRLF,
			},
		},
		{
			name:     "identical formatting, different text reports nothing",
			existing: []byte("a\nb\n"),
			written:  []byte("x\ny\nz\n"),
			want:     nil,
		},
		{
			name:     "identical BOM and CRLF reports nothing",
			existing: withBOM("a\r\n"),
			written:  withBOM("b\r\nc\r\n"),
			want:     nil,
		},
		{
			name:     "nil existing and BOM-less LF written reports nothing",
			existing: nil,
			written:  []byte("a\nb\n"),
			want:     nil,
		},
		{
			name:     "empty existing and BOM-less content reports nothing",
			existing: []byte{},
			written:  []byte("a\r\nb\r\n"),
			want:     nil,
		},
		{
			name:     "empty existing and BOM written reports BOM added only",
			existing: []byte{},
			written:  withBOM("a\n"),
			want: &domain.FormatChange{
				BOMBefore: false, BOMAfter: true,
				LineEndingsBefore: domain.LineEndingNone, LineEndingsAfter: domain.LineEndingLF,
			},
		},
		{
			name:     "lone CR is not a terminator, class none on that side",
			existing: []byte("a\rb"),
			written:  []byte("a\nb\n"),
			want:     nil,
		},
		{
			name:     "a CR b LF is classified lf, matches LF written",
			existing: []byte("a\rb\n"),
			written:  []byte("x\n"),
			want:     nil,
		},
		{
			name:     "written without terminators never reports a line-ending change",
			existing: []byte("a\r\nb\r\n"),
			written:  []byte("single line"),
			want:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deploy.DetectFormatChange(tc.existing, tc.written)

			if tc.want == nil {
				if got != nil {
					t.Fatalf("DetectFormatChange = %+v; want nil", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("DetectFormatChange = nil; want %+v", *tc.want)
			}
			if *got != *tc.want {
				t.Errorf("DetectFormatChange = %+v; want %+v", *got, *tc.want)
			}
		})
	}
}

// TestFormatChange_StringAndPredicates pins the documented rendering of a FormatChange.
func TestFormatChange_StringAndPredicates(t *testing.T) {
	tests := []struct {
		name         string
		fc           domain.FormatChange
		wantString   string
		wantBOM      bool
		wantLineEnds bool
	}{
		{
			name: "BOM removed and CRLF to LF",
			fc: domain.FormatChange{BOMBefore: true,
				LineEndingsBefore: domain.LineEndingCRLF, LineEndingsAfter: domain.LineEndingLF},
			wantString: "BOM removed; line endings CRLF -> LF", wantBOM: true, wantLineEnds: true,
		},
		{
			name: "BOM added only",
			fc: domain.FormatChange{BOMAfter: true,
				LineEndingsBefore: domain.LineEndingLF, LineEndingsAfter: domain.LineEndingLF},
			wantString: "BOM added", wantBOM: true, wantLineEnds: false,
		},
		{
			name: "mixed to CRLF",
			fc: domain.FormatChange{
				LineEndingsBefore: domain.LineEndingMixed, LineEndingsAfter: domain.LineEndingCRLF},
			wantString: "line endings mixed -> CRLF", wantBOM: false, wantLineEnds: true,
		},
		{
			name: "none side never counts as a line-ending change",
			fc: domain.FormatChange{BOMAfter: true,
				LineEndingsBefore: domain.LineEndingNone, LineEndingsAfter: domain.LineEndingLF},
			wantString: "BOM added", wantBOM: true, wantLineEnds: false,
		},
		{
			name: "nothing changed renders empty",
			fc: domain.FormatChange{
				LineEndingsBefore: domain.LineEndingLF, LineEndingsAfter: domain.LineEndingLF},
			wantString: "", wantBOM: false, wantLineEnds: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fc.String(); got != tc.wantString {
				t.Errorf("String() = %q; want %q", got, tc.wantString)
			}
			if got := tc.fc.BOMChanged(); got != tc.wantBOM {
				t.Errorf("BOMChanged() = %v; want %v", got, tc.wantBOM)
			}
			if got := tc.fc.LineEndingsChanged(); got != tc.wantLineEnds {
				t.Errorf("LineEndingsChanged() = %v; want %v", got, tc.wantLineEnds)
			}
		})
	}
}
