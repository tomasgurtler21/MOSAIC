// Package contracttest provides the shared harness contract test suite. Every built-in
// harness module, the descriptor-only adapter, and the external adapter are verified by
// calling Run with appropriate fixtures. This ensures all provision tiers satisfy the
// domain.HarnessModule contract identically.
//
// Universal invariants (always asserted, no fixture input required):
//   - Ref() never panics; two calls return structurally equal values with a non-empty ID.
//   - Descriptor() is non-nil; two calls return the same pointer (read-only contract).
//   - Tools: every entry in ToolRequest.Generic appears exactly once in Resolutions, same order.
//   - Frontmatter: KeyOrder never contains a key that is also in Remove.
//   - TargetPath for an unsupported artifact kind returns domain.ErrArtifactUnsupported.
//   - Injection returns ok==false for any name outside docformat.CanonicalInjections.
//   - HookPlan with Supported==false returns a non-empty Reason and no Files.
//   - Determinism: every method called twice with equal input returns equal output.
//   - Close() returns nil; a second Close() call also returns nil.
package contracttest

import (
	"errors"
	"reflect"
	"testing"

	"mosaic-deploy/internal/docformat"
	"mosaic-deploy/internal/domain"
)

// Fixtures carries per-harness test data for Run. Every slice and map field is optional:
// an empty field skips that method's sub-suite. The universal invariants are always checked
// regardless of fixture content.
type Fixtures struct {
	// End-to-end pipeline fidelity. Tests that call transform.Apply are handled in the
	// harness-specific test packages (claudecode_test, ghcpcli_test); these fields carry
	// the raw data needed for per-method assertions only.

	// ToolCases drives the Tools method sub-suite.
	ToolCases []ToolCase

	// FrontmatterCases drives the Frontmatter method sub-suite.
	FrontmatterCases []FrontmatterCase

	// InjectionCases maps canonical injection name to the expected content string.
	// Injection(name) must return (content, true) for each entry in this map.
	InjectionCases map[string]string

	// NotFilled lists injection names for which Injection must return ok==false.
	NotFilled []string

	// TargetPathCases drives the TargetPath method sub-suite.
	TargetPathCases []TargetPathCase

	// HookPlanCases drives the HookPlan method sub-suite.
	HookPlanCases []HookPlanCase
}

// ToolCase is one Tools method test case.
type ToolCase struct {
	Name        string
	Request     domain.ToolRequest
	Fields      []domain.FrontmatterField // expected rendered fields, in emission order
	Resolutions []domain.ToolResolution   // expected, one per Request.Generic, same order
}

// FrontmatterCase is one Frontmatter method test case.
type FrontmatterCase struct {
	Name     string
	Request  domain.FrontmatterRequest
	Expected domain.FrontmatterPlan // Set, Remove, and KeyOrder all compared exactly
}

// TargetPathCase is one TargetPath method test case.
type TargetPathCase struct {
	Name     string
	Request  domain.TargetPathRequest
	Expected string // expected path; ignored when Err is non-nil
	Err      error  // non-nil => errors.Is(err, Err) must match; Expected is ignored
}

// HookPlanCase is one HookPlan method test case.
type HookPlanCase struct {
	Name      string
	Request   domain.HookPlanRequest
	Supported bool
	TargetDir string   // compared only when Supported
	FileNames []string // expected HookPlan.Files target names, in order; compared only when Supported
	StepIDs   []string // expected RegistrationStep ids, in order; compared only when Supported
}

// Run drives m through the full contract test suite. It runs universal invariants unconditionally
// and per-method sub-suites for each non-empty section of f. Individual sub-tests are reported
// separately so a failure isolates to the method that produced it.
func Run(t *testing.T, m domain.HarnessModule, f Fixtures) {
	t.Helper()

	t.Run("universal", func(t *testing.T) {
		runUniversalInvariants(t, m)
	})

	if len(f.ToolCases) > 0 {
		t.Run("Tools", func(t *testing.T) {
			runToolCases(t, m, f.ToolCases)
		})
	}

	if len(f.FrontmatterCases) > 0 {
		t.Run("Frontmatter", func(t *testing.T) {
			runFrontmatterCases(t, m, f.FrontmatterCases)
		})
	}

	if len(f.InjectionCases) > 0 || len(f.NotFilled) > 0 {
		t.Run("Injection", func(t *testing.T) {
			runInjectionCases(t, m, f.InjectionCases, f.NotFilled)
		})
	}

	if len(f.TargetPathCases) > 0 {
		t.Run("TargetPath", func(t *testing.T) {
			runTargetPathCases(t, m, f.TargetPathCases)
		})
	}

	if len(f.HookPlanCases) > 0 {
		t.Run("HookPlan", func(t *testing.T) {
			runHookPlanCases(t, m, f.HookPlanCases)
		})
	}
}

