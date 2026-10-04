package ghcptrust

import (
	"reflect"
	"testing"
)

func TestParseTrustedFolders_ValidShapes(t *testing.T) {
	bom := "\xEF\xBB\xBF"
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"array of strings", `{"trustedFolders": ["/a", "/b"]}`, []string{"/a", "/b"}},
		{"empty array", `{"trustedFolders": []}`, nil},
		{"key absent", `{"other": 1}`, nil},
		{"empty object", `{}`, nil},
		{"non-string elements skipped", `{"trustedFolders": ["/a", 3, null, {"x":1}, "/b"]}`, []string{"/a", "/b"}},
		{"only non-string elements", `{"trustedFolders": [1, 2]}`, nil},
		{"leading comment lines", "// User settings\n// edited by the CLI\n{\"trustedFolders\": [\"/a\"]}", []string{"/a"}},
		{"utf-8 BOM", bom + `{"trustedFolders": ["/a"]}`, []string{"/a"}},
		{"BOM then comments", bom + "// header\n{\n  \"trustedFolders\": [\"/a\"]\n}\n", []string{"/a"}},
		{"whole-line comment inside object", "{\n// note\n\"trustedFolders\": [\"/a\"]\n}", []string{"/a"}},
		{"comment with leading whitespace", "  // header\n{\"trustedFolders\": [\"/a\"]}", []string{"/a"}},
		{"windows paths", `{"trustedFolders": ["C:\\work\\repo"]}`, []string{`C:\work\repo`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTrustedFolders([]byte(tc.in))

			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if len(got) != len(tc.want) || (len(tc.want) > 0 && !reflect.DeepEqual(got, tc.want)) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseTrustedFolders_InvalidShapesReturnError(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty content", ``},
		{"only comments", "// nothing here\n"},
		{"not json", `this is not json`},
		{"truncated object", `{"trustedFolders": ["/a"`},
		{"top-level array", `["/a"]`},
		{"top-level string", `"x"`},
		{"trustedFolders is a string", `{"trustedFolders": "/a"}`},
		{"trustedFolders is an object", `{"trustedFolders": {"a": 1}}`},
		{"trustedFolders is a number", `{"trustedFolders": 3}`},
		{"trustedFolders is null", `{"trustedFolders": null}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTrustedFolders([]byte(tc.in))

			if err == nil {
				t.Fatalf("err = nil, want an error (got %v)", got)
			}
			if got != nil {
				t.Fatalf("got %v, want nil on error", got)
			}
		})
	}
}
