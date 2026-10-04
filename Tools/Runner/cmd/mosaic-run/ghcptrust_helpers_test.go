package main

// ghcptrust_helpers_test.go holds the shared fixtures of the trust preflight tests.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	commonharness "mosaic-common/harness"
	"mosaic-common/interaction"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/ghcptrust"
)

// MockTrustChecker returns a scripted result and records every checked directory.
type MockTrustChecker struct {
	Result  ghcptrust.Result
	OnCheck func(workDir string)
	Checked []string
}

func (m *MockTrustChecker) Check(workDir string) ghcptrust.Result {
	m.Checked = append(m.Checked, workDir)
	if m.OnCheck != nil {
		m.OnCheck(workDir)
	}
	return m.Result
}

// MockInteraction records notices and confirm questions and answers Confirm
// with a scripted answer.
type MockInteraction struct {
	mainTestNoopInteraction

	ConfirmReply interaction.ConfirmAnswer
	ConfirmErr   error

	Notices   []interaction.Notice
	Questions []interaction.Question
}

func (m *MockInteraction) Confirm(_ context.Context, q interaction.Question) (interaction.ConfirmAnswer, error) {
	m.Questions = append(m.Questions, q)
	return m.ConfirmReply, m.ConfirmErr
}

func (m *MockInteraction) Notify(_ context.Context, n interaction.Notice) {
	m.Notices = append(m.Notices, n)
}

func trustResult(status ghcptrust.Status) ghcptrust.Result {
	r := ghcptrust.Result{Status: status, ConfigPath: filepath.Join("copilot-home", "config.json")}
	if status == ghcptrust.StatusIndeterminate {
		r.Reason = "config.json is missing"
	}
	return r
}

// newTrustConfig builds a preflight config for the GHCP CLI harness.
func newTrustConfig(runFolder string, checker trustChecker, in domain.Interaction, interactive bool) ghcpTrustConfig {
	return ghcpTrustConfig{
		RunFolder:   runFolder,
		HarnessID:   commonharness.HarnessIDGHCPCLI,
		Checker:     checker,
		Interact:    in,
		Interactive: interactive,
		Debug:       domain.NopDebugLogger{},
	}
}

// assertWarning fails unless n is a warning that names workDir, the lost
// hook logging and Copilot.
func assertWarning(t *testing.T, n interaction.Notice, workDir string) {
	t.Helper()
	if n.Level != interaction.NoticeWarning {
		t.Errorf("notice level = %q, want warning", n.Level)
	}
	if n.Title == "" {
		t.Error("notice title is empty")
	}
	lower := strings.ToLower(n.Message)
	if !strings.Contains(n.Message, workDir) {
		t.Errorf("notice message %q does not name the folder %q", n.Message, workDir)
	}
	if !strings.Contains(lower, "hook") || !strings.Contains(lower, "copilot") {
		t.Errorf("notice message %q must mention hook logging and Copilot", n.Message)
	}
}

var errScriptedStart = errors.New("scripted start failure")