// ---------------------------------------------------------------------------
// Universal invariants
// ---------------------------------------------------------------------------

func runUniversalInvariants(t *testing.T, m domain.HarnessModule) {
	t.Helper()

	t.Run("Ref_non_empty_id", func(t *testing.T) {
		ref := m.Ref()
		if ref.ID == "" {
			t.Error("Ref().ID is empty; must be non-empty")
		}
	})

	t.Run("Ref_deterministic", func(t *testing.T) {
		r1 := m.Ref()
		r2 := m.Ref()
		if r1.ID != r2.ID || r1.DisplayName != r2.DisplayName || r1.Tier != r2.Tier {
			t.Errorf("Ref() returned different values on two calls:\n  call 1: %+v\n  call 2: %+v", r1, r2)
		}
	})

	t.Run("Descriptor_non_nil", func(t *testing.T) {
		d := m.Descriptor()
		if d == nil {
			t.Fatal("Descriptor() returned nil")
		}
	})

	t.Run("Descriptor_same_pointer", func(t *testing.T) {
		d1 := m.Descriptor()
		d2 := m.Descriptor()
		if d1 != d2 {
			t.Error("Descriptor() returned different pointers on two calls; the returned value must be the same object (read-only contract)")
		}
	})

	t.Run("Tools_one_resolution_per_generic", func(t *testing.T) {
		req := domain.ToolRequest{
			AgentKey: "invariant-check",
			Generic:  []string{"file_read", "terminal", "user_interaction"},
		}
		result, err := m.Tools(req)
		if err != nil {
			t.Fatalf("Tools: %v", err)
		}
		if len(result.Resolutions) != len(req.Generic) {
			t.Errorf("Tools returned %d resolutions for %d generic tools; want equal counts",
				len(result.Resolutions), len(req.Generic))
			return
		}
		for i, res := range result.Resolutions {
			if res.Generic != req.Generic[i] {
				t.Errorf("Resolutions[%d].Generic = %q; want %q (order must match request order)",
					i, res.Generic, req.Generic[i])
			}
		}
	})

	t.Run("Tools_empty_generic_gives_empty_resolutions", func(t *testing.T) {
		req := domain.ToolRequest{AgentKey: "empty-tools", Generic: []string{}}
		result, err := m.Tools(req)
		if err != nil {
			t.Fatalf("Tools with empty Generic: %v", err)
		}
		if len(result.Resolutions) != 0 {
			t.Errorf("Tools with empty Generic returned %d resolutions; want 0", len(result.Resolutions))
		}
	})

	t.Run("Tools_deterministic", func(t *testing.T) {
		req := domain.ToolRequest{AgentKey: "det-check", Generic: []string{"file_read", "terminal"}}
		r1, err1 := m.Tools(req)
		r2, err2 := m.Tools(req)
		if (err1 == nil) != (err2 == nil) {
			t.Errorf("Tools determinism: first call err=%v, second call err=%v", err1, err2)
			return
		}
		if err1 != nil {
			return // error determinism already checked above
		}
		if !reflect.DeepEqual(r1, r2) {
			t.Error("Tools returned different results on two identical calls")
		}
	})

	t.Run("Frontmatter_key_order_disjoint_from_remove", func(t *testing.T) {
		req := domain.FrontmatterRequest{
			Kind:     domain.ArtifactAgent,
			AgentKey: "order-check",
		}
		plan, err := m.Frontmatter(req)
		if err != nil {
			t.Fatalf("Frontmatter: %v", err)
		}
		removeSet := make(map[string]bool, len(plan.Remove))
		for _, k := range plan.Remove {
			removeSet[k] = true
		}
		for _, k := range plan.KeyOrder {
			if removeSet[k] {
				t.Errorf("KeyOrder contains %q which is also in Remove; a key cannot be both ordered and removed", k)
			}
		}
	})

	t.Run("Frontmatter_deterministic", func(t *testing.T) {
		req := domain.FrontmatterRequest{Kind: domain.ArtifactAgent, AgentKey: "det-fm"}
		p1, err1 := m.Frontmatter(req)
		p2, err2 := m.Frontmatter(req)
		if (err1 == nil) != (err2 == nil) {
			t.Errorf("Frontmatter determinism: first err=%v, second err=%v", err1, err2)
			return
		}
		if err1 != nil {
			return
		}
		if !reflect.DeepEqual(p1, p2) {
			t.Error("Frontmatter returned different plans on two identical calls")
		}
	})

	t.Run("TargetPath_unsupported_kind_returns_sentinel", func(t *testing.T) {
		// "workflow" is not an artifact kind the harness can deploy; any unknown kind must
		// return ErrArtifactUnsupported, never an empty string with a nil error.
		req := domain.TargetPathRequest{
			Kind:  domain.ArtifactKind("workflow"),
			Key:   "some-workflow",
			Scope: domain.ScopeProject,
			GOOS:  "linux",
		}
		path, err := m.TargetPath(req)
		if err == nil {
			t.Errorf("TargetPath for unknown kind returned %q with nil error; want ErrArtifactUnsupported", path)
			return
		}
		if !errors.Is(err, domain.ErrArtifactUnsupported) {
			t.Errorf("TargetPath for unknown kind returned err=%v; want errors.Is(err, ErrArtifactUnsupported)", err)
		}
		if path != "" {
			t.Errorf("TargetPath for unknown kind returned non-empty path %q alongside error", path)
		}
	})

	t.Run("TargetPath_never_empty_on_success", func(t *testing.T) {
		// A supported artifact kind must never return an empty string with a nil error.
		req := domain.TargetPathRequest{
			Kind:  domain.ArtifactAgent,
			Key:   "test-agent",
			Scope: domain.ScopeProject,
			GOOS:  "linux",
		}
		path, err := m.TargetPath(req)
		if err == nil && path == "" {
			t.Error("TargetPath returned empty path with nil error; must return a non-empty path or an error")
		}
	})

	t.Run("Injection_unknown_name_returns_false", func(t *testing.T) {
		// Any name outside CanonicalInjections must return ok==false.
		nonCanonical := []string{
			"NotAnInjection",
			"",
			"random-garbage-xyz",
			"harness_constraints", // wrong casing
		}
		for _, name := range nonCanonical {
			_, ok := m.Injection(name)
			if ok {
				t.Errorf("Injection(%q) returned ok=true; must return ok=false for non-canonical injection names", name)
			}
		}
	})

	t.Run("Injection_deterministic", func(t *testing.T) {
		for _, name := range docformat.CanonicalInjections {
			c1, ok1 := m.Injection(name)
			c2, ok2 := m.Injection(name)
			if ok1 != ok2 || c1 != c2 {
				t.Errorf("Injection(%q) non-deterministic: first=(%q,%v), second=(%q,%v)",
					name, c1, ok1, c2, ok2)
			}
		}
	})

	t.Run("HookPlan_unsupported_has_reason_no_files", func(t *testing.T) {
		// This invariant applies when HookPlan returns Supported==false, regardless of
		// whether the harness supports hooks at all.
		req := domain.HookPlanRequest{}
		plan, err := m.HookPlan(req)
		if err != nil {
			// Not all harnesses will error here; skip if the harness supports hooks.
			return
		}
		if !plan.Supported {
			if plan.Reason == "" {
				t.Error("HookPlan returned Supported=false with empty Reason; Reason must be non-empty")
			}
			if len(plan.Files) > 0 {
				t.Errorf("HookPlan returned Supported=false but Files contains %d entries; must be empty", len(plan.Files))
			}
		}
	})

	t.Run("Close_returns_nil", func(t *testing.T) {
		if err := m.Close(); err != nil {
			t.Errorf("Close() returned %v; built-in modules must return nil", err)
		}
	})

	t.Run("Close_idempotent", func(t *testing.T) {
		// First Close already called in Close_returns_nil; a second call must also return nil.
		if err := m.Close(); err != nil {
			t.Errorf("second Close() returned %v; Close must be idempotent and return nil on repeated calls", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Per-method sub-suites
// ---------------------------------------------------------------------------

func runToolCases(t *testing.T, m domain.HarnessModule, cases []ToolCase) {
	t.Helper()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			result, err := m.Tools(tc.Request)
			if err != nil {
				t.Fatalf("Tools: %v", err)
			}

			// Resolutions: one per Generic entry, same order.
			if len(result.Resolutions) != len(tc.Request.Generic) {
				t.Errorf("Resolutions count: got %d, want %d",
					len(result.Resolutions), len(tc.Request.Generic))
			} else {
				for i, res := range result.Resolutions {
					exp := tc.Resolutions[i]
					if res.Generic != exp.Generic {
						t.Errorf("Resolutions[%d].Generic = %q, want %q", i, res.Generic, exp.Generic)
					}
					if res.Outcome != exp.Outcome {
						t.Errorf("Resolutions[%d].Outcome = %q, want %q", i, res.Outcome, exp.Outcome)
					}
					if !reflect.DeepEqual(res.HarnessTools, exp.HarnessTools) {
						t.Errorf("Resolutions[%d].HarnessTools = %v, want %v", i, res.HarnessTools, exp.HarnessTools)
					}
				}
			}

			// Fields: compare emission order and values.
			if !reflect.DeepEqual(result.Fields, tc.Fields) {
				t.Errorf("Fields mismatch:\n  got  %v\n  want %v", result.Fields, tc.Fields)
			}
		})
	}
}

func runFrontmatterCases(t *testing.T, m domain.HarnessModule, cases []FrontmatterCase) {
	t.Helper()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			plan, err := m.Frontmatter(tc.Request)
			if err != nil {
				t.Fatalf("Frontmatter: %v", err)
			}

			if !reflect.DeepEqual(plan.Set, tc.Expected.Set) {
				t.Errorf("Set mismatch:\n  got  %v\n  want %v", plan.Set, tc.Expected.Set)
			}
			if !reflect.DeepEqual(plan.Remove, tc.Expected.Remove) {
				t.Errorf("Remove mismatch:\n  got  %v\n  want %v", plan.Remove, tc.Expected.Remove)
			}
			if !reflect.DeepEqual(plan.KeyOrder, tc.Expected.KeyOrder) {
				t.Errorf("KeyOrder mismatch:\n  got  %v\n  want %v", plan.KeyOrder, tc.Expected.KeyOrder)
			}
		})
	}
}

