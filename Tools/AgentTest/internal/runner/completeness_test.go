package runner_test

// TestProvisionRequestCompletenessGuard is the dedicated completeness guard
// for domain.ProvisionRequest called for by Plan.md ("Add the completeness
// guard (test or unrepresentable-state model change) covering exactly these
// two fields") and ContractsDesign.md ("Completeness is a comparison, not a
// claim. The ProvisionRequest guard compares the request the runner builds
// against each adapter's requirements for two named fields").
//
// It differs from the field-level tests in setup_test.go
// (TestRun_SetupProvisionRequestCarriesTheInterceptorSubcommand,
// TestRun_SetupProvisionRequestCarriesTheResolvedInterpreter,
// TestRun_SetupProvisionRequestLeavesInterpreterCmdEmptyWhenUnresolved) in
// what it guards against: those tests each pin the value of one already-known
// field and would stay green even if a future change added an eighth field to
// domain.ProvisionRequest that setup silently left unpopulated — exactly the
// class of defect Plan.md's Goal names ("make this class of defect
// structurally unable to recur silently"). This test instead enumerates every
// field of domain.ProvisionRequest by reflection and requires each one to be
// explicitly classified as guarded, excluded or out of this guard's scope. An
// unclassified field fails the test immediately, forcing a conscious decision
// rather than a silent gap.
import (
	"context"
	"reflect"
	"sort"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runner"
)

func TestProvisionRequestCompletenessGuard(t *testing.T) {
	// guardedFields are the fields this guard actively checks the runner
	// populates correctly: exactly the two fields Plan.md and
	// ContractsDesign.md name (InterceptorArgs, InterpreterCmd).
	guardedFields := map[string]bool{
		"InterceptorArgs": true,
		"InterpreterCmd":  true,
	}

	// excludedFields are deliberately unset by the runner and deliberately
	// excluded from the completeness obligation. There are currently no fields
	// in this category: ProvisionRequest.Collaborators was removed when the
	// adapter's collaborator-writing loop was retired in favour of the deploy
	// port, so there is nothing to exclude here. The map is kept in place so
	// the guard's three-category structure stays visible if a future field
	// warrants exclusion.
	excludedFields := map[string]bool{}

	// outOfScopeFields are fields the runner already populates from data the
	// caller supplies directly (the sandbox, the subject, the logger bundle
	// location, this binary's own path) rather than from the two
	// preflight-resolved values this guard is about. They are not part of the
	// defect this guard closes, but they must still be named here: an
	// unnamed field is what makes this a completeness guard rather than a
	// two-field spot check.
	outOfScopeFields := map[string]bool{
		"Sandbox":         true,
		"Subject":         true,
		"LoggerBundleDir": true,
		"InterceptorPath": true,
	}

	reqType := reflect.TypeOf(domain.ProvisionRequest{})
	var unclassified []string
	for i := 0; i < reqType.NumField(); i++ {
		name := reqType.Field(i).Name
		if guardedFields[name] || excludedFields[name] || outOfScopeFields[name] {
			continue
		}
		unclassified = append(unclassified, name)
	}
	if len(unclassified) > 0 {
		sort.Strings(unclassified)
		t.Fatalf("domain.ProvisionRequest has field(s) %v not classified as guarded, excluded or out of scope by this test; "+
			"a new field must be explicitly added to one of those categories rather than left to silently go unpopulated", unclassified)
	}

	h := newHarness(t)
	h.Deps.InterpreterCmd = "py"
	req := newRequest("provision-request-completeness-guard")

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	got := h.Adapter.lastProvisionReq

	// Guarded field: InterceptorArgs. Left unset, the registered hook falls
	// through to the CLI frontend instead of reaching the interceptor.
	if len(got.InterceptorArgs) == 0 || got.InterceptorArgs[0] != domain.InterceptorSubcommand {
		t.Errorf("ProvisionRequest.InterceptorArgs = %v, want it to begin with domain.InterceptorSubcommand %q", got.InterceptorArgs, domain.InterceptorSubcommand)
	}

	// Guarded field: InterpreterCmd. Left unset, the logger bundle keeps the
	// published placeholder interpreter and the logger hooks never run.
	if got.InterpreterCmd != h.Deps.InterpreterCmd {
		t.Errorf("ProvisionRequest.InterpreterCmd = %q, want Deps.InterpreterCmd %q carried verbatim", got.InterpreterCmd, h.Deps.InterpreterCmd)
	}
}
