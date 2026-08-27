package pathutil

// normalize_test.go verifies the NormalizeInput helper in isolation.
//
// NormalizeInput is the exported function that applies the standard
// three-step input normalization rules for TUI text-entry screens:
//
//  1. Trim surrounding whitespace.
//  2. Strip one matched pair of surrounding double-quote characters (").
//  3. Filter out control characters (NUL and all runes where
//     unicode.IsControl returns true), preserving all printable characters.
//
// Single-quote stripping is deliberately omitted (double-quote-only, matching
// Deployment's pathinput.Unquote and Runner's normalizePath convention).
// These tests document that behavior explicitly.
//
// Tests are in package pathutil (not pathutil_test) following the convention
// established by Runner's normalize_path_test.go. All tests are table-driven
// for compact coverage.
//
// TDD RED: NormalizeInput is a stub that panics. This file will fail when run
// until the implementation is in place. Every assertion below is intentional
// and must pass once the implementation exists.

import "testing"

// TestNormalizeInput covers the full contract of the NormalizeInput function.
func TestNormalizeInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// --- Whitespace trimming ---

		{
			name:  "plain path without modification is returned unchanged",
			input: `/home/user/file.md`,
			want:  `/home/user/file.md`,
		},
		{
			name:  "leading whitespace is trimmed",
			input: `   /home/user/file.md`,
			want:  `/home/user/file.md`,
		},
		{
			name:  "trailing whitespace is trimmed",
			input: `/home/user/file.md   `,
			want:  `/home/user/file.md`,
		},
		{
			name:  "leading and trailing whitespace is trimmed",
			input: "  /home/user/file.md  ",
			want:  `/home/user/file.md`,
		},
		{
			name:  "whitespace-only input becomes empty string",
			input: "   ",
			want:  "",
		},
		{
			name:  "empty string stays empty",
			input: "",
			want:  "",
		},

		// --- Double-quote stripping ---

		{
			name:  "surrounding double quotes are stripped",
			input: `"/home/user/file.md"`,
			want:  `/home/user/file.md`,
		},
		{
			name:  "whitespace outside double quotes is trimmed then quotes are stripped",
			input: `  "/home/user/file.md"  `,
			want:  `/home/user/file.md`,
		},
		{
			name:  "Windows Copy-as-path style input is normalised",
			input: `"C:\Users\user\file.md"`,
			want:  `C:\Users\user\file.md`,
		},
		{
			name:  "whitespace-padded Windows path is normalised",
			input: `  "C:\Users\user\file.md"  `,
			want:  `C:\Users\user\file.md`,
		},
		{
			name:  "only the outermost matched double-quote pair is stripped",
			input: `"C:\path with \"inner\" quotes.md"`,
			want:  `C:\path with \"inner\" quotes.md`,
		},
		{
			name:  "double quotes enclosing an empty string produce an empty result",
			input: `""`,
			want:  ``,
		},

		// --- Single quotes NOT stripped ---

		{
			name:  "single-quoted path is NOT stripped -- single-quote stripping is omitted",
			input: `'/home/user/file.md'`,
			want:  `'/home/user/file.md'`,
		},
		{
			name:  "whitespace-padded single-quoted path has whitespace trimmed but quotes retained",
			input: `  '/home/user/file.md'  `,
			want:  `'/home/user/file.md'`,
		},

		// --- Mismatched / one-sided quotes ---

		{
			name:  "leading double quote without closing quote is not stripped",
			input: `"/home/user/file.md`,
			want:  `"/home/user/file.md`,
		},
		{
			name:  "trailing double quote without opening quote is not stripped",
			input: `/home/user/file.md"`,
			want:  `/home/user/file.md"`,
		},
		{
			name:  "leading single quote with closing double quote is not stripped",
			input: `'/home/user/file.md"`,
			want:  `'/home/user/file.md"`,
		},
		{
			name:  "leading double quote with closing single quote is not stripped",
			input: `"/home/user/file.md'`,
			want:  `"/home/user/file.md'`,
		},

		// --- Minimum length boundary ---

		{
			name:  "single double-quote character is too short to form a matched pair and is returned unchanged",
			input: `"`,
			want:  `"`,
		},
		{
			name:  "single non-quote character is returned unchanged",
			input: `x`,
			want:  `x`,
		},

		// --- Control-character filtering ---

		{
			name:  "NUL byte at the end is stripped",
			input: "/path\x00",
			want:  "/path",
		},
		{
			name:  "NUL byte embedded in the middle is stripped",
			input: "/pa\x00th",
			want:  "/path",
		},
		{
			name:  "NUL byte at the start is stripped",
			input: "\x00/path",
			want:  "/path",
		},
		{
			name:  "multiple control characters are all stripped",
			input: "\x00\x07/path\x1b",
			want:  "/path",
		},
		{
			name:  "tab inside path is stripped as a control character",
			input: "/pa\tth",
			want:  "/path",
		},
		{
			name:  "newline inside path is stripped as a control character",
			input: "/pa\nth",
			want:  "/path",
		},
		{
			name:  "BEL control character is stripped",
			input: "/path\x07",
			want:  "/path",
		},
		{
			name:  "ESC control character is stripped",
			input: "/path\x1b",
			want:  "/path",
		},
		{
			name:  "DEL control character (0x7f) is stripped",
			input: "/path\x7f",
			want:  "/path",
		},

		// --- Combined scenarios: whitespace + quotes + control chars ---

		{
			name:  "whitespace trimmed then quotes stripped then control chars filtered",
			input: "  \"\x00C:\\path\"  ",
			want:  `C:\path`,
		},
		{
			name:  "control chars alongside whitespace and quotes all cleaned",
			input: "  \"/pa\x00th\"  ",
			want:  "/path",
		},
		{
			name:  "NUL-only string becomes empty after filtering",
			input: "\x00",
			want:  "",
		},
		{
			name:  "all control chars with surrounding whitespace becomes empty",
			input: "  \x00\x07\x1b  ",
			want:  "",
		},

		// --- Printable characters preserved ---

		{
			name:  "printable unicode characters are preserved",
			input: "/path/\u00e9l\u00e8ve",
			want:  "/path/\u00e9l\u00e8ve",
		},
		{
			name:  "path with spaces inside is preserved",
			input: `/home/user/my documents/file.md`,
			want:  `/home/user/my documents/file.md`,
		},
		{
			name:  "path separators and punctuation are preserved",
			input: `/path/to/file-v1.2_final.md`,
			want:  `/path/to/file-v1.2_final.md`,
		},
		{
			name:  "interior double quotes are preserved after outer pair is stripped",
			input: `"/path/\"quoted\"/file"`,
			want:  `/path/\"quoted\"/file`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeInput(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeInput(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
