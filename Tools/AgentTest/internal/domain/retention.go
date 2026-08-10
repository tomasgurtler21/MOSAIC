package domain

// RetentionPolicy is the run-level decision about what happens to a sandbox
// after an attempt. It is passed into the runner by the frontend, never read
// from the environment inside it: a diagnostic capability that depends on an
// ambient variable is a capability no report can explain.
type RetentionPolicy string

const (
	// RetainNever deprovisions and deletes the sandbox. The default.
	RetainNever RetentionPolicy = "never"
	// RetainAlways keeps every sandbox, scrubbed of credential material.
	RetainAlways RetentionPolicy = "always"
	// RetainOnFailure keeps a sandbox only when the attempt failed or never
	// started. The useful default for CI.
	RetainOnFailure RetentionPolicy = "on_failure"
)
