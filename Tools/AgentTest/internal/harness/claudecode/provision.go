package claudecode

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mosaic-common/hookbundle"

	"mosaic-agent-test/internal/domain"
)

// DefaultSpawnTimeout is the adapter's own backstop on a single invocation —
// a generous ceiling that keeps a subject from outliving its supervisor. It
// is NOT the test's declared timeout: domain.RunSettings never reaches an
// adapter, and the declared timeout is enforced by the runner's supervisor
// cancelling the context given to SubjectLauncher.Launch. SpawnPlan.Timeout
// is set from this constant; tests assert equality with it, not merely that
// the value is positive.
const DefaultSpawnTimeout = 30 * time.Minute

// Options configures the adapter. Every field has a zero value that selects
// real behaviour, so the composition root passes only what it overrides and
// tests substitute seams without a build tag.
type Options struct {
	// LoggerBundleDir is the MOSAIC logger hook bundle, read as data.
	LoggerBundleDir string

	// Interpreter, when non-empty, is the interpreter command to substitute
	// into the bundle's generated entries instead of resolving one.
	Interpreter string

	// ScopeProbe reads configuration scopes outside the sandbox.
	// nil selects the real filesystem probe.
	ScopeProbe ScopeProbe

	// SelfPath resolves the absolute path of the running binary.
	// nil selects the real resolver. The bridge depends on this, so it is a
	// seam rather than an ambient call.
	SelfPath func() (string, error)

	// Credentials supplies the harness credential material Provision seeds
	// into the relocated config home. nil selects the real
	// user-home-based source. A test substitutes this seam so no test run
	// ever reads or copies the developer's real credentials.
	Credentials CredentialSource
}

// Adapter implements domain.HarnessAdapter for the Claude Code harness.
type Adapter struct {
	opts Options
}

var _ domain.HarnessAdapter = (*Adapter)(nil)

// New constructs the adapter. Construction alone performs no I/O.
func New(opts Options) *Adapter {
	return &Adapter{opts: opts}
}

// ID implements domain.HarnessAdapter.
func (a *Adapter) ID() string {
	return HarnessID
}

// Capabilities is a package-level function as well as a method, so the
// declared values can be asserted without constructing an adapter.
//
//	SupportsDirectSubstitution: false
//	    The pre-invocation point can rewrite the call's input but cannot
//	    fabricate a successful result; its denial path refuses a call with a
//	    reason, which reads to the subject as a refusal and not as a
//	    response. Every stubbed call therefore travels the prompt-rewrite
//	    path.
//	SupportsPostInterception:   true
//	    A post-invocation point exists, but it fires when the collaborator is
//	    launched, not when it finishes: it carries the harness's async-launch
//	    acknowledgement, never the collaborator's real reply. It remains an
//	    observation point (it never denies), but echo fidelity cannot be
//	    evaluated against what it observes.
//	SupportsReplyRecovery:      true
//	    The collaborator's real reply is recovered from the harness's own
//	    completion signal (its SubagentStop hook) instead, which is what
//	    makes echo-fidelity comparison possible on this harness.
//	CorrelationField:           "tool_use_id"
//	    The dispatch-scoped identifier the harness sends on every PreToolUse,
//	    PostToolUse and SubagentStop event. The correlation token is populated
//	    directly from this field on all three interception phases. It is not
//	    planted in the prompt text; prompt-planting and transcript-scanning are
//	    both retired. See correlation.go for the full mechanism basis.
//	RegistrationModel:          domain.RegistrationSharedFile
//	BridgeKind:                 domain.BridgeSpawned
func Capabilities() domain.HarnessCapabilities {
	return domain.HarnessCapabilities{
		SupportsDirectSubstitution: false,
		SupportsPostInterception:   true,
		SupportsReplyRecovery:      true,
		CorrelationField:           "tool_use_id",
		RegistrationModel:          domain.RegistrationSharedFile,
		BridgeKind:                 domain.BridgeSpawned,
		ProducibleToolNames:        []string{domain.DispatchToolName},
	}
}

// Capabilities implements domain.HarnessAdapter.
func (a *Adapter) Capabilities() domain.HarnessCapabilities {
	return Capabilities()
}

// ConfigScopes implements domain.HarnessAdapter. It enumerates scopes
// independent of any particular sandbox: the port method takes none, so the
// sandbox-scoped entry it returns carries no real root and exists only so
// InspectScopes and CheckEnvironment can filter it out uniformly.
func (a *Adapter) ConfigScopes() []domain.ConfigScope {
	return Scopes(domain.Sandbox{})
}

