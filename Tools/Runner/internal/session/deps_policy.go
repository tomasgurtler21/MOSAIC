package session

import (
	"errors"
	"fmt"

	"mosaic-run/internal/domain"
)

// ErrMissingPort is the class sentinel for a required session port that is
// not wired. Every *MissingPortError matches it via errors.Is.
var ErrMissingPort = errors.New("session: required port not wired")

// Port names a session.Deps field in dependency-policy errors. The values equal
// the Go field names; the declaration order below is the reporting order.
type Port string

const (
	PortHarness    Port = "Harness"
	PortStore      Port = "Store"
	PortClock      Port = "Clock"
	PortInteract   Port = "Interact"
	PortPreConsult Port = "PreConsult"
	PortManual     Port = "Manual"
)

// MissingPortError reports that a port the run requires is not wired.
type MissingPortError struct {
	Port       Port   // which Deps field is nil
	Capability string // user-facing capability name
	Reason     string // why it is required for this run
}

// Error implements error. It names the capability and the Deps field.
func (e *MissingPortError) Error() string {
	return e.Capability + " is not available (session Deps." + string(e.Port) + " is not wired): " + e.Reason
}

// Unwrap returns ErrMissingPort so errors.Is matches the class sentinel.
func (e *MissingPortError) Unwrap() error { return ErrMissingPort }

// CheckRequired reports every always-required port that is nil: Harness,
// Store, Clock, Interact. It returns nil when all are wired; otherwise an
// errors.Join of one *MissingPortError per missing port, in Port declaration
// order. It reads only d and has no side effects.
func (d Deps) CheckRequired() error {
	const reason = "always required"
	var errs []error
	if d.Harness == nil {
		errs = append(errs, &MissingPortError{Port: PortHarness, Capability: "harness adapter", Reason: reason})
	}
	if d.Store == nil {
		errs = append(errs, &MissingPortError{Port: PortStore, Capability: "artifact store", Reason: reason})
	}
	if d.Clock == nil {
		errs = append(errs, &MissingPortError{Port: PortClock, Capability: "clock", Reason: reason})
	}
	if d.Interact == nil {
		errs = append(errs, &MissingPortError{Port: PortInteract, Capability: "user interaction", Reason: reason})
	}
	return errors.Join(errs...)
}

// CheckForSettings reports every port that the given effective settings
// require but d does not wire: PreConsult when pre-consultation is enabled in
// auto or auto-review mode, Manual when manual resolution is enabled. Same
// return shape as CheckRequired.
func (d Deps) CheckForSettings(settings domain.RunSettings) error {
	var errs []error
	if d.PreConsult == nil && settings.PreConsultation && preConsultMode(settings.Mode) {
		errs = append(errs, newPreConsultMissing(settings.Mode))
	}
	if d.Manual == nil && settings.ManualResolution {
		errs = append(errs, newManualMissing())
	}
	return errors.Join(errs...)
}

// preConsultMode reports whether pre-consultation applies in the given mode.
func preConsultMode(mode domain.ExecutionMode) bool {
	return mode == domain.ExecutionModeAuto || mode == domain.ExecutionModeAutoReview
}

func newPreConsultMissing(mode domain.ExecutionMode) *MissingPortError {
	return &MissingPortError{
		Port:       PortPreConsult,
		Capability: "pre-consultation",
		Reason:     fmt.Sprintf("the run's effective settings enable pre-consultation in %s mode", mode),
	}
}

func newManualMissing() *MissingPortError {
	return &MissingPortError{
		Port:       PortManual,
		Capability: "manual resolution",
		Reason:     "the run's effective settings enable manual resolution",
	}
}