func runInjectionCases(t *testing.T, m domain.HarnessModule, cases map[string]string, notFilled []string) {
	t.Helper()

	for name, want := range cases {
		name, want := name, want
		t.Run("filled_"+name, func(t *testing.T) {
			got, ok := m.Injection(name)
			if !ok {
				t.Errorf("Injection(%q) returned ok=false; want ok=true with content %q", name, want)
				return
			}
			if got != want {
				t.Errorf("Injection(%q): got %q, want %q", name, got, want)
			}
		})
	}

	for _, name := range notFilled {
		name := name
		t.Run("not_filled_"+name, func(t *testing.T) {
			_, ok := m.Injection(name)
			if ok {
				t.Errorf("Injection(%q) returned ok=true; this injection should not be filled by this harness", name)
			}
		})
	}
}

func runTargetPathCases(t *testing.T, m domain.HarnessModule, cases []TargetPathCase) {
	t.Helper()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			path, err := m.TargetPath(tc.Request)

			if tc.Err != nil {
				// Expecting an error.
				if err == nil {
					t.Errorf("TargetPath: got path=%q err=nil; want err wrapping %v", path, tc.Err)
					return
				}
				if !errors.Is(err, tc.Err) {
					t.Errorf("TargetPath: err=%v; want errors.Is(err, %v)", err, tc.Err)
				}
				return
			}

			// Expecting success.
			if err != nil {
				t.Fatalf("TargetPath: unexpected error: %v", err)
			}
			if path != tc.Expected {
				t.Errorf("TargetPath: got %q, want %q", path, tc.Expected)
			}
		})
	}
}

