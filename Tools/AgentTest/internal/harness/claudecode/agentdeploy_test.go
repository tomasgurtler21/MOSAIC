package claudecode_test

// The collaborator-writing tests that previously lived here (deploying stub
// collaborator definitions through ProvisionRequest.Collaborators) have been
// removed together with the mechanism they tested.
//
// Stub collaborator definitions are now rendered into the sandbox by the
// deployment port (domain.AgentDeployer) during runner.setup, before the
// harness adapter's Provision is ever called. The adapter's Provision no
// longer receives or writes stub definitions; tests for that old behaviour
// would test code that no longer exists.
//
// The new placement contract is exercised at the runner layer
// (internal/runner/render_test.go) and at the adapter contract level
// (internal/harness/contract). This file is kept as a placeholder so the
// test package boundary remains explicit.
