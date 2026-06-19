package connectivity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/shellsafe"
	"github.com/mitchell-wallace/rover/internal/ui"
)

// Reauthenticate repairs Tailscale inside the VM and waits for a reachable peer.
func (s *Service) Reauthenticate(ctx context.Context) bool {
	ctx = contextOrBackground(ctx)

	var authKey string
	if key := os.Getenv("TS_AUTHKEY"); key != "" {
		authKey = sanitizeAuthKey(key)
	} else if s.State.HasTSOAuth() {
		ui.Info("Generating Tailscale auth key...")
		key, err := s.TS.GetAuthKey(s.State.TSClientID(), s.State.TSClientSecret(), s.State.TSTagSlice())
		if err != nil {
			ui.Warn("Failed to generate Tailscale auth key: %v", err)
		} else {
			authKey = sanitizeAuthKey(key)
		}
	}

	if authKey != "" {
		ui.Info("Re-authenticating Tailscale inside the VM...")
		script := buildReauthScript(authKey, s.State.TSHostname(), s.State.TSTags())
		if err := s.Azure.RunCommand(ctx, script); err != nil {
			reportRunCommandFailure(err)
		}

		ui.Info("Waiting for Tailscale peer to come online...")
		tshost := s.State.TSHostname()
		for i := 0; i < s.Poll.Count; i++ {
			if err := ctx.Err(); err != nil {
				ui.Warn("Cancelled while waiting for Tailscale peer.")
				return false
			}
			select {
			case <-ctx.Done():
				ui.Warn("Cancelled while waiting for Tailscale peer.")
				return false
			case <-time.After(s.Poll.Wait):
			}
			if peer, err := s.TS.FindPeer(tshost); err == nil && s.TS.PingPeer(peer) {
				ui.Info("Tailscale re-authenticated — VM reachable via 'rover connect'.")
				return true
			}
		}
		ui.Warn("Tailscale peer did not become reachable after %s.", s.connectivityWaitBudget())
	}

	return false
}

// Restore reopens connectivity when public SSH is locked down.
func (s *Service) Restore(ctx context.Context) error {
	if !s.State.PublicSSHClosed {
		return nil
	}

	ui.Info("Public SSH is locked down — restoring Tailscale connectivity...")
	if s.Reauthenticate(ctx) {
		return nil
	}

	ui.Warn("Opening public SSH as fallback (Tailscale not available).")
	if err := s.Azure.SetPublicSSH(true); err != nil {
		return fmt.Errorf("failed to open public SSH: %w", err)
	}
	s.State.PublicSSHClosed = false
	if err := s.State.Save(); err != nil {
		return fmt.Errorf("save state after opening public SSH: %w", err)
	}
	ui.Info("Public SSH opened on port %d. Run 'rover provision' to re-establish Tailscale.", s.State.SSHPort())
	return nil
}

func sanitizeAuthKey(key string) string {
	clean, stripped := shellsafe.AuthKey(key)
	if stripped {
		ui.Warn("Auth key contained unexpected characters — they were stripped. Use only alphanumeric, '-', or '_'.")
	}
	return clean
}

// buildReauthScript returns the Run Command script used to repair Tailscale
// inside the VM. The daemon is restarted first so a wedged tailscaled (alive on
// its socket but not establishing a data plane) reloads its existing node
// credentials instead of minting a duplicate node. Every invocation is bounded
// by timeout(1) so a stuck daemon cannot pin the Run Command extension for
// Azure's ~90 minute script ceiling, and the final `tailscale up` does not
// swallow its exit code so real failures propagate to the caller.
func buildReauthScript(authKey, hostname, tags string) string {
	return fmt.Sprintf(`if ! command -v tailscale >/dev/null 2>&1; then
  echo 'tailscale CLI not installed on VM' >&2
  exit 127
fi
sudo timeout 60s systemctl restart tailscaled 2>&1 || true
sleep 3
sudo timeout 120s tailscale up --authkey='%s' --ssh --hostname='%s' --advertise-tags='%s'`,
		authKey, shellsafe.ShellArg(hostname), shellsafe.ShellArg(tags))
}

// reportRunCommandFailure surfaces a RunCommand error to the user. When the
// azure boundary classified it (conflict/transient/guest-failure), the captured
// guest output is printed so the user sees the real cause (invalid auth key,
// unauthorized tag, tailscaled down) instead of a bare exit code.
func reportRunCommandFailure(err error) {
	var rcErr *azure.RunCommandError
	if !errors.As(err, &rcErr) {
		ui.Warn("Tailscale re-auth via Azure Run Command failed: %v", err)
		return
	}
	attempt := "attempt"
	if rcErr.Attempts != 1 {
		attempt = "attempts"
	}
	ui.Warn("Tailscale re-auth via Azure Run Command failed (%s after %d %s).", rcErr.Kind, rcErr.Attempts, attempt)
	for _, line := range strings.Split(strings.TrimSpace(rcErr.Output), "\n") {
		if line != "" {
			fmt.Println("  " + line)
		}
	}
}

func (s *Service) connectivityWaitBudget() time.Duration {
	return (time.Duration(s.Poll.Count) * s.Poll.Wait).Round(time.Second)
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
