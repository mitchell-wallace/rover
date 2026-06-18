package connectivity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/tailscale"
)

// AzureControl is the subset of Azure operations used by connectivity repair
// and command routing.
type AzureControl interface {
	Status() (azure.Info, error)
	SetPublicSSH(allowed bool) error
	RunCommand(script string) error
}

// CommandRunner runs an external command with the user's stdio attached.
type CommandRunner func(name string, args ...string) error

// PollConfig controls bounded Tailscale repair polling.
type PollConfig struct {
	Count int
	Wait  time.Duration
}

// DefaultPoll matches the legacy restoreConnectivity poll policy.
var DefaultPoll = PollConfig{Count: 12, Wait: 5 * time.Second}

// ReconnectConfig controls post-drop Tailscale SSH reconnects.
type ReconnectConfig struct {
	MaxConsecutive int
	BaseWait       time.Duration
	MaxWait        time.Duration
	HealthyAfter   time.Duration
}

// DefaultReconnect matches the legacy connectReconnect policy.
var DefaultReconnect = ReconnectConfig{
	MaxConsecutive: 5,
	BaseWait:       2 * time.Second,
	MaxWait:        30 * time.Second,
	HealthyAfter:   time.Minute,
}

// Restarter repairs a wedged Tailscale data plane by restarting the VM. It is
// optional because the package cannot import cmd or vm during the extraction.
type Restarter func() error

// Service owns Tailscale readiness, repair, routing, and command execution.
type Service struct {
	State     *config.State
	Azure     AzureControl
	TS        tailscale.Client
	Run       CommandRunner
	Poll      PollConfig
	Reconnect ReconnectConfig
	Restart   Restarter
}

// New builds a connectivity service with production timing and exec defaults.
func New(st *config.State, az AzureControl, ts tailscale.Client) *Service {
	return &Service{
		State:     st,
		Azure:     az,
		TS:        ts,
		Run:       defaultCommandRunner,
		Poll:      DefaultPoll,
		Reconnect: DefaultReconnect,
	}
}

// Ready reports whether local Tailscale credentials/configuration are usable.
func (s *Service) Ready() bool {
	if s.State == nil || s.TS == nil {
		return false
	}
	if os.Getenv("TS_AUTHKEY") == "" && !s.State.HasTSOAuth() {
		return false
	}
	_, err := s.TS.FindPeer(s.State.TSHostname())
	if err == nil {
		return true
	}
	var notFound *tailscale.PeerNotFoundError
	return errors.As(err, &notFound)
}

func defaultCommandRunner(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
