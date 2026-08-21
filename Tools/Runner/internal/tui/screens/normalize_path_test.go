package screens

// normalize_path_test.go verifies the normalizePath helper in isolation.
//
// normalizePath is the package-level helper that applies the standard path
// normalisation rules for all three path-returning TUI screens:
//  1. Trim surrounding whitespace.
//  2. Strip one matched pair of surrounding double-quote characters (").
//  3. Leave everything else intact -- interior quotes, separators, single quotes.
//
// Single-quote stripping is deliberately omitted (double-quote-only, matching
// Deployment's pathinput.Unquote convention). These tests document that change
// explicitly.
//
// Tests are in package screens (not screens_test) because normalizePath is
// unexported. All tests are table-driven for compact coverage.
//
// TDD RED: normalizePath does not yet exist. This file will cause a compile
// error until I3.1 creates the function. Every assertion below is intentional
// and must pass once the implementation is in place.

import "testing"

// TestNormalizePath covers the full contract of the normalizePath helper.
func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// --- Whitespace trimming ---

		{
			name:  "plain path without quotes is returned unchanged",
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
			name:  "whitespace outside double quotes is trimmed before stripping",
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

		// --- Single quotes NOT stripped (behavior change from normalizeOrchestratorPath) ---

		{
			name:  "single-quoted path is NOT stripped -- single-quote stripping is dropped",
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizePath(tc.input)
			if got != tc.want {
				t.Errorf("normalizePath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
