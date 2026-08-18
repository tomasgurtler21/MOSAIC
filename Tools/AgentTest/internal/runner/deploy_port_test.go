package runner_test

// Tests for Stage 5 runner.Deps changes: the deployment port is wired as an
// injected dependency of the runner.
//
// The runner's dependency-injection contract requires that:
//   - runner.Deps carries a Deploy field of interface type domain.AgentDeployer.
//   - The field is an interface (a port), not any concrete harness type.
//     The runner spawns nothing directly and names no harness.
//   - Like SelfPath, LoggerBundleDir, and InterpreterCmd, Deploy is a
//     pass-through value the runner does not interpret.
//
// Structural rather than behavioural: these tests inspect runner.Deps' own
// field set via reflection, so they compile whether or not the field has been
// added yet, and fail only while the field is absent or has the wrong type.
// This is the same shape as TestRunSettings_NoLongerCarriesHarnessIdentity in
// internal/authoring, which proved the pattern works for struct-contract tests.
//
// The runner does not use Deploy during Stage 5. The dependency is wired but
// unused: the composition root supplies it, and the runner holds it so that
// Stage 6 can add the rendering calls without changing the Deps interface.
// That end state is correct and expected; there is no test for "runner calls
// Deploy" here.

import (
	"reflect"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runner"
)

// TestRunnerDeps_HasDeployField verifies that runner.Deps declares a field
// named Deploy. Without it the deployment port cannot be injected and the
// runner cannot pass it to the rendering path later added in Stage 6.
func TestRunnerDeps_HasDeployField(t *testing.T) {
	typ := reflect.TypeOf(runner.Deps{})
	if _, ok := typ.FieldByName("Deploy"); !ok {
		t.Fatal("runner.Deps does not have a Deploy field; " +
			"the deployment port must be an injected dependency so the runner " +
			"can render the subject and each stub collaborator without naming a harness")
	}
}

// TestRunnerDeps_Deploy_IsInterfaceType verifies that runner.Deps.Deploy is
// an interface type, not a concrete implementation. The runner must never name
// a harness or spawn anything itself; holding a port (interface) rather than a
// concrete type is the structural guarantee.
func TestRunnerDeps_Deploy_IsInterfaceType(t *testing.T) {
	typ := reflect.TypeOf(runner.Deps{})
	f, ok := typ.FieldByName("Deploy")
	if !ok {
		t.Fatal("runner.Deps does not have a Deploy field; " +
			"see TestRunnerDeps_HasDeployField")
	}
	if f.Type.Kind() != reflect.Interface {
		t.Errorf("runner.Deps.Deploy has kind %v, want Interface; "+
			"the runner must hold a port (interface), not a concrete harness type — "+
			"the runner spawns nothing directly and names no harness",
			f.Type.Kind())
	}
}

// TestRunnerDeps_Deploy_TypeIsAgentDeployer verifies that runner.Deps.Deploy
// is exactly domain.AgentDeployer — not some other interface. This pins the
// contract between the runner and the deployment port.
func TestRunnerDeps_Deploy_TypeIsAgentDeployer(t *testing.T) {
	typ := reflect.TypeOf(runner.Deps{})
	f, ok := typ.FieldByName("Deploy")
	if !ok {
		t.Fatal("runner.Deps does not have a Deploy field; " +
			"see TestRunnerDeps_HasDeployField")
	}

	wantType := reflect.TypeOf((*domain.AgentDeployer)(nil)).Elem()
	if f.Type != wantType {
		t.Errorf("runner.Deps.Deploy field type is %v, want domain.AgentDeployer; "+
			"the field must use the AgentDeployer port type so the runner is decoupled "+
			"from any concrete deployment implementation",
			f.Type)
	}
}

// TestRunnerDeps_DeployField_CanBeAssignedNil verifies that when a
// runner.Deps is constructed without setting Deploy, the field is nil rather
// than panicking or being a different zero value. This pins the "no deployer
// yet" state that the composition root starts from.
func TestRunnerDeps_DeployField_CanBeAssignedNil(t *testing.T) {
	typ := reflect.TypeOf(runner.Deps{})
	if _, ok := typ.FieldByName("Deploy"); !ok {
		t.Fatal("runner.Deps does not have a Deploy field; " +
			"see TestRunnerDeps_HasDeployField")
	}

	// Construct via reflection so this test compiles even before the field is
	// added; a successful FieldByName above guarantees the field index is valid.
	zero := reflect.New(typ).Elem()
	deployField := zero.FieldByName("Deploy")
	if !deployField.IsNil() {
		t.Error("runner.Deps.Deploy is non-nil in its zero value; " +
			"an interface field must default to nil so the composition root " +
			"can construct Deps before supplying the deployer")
	}
}
