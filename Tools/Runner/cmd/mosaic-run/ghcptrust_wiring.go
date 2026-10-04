package main

// ghcptrust_wiring.go holds the Copilot trust preflight session wrapper.
//
// GHCP CLI silently skips repo hooks in a folder that is not a Copilot trusted
// folder, so no MOSAIC hook logs are written. The preflight warns about that at
// every session start and, in the TUI, lets the user proceed or abort.

import (
	"context"
	"fmt"
	"path/filepath"

	commonharness "mosaic-common/harness"
	"mosaic-common/interaction"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/ghcptrust"
	"mosaic-run/internal/session"
)

// trustChecker is the narrow port the preflight uses (*ghcptrust.Checker satisfies it).
type trustChecker interface {
	Check(workDir string) ghcptrust.Result
}

// ghcpTrustConfig is the per-session input of the preflight.
type ghcpTrustConfig struct {
	RunFolder   string             // workDir = filepath.Dir(RunFolder)
	HarnessID   string             // check runs only for the GHCP CLI harness
	Checker     trustChecker       // production: ghcptrust.NewChecker()
	Interact    domain.Interaction // Notify for warnings; Confirm only when Interactive
	Interactive bool               // CLI: false; TUI: true
	Debug       domain.DebugLogger // diagnostic entries
}

type ghcpTrustPreflight struct {
	inner session.Session
	cfg   ghcpTrustConfig
}

// newGHCPTrustPreflight returns a session wrapper placed inside the lifecycle decorator.
func newGHCPTrustPreflight(inner session.Session, cfg ghcpTrustConfig) session.Session {
	return &ghcpTrustPreflight{inner: inner, cfg: cfg}
}

// withGHCPTrustPreflight wraps inner with a production preflight for the harness.
func withGHCPTrustPreflight(inner session.Session, runFolder, harnessID string, in domain.Interaction, interactive bool, debug domain.DebugLogger) session.Session {
	return newGHCPTrustPreflight(inner, ghcpTrustConfig{
		RunFolder:   runFolder,
		HarnessID:   harnessID,
		Checker:     ghcptrust.NewChecker(),
		Interact:    in,
		Interactive: interactive,
		Debug:       debug,
	})
}

func (p *ghcpTrustPreflight) Start(ctx context.Context, rc domain.RunConfig) (domain.RunOutcome, error) {
	if p.cfg.HarnessID != commonharness.HarnessIDGHCPCLI {
		return p.inner.Start(ctx, rc)
	}
	workDir := filepath.Dir(p.cfg.RunFolder)
	res := p.cfg.Checker.Check(workDir)
	p.cfg.Debug.Log("ghcptrust.check", string(res.Status),
		domain.F("work_dir", workDir), domain.F("config_path", res.ConfigPath), domain.F("reason", res.Reason))

	switch res.Status {
	case ghcptrust.StatusUntrusted:
		p.cfg.Interact.Notify(ctx, interaction.Notice{
			Level: interaction.NoticeWarning,
			Title: "Folder is not a Copilot trusted folder",
			Message: fmt.Sprintf("%s is not a Copilot trusted folder, so GitHub Copilot CLI will not load repo hooks "+
				"and MOSAIC hook logging will not occur. To fix it, trust the folder in Copilot CLI "+
				"(Runner never changes your Copilot configuration).", workDir),
		})
		if p.cfg.Interactive && !p.userChoosesProceed(ctx, workDir) {
			return domain.RunOutcome{
				Status:  domain.RunRefused,
				Message: "Aborted by user because the folder is not a Copilot trusted folder: " + workDir,
			}, nil
		}
	case ghcptrust.StatusIndeterminate:
		p.cfg.Interact.Notify(ctx, interaction.Notice{
			Level: interaction.NoticeWarning,
			Title: "Copilot folder trust could not be determined",
			Message: fmt.Sprintf("Could not determine whether %s is a Copilot trusted folder (%s). "+
				"If it is not trusted, GitHub Copilot CLI will not load repo hooks and MOSAIC hook logging will not occur. "+
				"Continuing.", workDir, res.Reason),
		})
	}
	return p.inner.Start(ctx, rc)
}

// userChoosesProceed asks whether to continue without hook logging. Only an
// explicit "no" or a cancel aborts; a failed prompt proceeds.
func (p *ghcpTrustPreflight) userChoosesProceed(ctx context.Context, workDir string) bool {
	ans, err := p.cfg.Interact.Confirm(ctx, interaction.Question{
		Subject: workDir,
		Title:   "Proceed without MOSAIC hook logging?",
		Prompt:  "This folder is not a Copilot trusted folder, so no MOSAIC hook logs will be written. Proceed anyway?",
	})
	if err != nil {
		p.cfg.Debug.Log("ghcptrust.confirm_error", err.Error())
		return true
	}
	switch ans.Status {
	case interaction.Answered:
		return ans.Confirm
	case interaction.Cancelled:
		return false
	default:
		return true
	}
}
