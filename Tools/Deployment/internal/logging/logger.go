package logging

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
)

// options holds the optional configuration for a Logger constructed by New.
type options struct {
	logDir string
}

// Option configures a Logger constructed by New.
type Option func(*options)

// WithLogDir overrides the directory the two sink files are written to.
//
// The empty string selects the default, <mosaicRoot>/MosaicDeploy/logs — the
// location this package's own doc comment calls fixed by contract, and which
// other tooling may read. Backward compatibility is absolute: an invocation
// that supplies no override writes to the same two paths, with the same
// truncate-at-run-start and append behaviour, as it does today.
//
// A supplied directory is created on demand, and write failures against it
// accumulate in Degraded rather than being returned, exactly as they do for
// the default location.
func WithLogDir(dir string) Option {
	return func(o *options) {
		o.logDir = dir
	}
}

// loggerImpl is the concrete Logger implementation that writes to two sink files.
type loggerImpl struct {
	latestPath  string
	historyPath string
	degraded    []error
}

// New constructs a Logger that writes to the two sinks under mosaicRoot, or
// under the directory WithLogDir supplies.
//
// Sink locations:
//
//	<logDir>/latest.log   truncated at StartRun
//	<logDir>/history.log  appended
//
// where logDir defaults to <mosaicRoot>/MosaicDeploy/logs.
//
// The parameter is variadic so every existing call site compiles unchanged.
func New(mosaicRoot string, cfg config.ToolConfig, opts ...Option) Logger {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}
	logDir := o.logDir
	if logDir == "" {
		logDir = filepath.Join(mosaicRoot, "MosaicDeploy", "logs")
	}
	return &loggerImpl{
		latestPath:  filepath.Join(logDir, "latest.log"),
		historyPath: filepath.Join(logDir, "history.log"),
	}
}

// Paths returns the absolute paths of the two sink files.
func (l *loggerImpl) Paths() domain.LogPaths {
	return domain.LogPaths{
		Latest:  l.latestPath,
		History: l.historyPath,
	}
}

// Degraded returns all non-fatal write failures accumulated during this run.
func (l *loggerImpl) Degraded() []error {
	return l.degraded
}

// writeBoth appends content to both sinks. For the latest sink, truncate controls whether
// the file is overwritten from scratch (true at StartRun) or appended (false thereafter).
// All errors are accumulated in Degraded rather than returned (CD-10, AC15.4).
func (l *loggerImpl) writeBoth(content string, truncateLatest bool) {
	if err := writeToFile(l.latestPath, content, truncateLatest); err != nil {
		l.degraded = append(l.degraded, err)
	}
	if err := writeToFile(l.historyPath, content, false); err != nil {
		l.degraded = append(l.degraded, err)
	}
}

// StartRun writes the run header to both sinks. The latest sink is truncated first so it
// contains only the current run; the history sink is appended (AC15.1).
func (l *loggerImpl) StartRun(rc RunContext) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "=== RUN %s mode=%s harness=%s tier=%s workspace=%s fallback=%s started=%s ===\n",
		rc.RunID,
		string(rc.Mode),
		rc.Harness.ID,
		string(rc.Harness.Tier),
		rc.WorkspacePath,
		string(rc.Fallback),
		rc.StartedAt.Format(time.RFC3339))
	if rc.Harness.SourcePath != "" {
		fmt.Fprintf(&sb, "harness_source=%s\n", rc.Harness.SourcePath)
	}
	if rc.External != nil {
		fmt.Fprintf(&sb, "external_harness=%s executable=%s protocol=%s\n",
			rc.External.HarnessID,
			rc.External.ExecutablePath,
			rc.External.ProtocolVersion)
	}
	if rc.DeploymentRoot != "" {
		fmt.Fprintf(&sb, "deployment_root=%s\n", rc.DeploymentRoot)
	}
	l.writeBoth(sb.String(), true)
}

// Event records one structured diagnostic event within the current run (AC15.2).
func (l *loggerImpl) Event(e Event) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[%s %s %s %s] %s\n",
		e.Time.Format(time.RFC3339),
		string(e.Level),
		e.Kind,
		e.Subject,
		e.Message)
	for _, delta := range e.Changes {
		fmt.Fprintf(&sb, "  change %s %s->%s\n", delta.Field, delta.Deployed, delta.Source)
	}
	l.writeBoth(sb.String(), false)
}

// Action records the outcome of one deployment item, naming every version field that drove
// an update and its before/after values (AC15.2). Skipped and backed-up files appear with
// their key and backup path (AC15.3).
func (l *loggerImpl) Action(ar domain.ActionRecord) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[action] %s %s target=%s",
		ar.Ref.String(),
		string(ar.Taken),
		ar.TargetPath)
	for _, delta := range ar.Stale {
		fmt.Fprintf(&sb, " delta_%s %s->%s", delta.Field, delta.Deployed, delta.Source)
	}
	if ar.BackupPath != "" {
		fmt.Fprintf(&sb, " backup=%s", ar.BackupPath)
	}
	if ar.Err != "" {
		fmt.Fprintf(&sb, " error=%s", ar.Err)
	}
	sb.WriteString("\n")
	l.writeBoth(sb.String(), false)
}

// EndRun writes the run outcome to both sinks.
func (l *loggerImpl) EndRun(outcome domain.Outcome) {
	content := fmt.Sprintf("=== END outcome=%s ===\n", string(outcome))
	l.writeBoth(content, false)
}
