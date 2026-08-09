package opencode

// Provision and Deprovision, and the directory-drop expression of the
// single-rewriter invariant. See ContractsDesign.md's "AgentTest: Provision
// and Deprovision" and "PluginContribution / PluginFile" for the full
// contract.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mosaic-common/hookbundle"

	"mosaic-agent-test/internal/domain"
)

// ErrMultipleRewriters reports that more than one plugin able to rewrite the
// intercepted call's input would be present in the sandbox at once. It names
// every competing source.
var ErrMultipleRewriters = errors.New("opencode: more than one plugin would rewrite the intercepted call's input")

// PluginFile is one file destined for the sandbox's plugins directory.
type PluginFile struct {
	// Target is relative to the plugins directory. Never absolute, never
	// escaping it.
	Target  string
	Content []byte
}

// PluginContribution is one source's contribution to the plugins directory.
//
// It exists because a directory-drop registration model has no shared
// configuration document to compose, and the single-rewriter invariant
// still has to be checkable before anything is written. The composed thing
// is therefore the set of files that will exist, and RewritesInput is the
// declaration the check runs over.
type PluginContribution struct {
	// Source names the contributor in a refusal message: "interceptor", or
	// the hook bundle's own identifier.
	Source string

	// RewritesInput declares that this contribution registers something able
	// to rewrite the intercepted call's input. At most one contribution may
	// declare it.
	RewritesInput bool

	Files []PluginFile
}

// ComposePlugins collects every contribution's files into the set that will
// be written, refusing two contributions that both declare RewritesInput and
// refusing two contributions targeting the same file name.
//
// This is how the single-rewriter invariant is expressed for a
// directory-drop registration model: there is no shared configuration
// document to compose, so the composed thing is the set of files that will
// exist in the plugins directory, and the check runs over that set before
// anything is written.
func ComposePlugins(contribs ...PluginContribution) ([]PluginFile, error) {
	var rewriters []string
	seen := make(map[string]string)
	var files []PluginFile

	for _, c := range contribs {
		if c.RewritesInput {
			rewriters = append(rewriters, c.Source)
		}
		for _, f := range c.Files {
			if existing, ok := seen[f.Target]; ok {
				return nil, fmt.Errorf("opencode: composing plugins: both %q and %q declare a file at %q", existing, c.Source, f.Target)
			}
			seen[f.Target] = c.Source
			files = append(files, f)
		}
	}

	if len(rewriters) > 1 {
		return nil, fmt.Errorf("%w: %s", ErrMultipleRewriters, strings.Join(rewriters, ", "))
	}

	return files, nil
}

// Provision implements domain.HarnessAdapter. It generates and installs the
// interception plugin, deploys the logger bundle's OpenCode variant and the
// stub collaborator definitions, all under req.Sandbox only, and refuses
// when more than one plugin able to rewrite the intercepted call's input
// would be present.
//
// On any failure Provision still returns the ledger of what it created
// before the failure, so a caller can tear down exactly that partial state
// through Deprovision; only the error signals that setup did not complete.
func (a *Adapter) Provision(ctx context.Context, req domain.ProvisionRequest) (domain.Provisioning, error) {
	prov := domain.Provisioning{Sandbox: req.Sandbox}

	pluginsDir := filepath.Join(req.Sandbox.SubjectDir, PluginsRelDir)

	// The directory-drop model's second single-rewriter check: presence
	// alone activates a plugin, and there is no document listing what is
	// active, so a plugins directory that already holds an entry this
	// provisioning did not create is refused outright.
	if err := checkNoForeignPlugins(pluginsDir); err != nil {
		return prov, err
	}

	preBridge, err := BuildBridge(req, domain.PhasePre)
	if err != nil {
		return prov, fmt.Errorf("opencode: building pre-invocation bridge: %w", err)
	}
	postBridge, err := BuildBridge(req, domain.PhasePost)
	if err != nil {
		return prov, fmt.Errorf("opencode: building post-invocation bridge: %w", err)
	}

	pluginContent, err := GeneratePlugin(preBridge, postBridge)
	if err != nil {
		return prov, fmt.Errorf("opencode: generating plugin: %w", err)
	}

	contribs := []PluginContribution{
		{
			Source:        "interceptor",
			RewritesInput: true,
			Files:         []PluginFile{{Target: PluginFileName, Content: pluginContent}},
		},
	}

	if req.LoggerBundleDir != "" {
		bundleContrib, err := loadLoggerBundleContribution(req.LoggerBundleDir)
		if err != nil {
			return prov, err
		}
		contribs = append(contribs, bundleContrib)
	}

	files, err := ComposePlugins(contribs...)
	if err != nil {
		return prov, fmt.Errorf("opencode: composing plugins: %w", err)
	}

	for _, f := range files {
		destPath, err := safeJoin(pluginsDir, f.Target)
		if err != nil {
			return prov, fmt.Errorf("opencode: plugin file target %q: %w", f.Target, err)
		}
		if err := writeTracked(&prov, destPath, f.Content); err != nil {
			return prov, err
		}
	}

	for _, c := range req.Collaborators {
		destPath, err := safeJoin(req.Sandbox.SubjectDir, c.TargetPath)
		if err != nil {
			return prov, fmt.Errorf("opencode: stub collaborator definition %q: %w", c.TargetPath, err)
		}
		if err := writeTracked(&prov, destPath, c.Definition); err != nil {
			return prov, err
		}
	}

	if findings, err := a.InspectScopes(ctx); err == nil {
		prov.ScopeFindings = findings
	}

	return prov, nil
}