// InspectScopes implements domain.HarnessAdapter. It examines every
// non-sandbox scope ConfigScopes declares through this adapter's ScopeProbe
// (the real filesystem unless Options.ScopeProbe overrides it), and never
// folds an absent, unreadable or malformed scope into the findings as an
// ordinary clean inspection.
func (a *Adapter) InspectScopes(ctx context.Context) ([]domain.ScopeFinding, error) {
	probe := a.opts.ScopeProbe
	if probe == nil {
		probe = fsScopeProbe{}
	}

	var findings []domain.ScopeFinding
	for _, scope := range a.ConfigScopes() {
		if scope.InSandbox {
			continue
		}
		findings = append(findings, inspectOneScope(probe, scope))
	}
	return findings, nil
}

// CheckEnvironment implements domain.HarnessAdapter. It validates everything
// that must hold before the first subject is spawned: an interpreter the
// logger bundle can actually run, a readable bundle, a resolvable binary
// path for the bridge, and configuration scopes free of competing input
// rewriters. It loses no check the pre-port CheckEnvironment performed.
//
// It is called during pre-flight, not during provisioning, because a wrong
// interpreter yields a run with no logs and therefore no cost — a silent
// failure that must surface before an agent's cost is incurred.
func (a *Adapter) CheckEnvironment(ctx context.Context) (domain.EnvironmentReport, error) {
	var report domain.EnvironmentReport
	report.InterpreterApplicable = true

	interp, err := ResolveInterpreter(ctx, a.opts.Interpreter)
	report.InterpreterCmd = interp.Command
	report.InterpreterTried = interp.Tried
	if err != nil {
		report.Problems = append(report.Problems, domain.EnvironmentProblem{
			Kind:   domain.ProblemInterpreterUnresolved,
			Detail: err.Error(),
		})
	}

	if a.opts.LoggerBundleDir != "" {
		if _, err := hookbundle.Load(a.opts.LoggerBundleDir); err != nil {
			report.Problems = append(report.Problems, bundleProblem(a.opts.LoggerBundleDir, err))
		}
	}

	binPath, err := a.resolveSelfPath()
	report.BinaryPath = binPath
	if err != nil {
		report.Problems = append(report.Problems, domain.EnvironmentProblem{
			Kind:   domain.ProblemBinaryPathUnresolved,
			Detail: err.Error(),
		})
	}

	findings, err := a.InspectScopes(ctx)
	report.Scopes = findings
	if err == nil {
		for _, f := range findings {
			if f.RewritesInput && !f.Neutralized {
				report.Problems = append(report.Problems, domain.EnvironmentProblem{
					Kind:   domain.ProblemCompetingRewriter,
					Detail: f.Detail,
				})
			}
		}
	}

	return report, nil
}

// expectedBundleLayout names the logger-bundle directory shape
// hookbundle.Load requires, verbatim as ContractsDesign.md's configuration
// resolution contract states it. bundleProblem's Detail always includes
// this: a bundle problem must name what is expected, not only that
// something failed.
const expectedBundleLayout = `logger-bundle/
  hook.yaml                 <- the bundle manifest
  claude-code/               <- one directory per harness variant
    mosaic_logger.py
    mosaic_logger_core.py
    ...`

// bundleProblem classifies a hookbundle.Load failure into the neutral
// vocabulary and names the expected layout, the path that was searched, and
// the --logger-bundle override that would change it — never a bare
// file-not-found (AC6.2). A manifest that exists but cannot be parsed is
// ProblemBundleMisshapen, distinct from an absent or otherwise unreadable
// directory (ProblemBundleUnreadable): the two call for different fixes.
func bundleProblem(bundleDir string, loadErr error) domain.EnvironmentProblem {
	kind := domain.ProblemBundleUnreadable
	if errors.Is(loadErr, hookbundle.ErrMalformedManifest) {
		kind = domain.ProblemBundleMisshapen
	}

	return domain.EnvironmentProblem{
		Kind: kind,
		Detail: fmt.Sprintf(
			"logger bundle at %s could not be loaded: %v\nexpected layout (override with --logger-bundle or the MOSAIC_AGENT_TEST_LOGGER_BUNDLE environment variable):\n%s",
			bundleDir, loadErr, expectedBundleLayout,
		),
	}
}

// resolveSelfPath resolves the absolute path of the running binary, through
// Options.SelfPath when supplied and the real process resolver otherwise.
func (a *Adapter) resolveSelfPath() (string, error) {
	if a.opts.SelfPath != nil {
		return a.opts.SelfPath()
	}
	return os.Executable()
}

