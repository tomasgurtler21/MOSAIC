package ghcptrust

import (
	"runtime"
	"testing"
)

func TestIsTrusted_Matching(t *testing.T) {
	cases := []struct {
		name    string
		workDir string
		entries []string
		ci      bool
		want    bool
	}{
		{"exact match", "/work/repo", []string{"/work/repo"}, false, true},
		{"ancestor entry covers descendant", "/work/repo/sub/deep", []string{"/work/repo"}, false, true},
		{"root-level ancestor", "/work/repo", []string{"/work"}, false, true},
		{"child-only entry does not cover parent", "/work", []string{"/work/repo"}, false, false},
		{"sibling entry does not cover", "/work/other", []string{"/work/repo"}, false, false},
		{"segment boundary: prefix of a longer name", "/work/foobar", []string{"/work/foo"}, false, false},
		{"segment boundary: descendant of longer name", "/work/foobar/x", []string{"/work/foo"}, false, false},
		{"trailing separator on entry", "/work/repo/sub", []string{"/work/repo/"}, false, true},
		{"trailing separator on workDir", "/work/repo/", []string{"/work/repo"}, false, true},
		{"trailing separator on both", "/work/repo/", []string{"/work/repo/"}, false, true},
		{"any entry may match", "/b/x", []string{"/a", "/b", "/c"}, false, true},
		{"no entries", "/work/repo", nil, false, false},
		{"empty entry list", "/work/repo", []string{}, false, false},
		{"case differs, case-sensitive", "/Work/Repo", []string{"/work/repo"}, false, false},
		{"case differs, case-insensitive", "/Work/Repo/Sub", []string{"/work/repo"}, true, true},
		{"case-insensitive keeps segment boundary", "/Work/FOOBAR", []string{"/work/foo"}, true, false},
		{"dot segments are cleaned", "/work/repo/../repo/sub", []string{"/work/repo"}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsTrusted(tc.workDir, tc.entries, tc.ci)

			if got != tc.want {
				t.Fatalf("IsTrusted(%q, %v, ci=%v) = %v, want %v", tc.workDir, tc.entries, tc.ci, got, tc.want)
			}
		})
	}
}

func TestIsTrusted_WindowsPathStyles(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("backslash separators are path separators on Windows only")
	}
	cases := []struct {
		name    string
		workDir string
		entries []string
		want    bool
	}{
		{"slash vs backslash", `C:\work\repo\sub`, []string{`C:/work/repo`}, true},
		{"backslash vs slash workDir", `C:/work/repo/sub`, []string{`C:\work\repo`}, true},
		{"trailing backslash on entry", `C:\work\repo\sub`, []string{`C:\work\repo\`}, true},
		{"drive letter case", `c:\work\repo`, []string{`C:\work\repo`}, true},
		{"segment boundary", `C:\foobar`, []string{`C:\foo`}, false},
		{"child-only entry", `C:\work`, []string{`C:\work\repo`}, false},
		{"different drive", `D:\work\repo`, []string{`C:\work\repo`}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsTrusted(tc.workDir, tc.entries, true)

			if got != tc.want {
				t.Fatalf("IsTrusted(%q, %v) = %v, want %v", tc.workDir, tc.entries, got, tc.want)
			}
		})
	}
}
