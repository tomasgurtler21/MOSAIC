package pathinput_test

// unquote_test.go specifies the behaviour of Unquote with a table-driven test covering
// every input class the contract names.
//
// The test is in the _test package so it tests only the exported API.
//
// In the TDD RED phase these tests will partially fail: Unquote is a stub that returns
// its input unchanged, so every case that expects stripping fails. Cases that expect
// no change pass. Both outcomes are the correct RED state — the failing cases drive the
// implementation in I1.1.

import (
	"testing"

	"mosaic-deploy/internal/pathinput"
)

func TestUnquote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// ----------------------------------------------------------------
		// One surrounding matched pair is removed.
		// ----------------------------------------------------------------
		{
			name:  "matched outer double-quote pair is removed",
			input: `"/path/to/dir"`,
			want:  "/path/to/dir",
		},
		{
			name:  "matched pair around path with spaces is removed",
			input: `"/path/to/my workspace"`,
			want:  "/path/to/my workspace",
		},
		{
			name:  "two quote characters with nothing inside yields empty string",
			input: `""`,
			want:  "",
		},
		{
			name:  "only the outermost pair is removed from double-nested quotes",
			input: `"` + `"` + `/path/to/dir` + `"` + `"`,
			want:  `"` + `/path/to/dir` + `"`,
		},

		// ----------------------------------------------------------------
		// Every other input is returned unchanged.
		// ----------------------------------------------------------------
		{
			name:  "unquoted path is unchanged",
			input: "/path/to/dir",
			want:  "/path/to/dir",
		},
		{
			name:  "empty string is unchanged",
			input: "",
			want:  "",
		},
		{
			name:  "leading double-quote only is unchanged",
			input: `"/path/to/dir`,
			want:  `"/path/to/dir`,
		},
		{
			name:  "trailing double-quote only is unchanged",
			input: `/path/to/dir"`,
			want:  `/path/to/dir"`,
		},
		{
			name:  "single double-quote character is unchanged",
			input: `"`,
			want:  `"`,
		},
		{
			name:  "single-quoted path is unchanged",
			input: `'/path/to/dir'`,
			want:  `'/path/to/dir'`,
		},
		{
			name:  "path with interior double quotes is unchanged",
			input: `/path/"interior"/dir`,
			want:  `/path/"interior"/dir`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pathinput.Unquote(tc.input)
			if got != tc.want {
				t.Errorf("Unquote(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
