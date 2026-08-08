package harness_test

// Tests for ResolveExecutable: executable resolution including the Windows
// .cmd/.bat shim path, ported and adapted from mosaic-run/internal/harness's
// buildCommand coverage (there implicit; here a directly testable, exported
// step per the design's requirement that resolution stay separate from the
// spawn call).

import (
	"runtime"
	"testing"

	"mosaic-common/harness"
)

func TestResolveExecutable_PlainExecutable_NoPrefixArgs(t *testing.T) {
	cmd, err := harness.ResolveExecutable("claude")
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if cmd.Path != "claude" {
		t.Errorf("want Path=%q, got %q", "claude", cmd.Path)
	}
	if len(cmd.PrefixArgs) != 0 {
		t.Errorf("want no PrefixArgs for a plain executable, got %v", cmd.PrefixArgs)
	}
}

func TestResolveExecutable_WindowsCmdShim_WrappedWithCmdSlashC(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only shim behaviour")
	}

	cmd, err := harness.ResolveExecutable(`C:\npm\claude.cmd`)
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if cmd.Path != "cmd" {
		t.Errorf("want Path=%q (the cmd.exe wrapper), got %q", "cmd", cmd.Path)
	}
	if len(cmd.PrefixArgs) < 2 || cmd.PrefixArgs[0] != "/c" || cmd.PrefixArgs[1] != `C:\npm\claude.cmd` {
		t.Errorf("want PrefixArgs=[/c <original-path>], got %v", cmd.PrefixArgs)
	}
}

func TestResolveExecutable_WindowsBatShim_WrappedWithCmdSlashC(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only shim behaviour")
	}

	cmd, err := harness.ResolveExecutable(`C:\npm\claude.bat`)
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if cmd.Path != "cmd" {
		t.Errorf("want Path=%q (the cmd.exe wrapper), got %q", "cmd", cmd.Path)
	}
	if len(cmd.PrefixArgs) < 2 || cmd.PrefixArgs[0] != "/c" || cmd.PrefixArgs[1] != `C:\npm\claude.bat` {
		t.Errorf("want PrefixArgs=[/c <original-path>], got %v", cmd.PrefixArgs)
	}
}

func TestResolveExecutable_WindowsCmdShim_CaseInsensitiveExtension(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only shim behaviour")
	}

	cmd, err := harness.ResolveExecutable(`C:\npm\claude.CMD`)
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if cmd.Path != "cmd" {
		t.Errorf("want the .CMD (uppercase) extension recognised and wrapped, got Path=%q", cmd.Path)
	}
}

func TestResolveExecutable_WindowsPlainExe_NotWrapped(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only shim behaviour")
	}

	cmd, err := harness.ResolveExecutable(`C:\bin\claude.exe`)
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if cmd.Path != `C:\bin\claude.exe` {
		t.Errorf("want a plain .exe left unwrapped, got Path=%q PrefixArgs=%v", cmd.Path, cmd.PrefixArgs)
	}
	if len(cmd.PrefixArgs) != 0 {
		t.Errorf("want no PrefixArgs for a plain .exe, got %v", cmd.PrefixArgs)
	}
}
