// Package provision owns Rover's Ansible provisioning workflow.
package provision

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/mitchell-wallace/rover/internal/ansible"
	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/shellsafe"
	"github.com/mitchell-wallace/rover/internal/stateutil"
	"github.com/mitchell-wallace/rover/internal/tailscale"
	"github.com/mitchell-wallace/rover/internal/ui"
)

// AzureProvisioner is the Azure subset needed by provisioning.
type AzureProvisioner interface {
	Info() (azure.Info, error)
	SetPublicSSH(allowed bool) error
}

// SSHWaiter waits for SSH to become reachable before provisioning.
type SSHWaiter func(ctx context.Context, host string, port int)

// Service runs Ansible provisioning against a Rover VM.
type Service struct {
	State    *config.State
	Azure    AzureProvisioner
	TS       tailscale.Client
	AssetDir string
	Ansible  func(ansible.Params) error
	Wait     SSHWaiter
	Timezone string
	Locale   string
}

// New constructs a Service with production defaults for injectable seams.
func New(st *config.State, az AzureProvisioner, ts tailscale.Client, assetDir string) *Service {
	return &Service{
		State:    st,
		Azure:    az,
		TS:       ts,
		AssetDir: assetDir,
		Ansible:  ansible.Provision,
		Wait:     defaultSSHWait,
		Timezone: st.EffectiveTimezone(),
		Locale:   st.EffectiveLocale(),
	}
}

// Run provisions the VM with Ansible, then verifies and locks down Tailscale
// access when a Tailscale auth key was used.
func (s *Service) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	info, err := s.Azure.Info()
	if err != nil {
		return err
	}
	if err := requireRunning(info); err != nil {
		return err
	}

	var authKey string
	var usingOAuth bool
	if key := os.Getenv("TS_AUTHKEY"); key != "" {
		authKey = sanitizeAuthKey(key)
		ui.Info("TS_AUTHKEY detected in environment — VM will join your tailnet as %q.", s.State.TSHostname())
	} else if s.State.HasTSOAuth() {
		ui.Info("Generating Tailscale auth key via OAuth client for hostname %q...", s.State.TSHostname())
		key, err := s.TS.GetAuthKey(s.State.TSClientID(), s.State.TSClientSecret(), s.State.TSTagSlice())
		if err != nil {
			return fmt.Errorf("generate tailscale auth key: %w", err)
		}
		authKey = sanitizeAuthKey(key)
		usingOAuth = true
	} else {
		ui.Info("Tailscale credentials not set (TS_AUTHKEY or OAuth client ID/secret) — skipping Tailscale.")
	}

	if authKey != "" {
		if err := os.Setenv("TS_AUTHKEY", authKey); err != nil {
			return fmt.Errorf("set TS_AUTHKEY: %w", err)
		}
		defer func() { _ = os.Unsetenv("TS_AUTHKEY") }()
	}

	host := info.Host()
	tshost := s.State.TSHostname()
	if peer, err := s.TS.FindPeer(tshost); err == nil && peer.Online {
		target := peer.Target()
		ui.Info("Tailscale connection active. Provisioning over Tailscale (%s)...", target)
		host = target
	} else {
		ui.Info("Provisioning %s (%s) over public IP with Ansible...", info.VMName, host)
	}

	s.waiter()(ctx, host, s.State.SSHPort())

	err = s.ansible()(ansible.Params{
		Host:       host,
		User:       s.State.AdminUsername,
		PrivateKey: s.State.PrivateKeyPath(),
		AssetDir:   s.AssetDir,
		ExtraVars: map[string]string{
			"ansible_port":       strconv.Itoa(s.State.SSHPort()),
			"tailscale_hostname": s.State.TSHostname(),
			"tailscale_tags":     s.State.TSTags(),
			"rover_timezone":     s.Timezone,
			"rover_locale":       s.Locale,
		},
	})
	if err != nil {
		return err
	}
	s.State.AnsibleApplied = true
	s.State.Timezone = s.Timezone
	s.State.Locale = s.Locale
	if err := s.State.Save(); err != nil {
		ui.Warn("Failed to save timezone/locale to state: %v", err)
	}
	if err := stateutil.SyncConnection(s.State, info); err != nil {
		return err
	}
	ui.Info("Provisioning complete.")

	if authKey != "" || usingOAuth {
		ui.Info("Verifying Tailscale connection to VM...")
		if peer, err := s.TS.FindPeer(tshost); err == nil && peer.Online && s.TS.PingPeer(peer) {
			ui.Info("Tailscale connection verified.")
			if s.State.PublicSSHClosed {
				ui.Info("Public SSH already closed — VM reachable only over Tailscale.")
			} else {
				ui.Info("Locking down: closing public SSH (VM stays reachable over Tailscale)...")
				if err := s.Azure.SetPublicSSH(false); err != nil {
					ui.Warn("Failed to close public SSH: %v — public SSH left OPEN on port %d.", err, s.State.SSHPort())
				} else {
					s.State.PublicSSHClosed = true
					if err := s.State.Save(); err != nil {
						ui.Warn("Failed to save state after closing public SSH: %v", err)
					} else {
						ui.Info("Public SSH closed. The VM is now reachable only over Tailscale ('rover connect').")
					}
				}
			}
		} else {
			ui.Warn("Tailscale verification failed: peer offline, not found, or unreachable — keeping public SSH OPEN on port %d.", s.State.SSHPort())
		}
	}

	ui.Info("Connect with 'rover ssh' (or 'rover connect' if Tailscale is active) and run 'dune'.")
	return nil
}

func (s *Service) ansible() func(ansible.Params) error {
	if s.Ansible != nil {
		return s.Ansible
	}
	return ansible.Provision
}

func (s *Service) waiter() SSHWaiter {
	if s.Wait != nil {
		return s.Wait
	}
	return defaultSSHWait
}

func sanitizeAuthKey(key string) string {
	clean, stripped := shellsafe.AuthKey(key)
	if stripped {
		ui.Warn("Auth key contained unexpected characters — they were stripped. Use only alphanumeric, '-', or '_'.")
	}
	return clean
}