// Provision implements domain.HarnessAdapter. It installs the composed
// hook configuration and the logger bundle into req.Sandbox, and refuses
// when the composed configuration would contain more than one entry that
// rewrites the intercepted call's input.
//
// On any failure Provision still returns the ledger of what it created
// before the failure, so a caller can tear down exactly that partial state
// through Deprovision; only the error signals that setup did not complete.
func (a *Adapter) Provision(ctx context.Context, req domain.ProvisionRequest) (domain.Provisioning, error) {
	prov := domain.Provisioning{Sandbox: req.Sandbox}

	hooksDir := filepath.Join(req.Sandbox.SubjectDir, HooksRelDir)

	var bundleContribs []Contribution
	if req.LoggerBundleDir != "" {
		contribs, err := a.provisionLoggerBundle(&prov, req, hooksDir)
		if err != nil {
			return prov, err
		}
		bundleContribs = contribs
	}

	preBridge, err := BuildBridge(req, domain.PhasePre)
	if err != nil {
		return prov, fmt.Errorf("claudecode: building pre-invocation bridge: %w", err)
	}
	postBridge, err := BuildBridge(req, domain.PhasePost)
	if err != nil {
		return prov, fmt.Errorf("claudecode: building post-invocation bridge: %w", err)
	}
	completionBridge, err := BuildBridge(req, domain.PhaseCompletion)
	if err != nil {
		return prov, fmt.Errorf("claudecode: building completion bridge: %w", err)
	}

	allContribs := make([]Contribution, 0, len(bundleContribs)+2)
	allContribs = append(allContribs, InterceptorEntries(preBridge, postBridge))
	allContribs = append(allContribs, CompletionEntry(completionBridge))
	allContribs = append(allContribs, bundleContribs...)

	if req.InterpreterCmd != "" {
		for i := range allContribs {
			substituted, _ := SubstituteInterpreter(allContribs[i].Settings, PublishedInterpreter, req.InterpreterCmd)
			allContribs[i].Settings = substituted
		}
	}

	composed, err := Compose(allContribs...)
	if err != nil {
		return prov, fmt.Errorf("claudecode: composing hook configuration: %w", err)
	}

	marshaled, err := Marshal(composed)
	if err != nil {
		return prov, fmt.Errorf("claudecode: marshaling composed hook configuration: %w", err)
	}

	settingsPath := filepath.Join(req.Sandbox.SubjectDir, SettingsRelPath)
	if err := writeTracked(&prov, settingsPath, marshaled); err != nil {
		return prov, err
	}

	if err := a.seedCredentials(&prov, req); err != nil {
		return prov, err
	}

	if findings, err := a.InspectScopes(ctx); err == nil {
		prov.ScopeFindings = findings
	}

	return prov, nil
}

// seedCredentials seeds the harness's credential file(s) into the same
// relocated config-home directory SpawnPlan points ConfigHomeEnvVar at
// (ConfigHomeRelDir), so relocating the user scope to neutralize its
// competing-rewriter risk does not also break the subject's ability to
// authenticate. Every seeded path is recorded both in prov.Files (so
// Deprovision removes it) and prov.Sensitive (so a retained sandbox never
// keeps it — see runner.Teardown's scrub path).
//
// The credential source is Options.Credentials when set, and the real
// user-home-based source otherwise. A source reporting no credentials (the
// harness is not logged in) is not an error here: the resulting
// authentication failure is diagnosed later from the subject's exit code and
// stderr, not pre-empted during provisioning.
func (a *Adapter) seedCredentials(prov *domain.Provisioning, req domain.ProvisionRequest) error {
	src := a.opts.Credentials
	if src == nil {
		src = realCredentialSource{}
	}

	files, err := src.Credentials()
	if err != nil {
		return fmt.Errorf("claudecode: reading harness credentials: %w", err)
	}

	configHomeDir := filepath.Join(req.Sandbox.ControlDir, ConfigHomeRelDir)
	for _, f := range files {
		destPath, err := safeJoin(configHomeDir, f.RelPath)
		if err != nil {
			return fmt.Errorf("claudecode: seeded credential file %q: %w", f.RelPath, err)
		}

		mode := f.Mode
		if mode == 0 {
			mode = 0o600
		}
		if err := writeTrackedMode(prov, destPath, f.Content, mode); err != nil {
			return err
		}
		prov.Sensitive = append(prov.Sensitive, destPath)
	}
	return nil
}

