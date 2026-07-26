package domain

// HookSupport declares whether a harness supports hook bundles and which variant key it uses.
type HookSupport struct {
	Supported  bool
	VariantKey string // which hook-bundle variant folder this harness consumes; defaults to harness ID
}

// RegistrationStep is one step the executor (or the user) must perform to register a hook.
type RegistrationStep struct {
	ID          string
	TargetPath  string // relative to workspace; empty when the tool cannot perform the step at all
	Fragment    string // exact content to write, or to show the user when the target already exists
	Performable bool   // false => always a TODO item (e.g. a user-level editor setting)
	Instruction string // human-readable instruction for the TODO checklist
}

// HookPlanRequest is the input to HarnessModule.HookPlan.
type HookPlanRequest struct {
	Bundle HookBundle
	Scope  Scope
}

// HookPlan is the resolved deployment plan for one hook bundle on one harness.
type HookPlan struct {
	Supported    bool
	Reason       string // why unsupported, when Supported is false
	Files        []HookFile
	TargetDir    string // relative to deployment root
	Registration []RegistrationStep
}