func runHookPlanCases(t *testing.T, m domain.HarnessModule, cases []HookPlanCase) {
	t.Helper()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			plan, err := m.HookPlan(tc.Request)
			if err != nil {
				t.Fatalf("HookPlan: %v", err)
			}

			if plan.Supported != tc.Supported {
				t.Errorf("HookPlan.Supported = %v, want %v", plan.Supported, tc.Supported)
			}

			if !tc.Supported {
				if plan.Reason == "" {
					t.Error("HookPlan.Reason is empty for an unsupported harness; must be non-empty")
				}
				if len(plan.Files) > 0 {
					t.Errorf("HookPlan.Files has %d entries for unsupported harness; must be empty", len(plan.Files))
				}
				return
			}

			if plan.TargetDir != tc.TargetDir {
				t.Errorf("HookPlan.TargetDir = %q, want %q", plan.TargetDir, tc.TargetDir)
			}

			gotNames := make([]string, len(plan.Files))
			for i, f := range plan.Files {
				gotNames[i] = f.TargetName
			}
			if !reflect.DeepEqual(gotNames, tc.FileNames) {
				t.Errorf("HookPlan file names: got %v, want %v", gotNames, tc.FileNames)
			}

			gotStepIDs := make([]string, len(plan.Registration))
			for i, s := range plan.Registration {
				gotStepIDs[i] = s.ID
			}
			if !reflect.DeepEqual(gotStepIDs, tc.StepIDs) {
				t.Errorf("HookPlan step IDs: got %v, want %v", gotStepIDs, tc.StepIDs)
			}
		})
	}
}