// provisionLoggerBundle resolves the bundle's variant for this harness
// through the shared manifest package and copies exactly the files that
// variant lists into the workspace's hook directory, honouring source and
// target renames and a source path that reaches outside the variant
// directory. Its file list and event registrations are read from the
// manifest as data and never restated here: adding a file or an event to
// the bundle changes what this deploys with no edit to this function.
func (a *Adapter) provisionLoggerBundle(prov *domain.Provisioning, req domain.ProvisionRequest, hooksDir string) ([]Contribution, error) {
	manifest, err := hookbundle.Load(req.LoggerBundleDir)
	if err != nil {
		return nil, fmt.Errorf("claudecode: loading logger bundle manifest: %w", err)
	}
	variant, err := manifest.ResolveVariant(HarnessID, "")
	if err != nil {
		return nil, fmt.Errorf("claudecode: resolving logger bundle variant for %q: %w", HarnessID, err)
	}

	variantDir := filepath.Join(req.LoggerBundleDir, variant.Key)
	for _, f := range variant.Files {
		destPath, err := safeJoin(hooksDir, f.Target)
		if err != nil {
			return nil, fmt.Errorf("claudecode: logger bundle file target %q: %w", f.Target, err)
		}
		srcPath := filepath.Join(variantDir, filepath.FromSlash(f.Source))
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return nil, fmt.Errorf("claudecode: reading logger bundle file %q: %w", srcPath, err)
		}
		if err := writeTracked(prov, destPath, data); err != nil {
			return nil, err
		}
	}

	var contribs []Contribution
	for _, step := range variant.Registration {
		settings, err := ParseFragment(step.Fragment)
		if err != nil {
			return nil, fmt.Errorf("claudecode: parsing logger bundle registration fragment %q: %w", step.ID, err)
		}
		contribs = append(contribs, Contribution{
			Source: manifest.ID,
			// The logger bundle's entries are asynchronous and observational;
			// they never rewrite the intercepted call's input, which is what
			// lets them coexist with the interceptor's single rewriting entry.
			RewritesInput: false,
			Settings:      settings,
		})
	}
	return contribs, nil
}

// Deprovision implements domain.HarnessAdapter. It removes exactly what
// Provisioning records and nothing else, and is idempotent: a path already
// gone is not an error.
func (a *Adapter) Deprovision(ctx context.Context, p domain.Provisioning) error {
	for i := len(p.Files) - 1; i >= 0; i-- {
		if err := os.Remove(p.Files[i]); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("claudecode: removing %q: %w", p.Files[i], err)
		}
	}
	// Dirs is recorded outermost first; removing in reverse order removes
	// children before their parents.
	for i := len(p.Dirs) - 1; i >= 0; i-- {
		if err := os.Remove(p.Dirs[i]); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("claudecode: removing directory %q: %w", p.Dirs[i], err)
		}
	}
	return nil
}

// safeJoin joins baseDir with a caller-declared relative path, refusing any
// path that would resolve outside baseDir. The tool never writes outside the
// workspace on any path, and this is the one place that promise is enforced.
func safeJoin(baseDir, rel string) (string, error) {
	cleanRel := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(cleanRel) {
		return "", fmt.Errorf("target %q must be relative", rel)
	}

	full := filepath.Join(baseDir, cleanRel)

	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", baseDir, err)
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", full, err)
	}

	relToBase, err := filepath.Rel(baseAbs, fullAbs)
	if err != nil || relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("target %q escapes %q", rel, baseDir)
	}
	return full, nil
}

// writeTracked creates any missing parent directories (recording exactly
// the ones it creates), writes data to path, and records path — so the
// returned ledger describes precisely what Deprovision must remove.
func writeTracked(prov *domain.Provisioning, path string, data []byte) error {
	return writeTrackedMode(prov, path, data, 0o644)
}

// writeTrackedMode is writeTracked with an explicit file mode — the seam
// credential seeding uses so a credential file lands with a stricter mode
// than the composed configuration's 0o644 default.
func writeTrackedMode(prov *domain.Provisioning, path string, data []byte, mode fs.FileMode) error {
	if err := ensureDirsFor(prov, filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("claudecode: writing %q: %w", path, err)
	}
	prov.Files = append(prov.Files, path)
	return nil
}

// ensureDirsFor creates dir and any missing ancestors, recording only the
// ones that did not already exist, outermost first, so Deprovision can undo
// exactly this call's effect and nothing a prior run left behind.
func ensureDirsFor(prov *domain.Provisioning, dir string) error {
	var missing []string
	d := dir
	for {
		info, err := os.Stat(d)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("claudecode: %q exists and is not a directory", d)
			}
			break
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("claudecode: stat %q: %w", d, err)
		}
		missing = append(missing, d)
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}

	for i, j := 0, len(missing)-1; i < j; i, j = i+1, j-1 {
		missing[i], missing[j] = missing[j], missing[i]
	}

	for _, m := range missing {
		if err := os.Mkdir(m, 0o755); err != nil && !os.IsExist(err) {
			return fmt.Errorf("claudecode: creating directory %q: %w", m, err)
		}
		prov.Dirs = append(prov.Dirs, m)
	}
	return nil
}
