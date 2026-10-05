package claudecode_test

// Tests for capturing the Claude Code CLI version reported for a run.
// The version probe is injected, so no test spawns the real CLI.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

func TestParseHarnessVersion(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   string
	}{
		{"typical output", "2.1.284 (Claude Code)\n", "2.1.284"},
		{"bare version", "2.1.284", "2.1.284"},
		{"leading blank lines", "\n\n2.1.284 (Claude Code)\n", "2.1.284"},
		{"windows line endings", "2.1.284 (Claude Code)\r\n", "2.1.284"},
		{"two components", "3.0 (Claude Code)", "3.0"},
		{"empty output", "", ""},
		{"whitespace only", "  \n\t\n", ""},
		{"non-numeric first token", "Claude Code 2.1.284\n", ""},
		{"error text", "error: unknown option '--version'\n", ""},
		{"partly numeric token", "2.1.x (Claude Code)", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got := claudecode.ParseHarnessVersion([]byte(tc.output))

			// Assert
			if got != tc.want {
				t.Errorf("ParseHarnessVersion(%q) = %q, want %q", tc.output, got, tc.want)
			}
		})
	}
}

func adapterWithProbe(probe func(ctx context.Context) ([]byte, error)) *claudecode.Adapter {
	return claudecode.New(claudecode.Options{VersionProbe: probe})
}

func TestAdapter_HarnessVersion_ReturnsParsedProbeOutput(t *testing.T) {
	// Arrange
	a := adapterWithProbe(func(ctx context.Context) ([]byte, error) {
		return []byte("2.1.284 (Claude Code)\n"), nil
	})

	// Act
	got := a.HarnessVersion(context.Background())

	// Assert
	if got != "2.1.284" {
		t.Errorf("HarnessVersion = %q, want %q", got, "2.1.284")
	}
}

func TestAdapter_HarnessVersion_UnknownWhenProbeFails(t *testing.T) {
	// Arrange
	a := adapterWithProbe(func(ctx context.Context) ([]byte, error) {
		return nil, errors.New("claude: not found")
	})

	// Act
	got := a.HarnessVersion(context.Background())

	// Assert
	if got != "" {
		t.Errorf("HarnessVersion = %q after a probe failure, want empty", got)
	}
}

func TestAdapter_HarnessVersion_UnknownWhenProbeFailsEvenWithOutput(t *testing.T) {
	// Arrange: a failing command's output must not be trusted.
	a := adapterWithProbe(func(ctx context.Context) ([]byte, error) {
		return []byte("2.1.284 (Claude Code)"), errors.New("exit status 1")
	})

	// Act
	got := a.HarnessVersion(context.Background())

	// Assert
	if got != "" {
		t.Errorf("HarnessVersion = %q after a probe error, want empty", got)
	}
}

func TestAdapter_HarnessVersion_UnknownWhenOutputEmptyOrUnparseable(t *testing.T) {
	for _, out := range []string{"", "   \n", "not a version"} {
		a := adapterWithProbe(func(ctx context.Context) ([]byte, error) {
			return []byte(out), nil
		})

		if got := a.HarnessVersion(context.Background()); got != "" {
			t.Errorf("HarnessVersion with probe output %q = %q, want empty", out, got)
		}
	}
}

// A hanging probe that honours cancellation must not hang the caller when
// the context carries no deadline: the adapter bounds the call itself.
func TestAdapter_HarnessVersion_ReturnsWithinBoundWhenProbeHangs(t *testing.T) {
	// Arrange
	a := adapterWithProbe(func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	done := make(chan string, 1)

	// Act
	go func() { done <- a.HarnessVersion(context.Background()) }()

	// Assert
	select {
	case got := <-done:
		if got != "" {
			t.Errorf("HarnessVersion = %q for a hung probe, want empty", got)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("HarnessVersion did not return for a hanging probe; it must apply its own timeout")
	}
}

func TestAdapter_HarnessVersion_ConcurrentCallsAreRaceFree(t *testing.T) {
	// Arrange
	a := adapterWithProbe(func(ctx context.Context) ([]byte, error) {
		return []byte("2.1.284 (Claude Code)"), nil
	})
	const goroutines = 20
	results := make([]string, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	// Act
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = a.HarnessVersion(context.Background())
		}(i)
	}
	wg.Wait()

	// Assert
	for i, got := range results {
		if got != "2.1.284" {
			t.Errorf("goroutine %d: HarnessVersion = %q, want %q", i, got, "2.1.284")
		}
	}
}

func TestAdapter_ImplementsHarnessVersionReporter(t *testing.T) {
	var adapter domain.HarnessAdapter = claudecode.New(claudecode.Options{})

	if _, ok := adapter.(domain.HarnessVersionReporter); !ok {
		t.Error("the Claude Code adapter must expose the optional HarnessVersionReporter capability")
	}
}
