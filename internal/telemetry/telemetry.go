// Package telemetry provides best-effort Rover usage telemetry.
package telemetry

import (
	"os"
	"strings"
	"time"
	"unicode/utf8"

	nr "github.com/newrelic/go-agent/v3/newrelic"
)

const (
	EventRoverUp         = "RoverUp"
	EventRoverProvision  = "RoverProvision"
	EventRoverDiagnostic = "RoverDiagnostic"

	envLicenseKey = "ROVER_NEW_RELIC_LICENSE_KEY"
	envAppName    = "ROVER_NEW_RELIC_APP_NAME"

	defaultAppName = "Rover CLI"
	flushTimeout   = 2 * time.Second
)

// Config contains fallback values used when Rover-prefixed environment
// variables are unset. Environment values take precedence.
type Config struct {
	LicenseKey      string
	AppName         string
	ShutdownTimeout time.Duration
}

// UpEvent describes one VM create, start, redeploy, or resize attempt.
type UpEvent struct {
	ComputeFamily       string
	ComputeSize         string
	SelectionSource     string
	ProvisioningOutcome string
	Success             bool
	Duration            time.Duration
}

// ProvisionEvent describes one full or swapfile-only provisioning attempt.
type ProvisionEvent struct {
	Mode     string
	Success  bool
	Duration time.Duration
}

// DiagnosticEvent contains stable classifications only, never paths or raw
// provider error text.
type DiagnosticEvent struct {
	Command  string
	Category string
}

// Sink records Rover custom events. Implementations are safe to call when
// telemetry is disabled.
type Sink interface {
	RecordUp(UpEvent)
	RecordProvision(ProvisionEvent)
	RecordDiagnostic(DiagnosticEvent)
}

type NoopSink struct{}

func (NoopSink) RecordUp(UpEvent)                 {}
func (NoopSink) RecordProvision(ProvisionEvent)   {}
func (NoopSink) RecordDiagnostic(DiagnosticEvent) {}

type newRelicSink struct{ app *nr.Application }

// Init conditionally initializes New Relic. With no license key it returns a
// no-op sink without constructing an agent. Agent errors are deliberately
// swallowed so observability can never prevent Rover from running.
func Init(cfg Config) (Sink, func()) {
	license, appName := resolveConfig(cfg)
	if license == "" {
		return NoopSink{}, func() {}
	}

	app, err := nr.NewApplication(
		nr.ConfigLicense(license),
		nr.ConfigAppName(appName),
		nr.ConfigEnabled(true),
	)
	if err != nil {
		return NoopSink{}, func() {}
	}

	timeout := cfg.ShutdownTimeout
	if timeout <= 0 {
		timeout = flushTimeout
	}
	return &newRelicSink{app: app}, func() { app.Shutdown(timeout) }
}

func resolveConfig(cfg Config) (license string, appName string) {
	license = strings.TrimSpace(os.Getenv(envLicenseKey))
	if license == "" {
		license = strings.TrimSpace(cfg.LicenseKey)
	}
	appName = strings.TrimSpace(os.Getenv(envAppName))
	if appName == "" {
		appName = strings.TrimSpace(cfg.AppName)
	}
	if appName == "" {
		appName = defaultAppName
	}
	return license, cleanAttribute(appName)
}

func (s *newRelicSink) RecordUp(event UpEvent) {
	if s == nil || s.app == nil {
		return
	}
	s.app.RecordCustomEvent(EventRoverUp, upAttributes(event))
}

func (s *newRelicSink) RecordProvision(event ProvisionEvent) {
	if s == nil || s.app == nil {
		return
	}
	s.app.RecordCustomEvent(EventRoverProvision, provisionAttributes(event))
}

func (s *newRelicSink) RecordDiagnostic(event DiagnosticEvent) {
	if s == nil || s.app == nil {
		return
	}
	s.app.RecordCustomEvent(EventRoverDiagnostic, diagnosticAttributes(event))
}

func upAttributes(event UpEvent) map[string]interface{} {
	return map[string]interface{}{
		"compute_family":       cleanAttribute(event.ComputeFamily),
		"compute_size":         cleanAttribute(event.ComputeSize),
		"selection_source":     cleanAttribute(event.SelectionSource),
		"provisioning_outcome": cleanAttribute(event.ProvisioningOutcome),
		"success":              event.Success,
		"duration_ms":          event.Duration.Milliseconds(),
	}
}

func provisionAttributes(event ProvisionEvent) map[string]interface{} {
	return map[string]interface{}{
		"mode": cleanAttribute(event.Mode), "success": event.Success,
		"duration_ms": event.Duration.Milliseconds(),
	}
}

func diagnosticAttributes(event DiagnosticEvent) map[string]interface{} {
	return map[string]interface{}{
		"command": cleanAttribute(event.Command), "category": cleanAttribute(event.Category),
	}
}

func cleanAttribute(value string) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "�")
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		value = strings.ReplaceAll(value, home, "~")
	}
	const maxBytes = 1024
	if len(value) > maxBytes {
		value = value[:maxBytes]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}