// checkNoForeignPlugins refuses a plugins directory that already contains
// any entry, since nothing has been written by this provisioning yet and
// any entry present is therefore someone else's.
func checkNoForeignPlugins(pluginsDir string) error {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("opencode: reading plugins directory %q: %w", pluginsDir, err)
	}
	if len(entries) == 0 {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return fmt.Errorf("%w: plugins directory %q already contains %s", ErrMultipleRewriters, pluginsDir, strings.Join(names, ", "))
}

// loadLoggerBundleContribution resolves the logger bundle's OpenCode variant
// through the shared manifest package and reads it as data, exactly as the
// Claude Code adapter does: its file list is never restated here, so adding
// a file to the bundle changes what this deploys with no edit to this
// function.
//
// The variant's registration list is expected empty by design — OpenCode
// plugins are auto-discovered, so the bundle contributes files and no
// registration entries. A non-empty list is a surprise this function must
// not silently ignore: it fails and names it.
func loadLoggerBundleContribution(bundleDir string) (PluginContribution, error) {
	manifest, err := hookbundle.Load(bundleDir)
	if err != nil {
		return PluginContribution{}, fmt.Errorf("opencode: loading logger bundle manifest: %w", err)
	}
	variant, err := manifest.ResolveVariant(HarnessID, "")
	if err != nil {
		return PluginContribution{}, fmt.Errorf("opencode: resolving logger bundle variant for %q: %w", HarnessID, err)
	}
	if len(variant.Registration) != 0 {
		return PluginContribution{}, fmt.Errorf(
			"opencode: logger bundle opencode variant declares a non-empty registration list (%d steps); opencode plugins are auto-discovered by directory presence and must never be registered",
			len(variant.Registration),
		)
	}

	variantDir := filepath.Join(bundleDir, variant.Key)
	var files []PluginFile
	for _, f := range variant.Files {
		srcPath := filepath.Join(variantDir, filepath.FromSlash(f.Source))
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return PluginContribution{}, fmt.Errorf("opencode: reading logger bundle file %q: %w", srcPath, err)
		}
		files = append(files, PluginFile{Target: f.Target, Content: data})
	}

	return PluginContribution{
		Source: manifest.ID,
		// The logger bundle's contribution never rewrites the intercepted
		// call's input, which is what lets it coexist with the interceptor's
		// single rewriting contribution.
		RewritesInput: false,
		Files:         files,
	}, nil
}

// Deprovision implements domain.HarnessAdapter. It removes exactly what
// Provisioning records and nothing else, is idempotent, and removes files
// before directories so children precede parents.
func (a *Adapter) Deprovision(ctx context.Context, p domain.Provisioning) error {
	for i := len(p.Files) - 1; i >= 0; i-- {
		if err := os.Remove(p.Files[i]); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("opencode: removing %q: %w", p.Files[i], err)
		}
	}
	// Dirs is recorded outermost first; removing in reverse order removes
	// children before their parents.
	for i := len(p.Dirs) - 1; i >= 0; i-- {
		if err := os.Remove(p.Dirs[i]); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("opencode: removing directory %q: %w", p.Dirs[i], err)
		}
	}
	return nil
}

// safeJoin joins baseDir with a caller-declared relative path, refusing any
// path that would resolve outside baseDir. The tool never writes outside the
// sandbox on any path, and this is the one place that promise is enforced.
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
	if err := ensureDirsFor(prov, filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("opencode: writing %q: %w", path, err)
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
				return fmt.Errorf("opencode: %q exists and is not a directory", d)
			}
			break
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("opencode: stat %q: %w", d, err)
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
			return fmt.Errorf("opencode: creating directory %q: %w", m, err)
		}
		prov.Dirs = append(prov.Dirs, m)
	}
	return nil
}
