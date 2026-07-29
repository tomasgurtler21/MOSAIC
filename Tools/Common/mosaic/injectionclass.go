package mosaic

// InjectionClass distinguishes the three kinds of injection point a harness fills.
type InjectionClass string

const (
	InjectionHarness  InjectionClass = "harness"  // filled from descriptor on every transform
	InjectionProject  InjectionClass = "project"  // always empty on create, preserved on update
	InjectionWorkflow InjectionClass = "workflow" // AvailableWorkflows, assembled from selections
)
