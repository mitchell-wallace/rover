package connectivity

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mitchell-wallace/rover/internal/tailscale"
	"github.com/mitchell-wallace/rover/internal/ui"
)

// Connect opens an interactive Tailscale SSH session, repairing data-plane
// failures and reconnecting after dropped sessions.
func (s *Service) Connect(ctx context.Context, extra ...string) error {
	ctx = contextOrBackground(ctx)
	host := s.State.TSHostname()
	peer, err := s.TS.FindPeer(host)
	if err != nil {
		var notFound *tailscale.PeerNotFoundError
		switch {
		case errors.Is(err, tailscale.ErrNotInstalled):
			return err
		case errors.Is(err, tailscale.ErrNotRunning):
			return err
		case errors.As(err, &notFound):
			ui.Warn("%v.", err)
			ui.Info("If the VM is up, provision it with Tailscale: TS_AUTHKEY=<key> rover provision")
			ui.Info("Otherwise start it with 'rover up'. Plain SSH still works via 'rover ssh'.")
			return fmt.Errorf("%q not reachable over Tailscale", host)
		default:
			return err
		}
	}
	if !peer.Online {
		ui.Warn("%q is in your tailnet but offline (likely deallocated).", host)
		ui.Info("Start it with 'rover up'.")
		return fmt.Errorf("%q is offline", host)
	}
	if !s.TS.PingPeer(peer) {
		ui.Warn("%q is online in Tailscale but not reachable on the data plane.", host)
		repairCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		if s.Reauthenticate(repairCtx) {
			repairedPeer, err := s.TS.FindPeer(host)
			if err == nil && repairedPeer.Online && s.TS.PingPeer(repairedPeer) {
				target := repairedPeer.Target()
				ui.Info("Connecting over Tailscale to %s@%s...", s.State.AdminUsername, target)
				return s.connectWithReconnect(ctx, target, extra...)
			}
		}
		if ok, cerr := ui.Confirm(
			"Restart the VM to repair Tailscale?",
			"A reboot restarts the Tailscale daemon inside the VM, which usually restores the data plane. rover reconnects automatically afterward.",
			false,
		); cerr == nil && ok && s.Restart != nil {
			if rerr := s.Restart(); rerr == nil {
				if repairedPeer, ferr := s.TS.FindPeer(host); ferr == nil && repairedPeer.Online && s.TS.PingPeer(repairedPeer) {
					target := repairedPeer.Target()
					ui.Info("Connecting over Tailscale to %s@%s...", s.State.AdminUsername, target)
					return s.connectWithReconnect(ctx, target, extra...)
				}
			} else {
				ui.Warn("Restart attempt failed: %v", rerr)
			}
		}
		ui.Info("Run 'rover restart' to repair Tailscale or temporarily open public SSH.")
		return fmt.Errorf("%q is not reachable over Tailscale", host)
	}

	target := peer.Target()
	ui.Info("Connecting over Tailscale to %s@%s...", s.State.AdminUsername, target)
	return s.connectWithReconnect(ctx, target, extra...)
}

// RunCommand routes a remote command over Tailscale when available, otherwise
// over public SSH.
func (s *Service) RunCommand(ctx context.Context, args []string) error {
	ctx = contextOrBackground(ctx)
	info, err := s.Azure.Status()
	if err != nil {
		return err
	}
	if !info.Exists {
		return fmt.Errorf("no VM provisioned; run 'rover up' first")
	}
	if !info.Running() {
		return fmt.Errorf("VM is %q, not running; run 'rover up' to start it", info.PowerState)
	}

	cmdStr := strings.Join(args, " ")
	runFn := s.Run
	if runFn == nil {
		runFn = defaultCommandRunner
	}

	if peer, perr := s.TS.FindPeer(s.State.TSHostname()); perr == nil && peer != nil {
		if s.TS.PingPeer(peer) {
			target := peer.Target()
			ui.Info("Running over Tailscale (%s): %s", target, cmdStr)
			return runFn("tailscale", "ssh", s.State.AdminUsername+"@"+target, "--", cmdStr)
		}
		if peer.Online {
			ui.Warn("Tailscale peer is online but unreachable.")
			if s.State.PublicSSHClosed {
				restoreCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				defer cancel()
				if err := s.Restore(restoreCtx); err != nil {
					return err
				}
				if repairedPeer, err := s.TS.FindPeer(s.State.TSHostname()); err == nil && s.TS.PingPeer(repairedPeer) {
					target := repairedPeer.Target()
					ui.Info("Running over Tailscale (%s): %s", target, cmdStr)
					return runFn("tailscale", "ssh", s.State.AdminUsername+"@"+target, "--", cmdStr)
				}
			}
			ui.Warn("Falling back to public SSH.")
		}
	}

	host := info.Host()
	if host == "" {
		return fmt.Errorf("no connection target; run 'rover up' first")
	}
	ui.Info("Running over SSH (%s): %s", host, cmdStr)

	sshArgs := []string{
		"-p", strconv.Itoa(s.State.SSHPort()),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
	}
	if pk := s.State.PrivateKeyPath(); pk != "" {
		sshArgs = append(sshArgs, "-i", pk)
	}
	sshArgs = append(sshArgs, s.State.AdminUsername+"@"+host, "--", cmdStr)
	return runFn("ssh", sshArgs...)
}

func (s *Service) connectWithReconnect(ctx context.Context, target string, extra ...string) error {
	consecutiveFailures := 0
	for {
		started := time.Now()
		err := s.TS.Connect(s.State.AdminUsername, target, extra...)
		if err == nil {
			return nil
		}
		if time.Since(started) >= s.Reconnect.HealthyAfter {
			consecutiveFailures = 0
		}
		consecutiveFailures++
		if consecutiveFailures > s.Reconnect.MaxConsecutive {
			return fmt.Errorf("tailscale ssh disconnected after %d reconnect attempts: %w", s.Reconnect.MaxConsecutive, err)
		}

		delay := s.connectReconnectDelay(consecutiveFailures)
		ui.Warn("Tailscale SSH disconnected: %v", err)
		ui.Info("Reconnecting over Tailscale in %s (Ctrl-C to stop)...", delay)
		if err := sleepContext(ctx, delay); err != nil {
			return err
		}

		peer, perr := s.reconnectableTailscalePeer(ctx)
		if perr != nil {
			return fmt.Errorf("tailscale ssh disconnected and reconnect check failed: %w", perr)
		}
		target = peer.Target()
		ui.Info("Reconnecting over Tailscale to %s@%s...", s.State.AdminUsername, target)
	}
}

func (s *Service) connectReconnectDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := s.Reconnect.BaseWait
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= s.Reconnect.MaxWait {
			return s.Reconnect.MaxWait
		}
	}
	return delay
}

func (s *Service) reconnectableTailscalePeer(ctx context.Context) (*tailscale.Peer, error) {
	host := s.State.TSHostname()
	peer, err := s.TS.FindPeer(host)
	if err != nil {
		return nil, err
	}
	if !peer.Online {
		return nil, fmt.Errorf("%q is offline", host)
	}
	if s.TS.PingPeer(peer) {
		return peer, nil
	}

	ui.Warn("%q is online in Tailscale but not reachable on the data plane.", host)
	repairCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if s.Reauthenticate(repairCtx) {
		repairedPeer, err := s.TS.FindPeer(host)
		if err == nil && repairedPeer.Online && s.TS.PingPeer(repairedPeer) {
			return repairedPeer, nil
		}
	}
	return nil, fmt.Errorf("%q is not reachable over Tailscale", host)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
