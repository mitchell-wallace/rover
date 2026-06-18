package vm

import (
	"fmt"

	"github.com/mitchell-wallace/rover/internal/tailscale"
	"github.com/mitchell-wallace/rover/internal/ui"
)

func tailscaleLogoutScript() string {
	return `if command -v tailscale >/dev/null 2>&1; then
  tailscale logout || true
  systemctl stop tailscaled || true
  systemctl disable tailscaled || true
fi`
}

// CleanupTailscaleDevices removes Rover's stale Tailscale devices using the
// configured OAuth client.
func (s *Service) CleanupTailscaleDevices(deleteOnline, dryRun bool) (tailscale.CleanupResult, error) {
	if !s.State.HasTSOAuth() {
		return tailscale.CleanupResult{}, fmt.Errorf("tailscale OAuth credentials not configured; set them with 'rover config --edit'")
	}
	res, err := s.TS.CleanupDevices(
		s.State.TSClientID(),
		s.State.TSClientSecret(),
		s.State.TSTagSlice(),
		s.State.TSHostname(),
		deleteOnline,
		dryRun,
	)
	if err != nil {
		return res, err
	}
	printTailscaleCleanupResult(res, dryRun)
	return res, nil
}

func printTailscaleCleanupResult(res tailscale.CleanupResult, dryRun bool) {
	if len(res.Matched) == 0 {
		ui.Info("No matching Rover Tailscale devices found.")
		return
	}
	for _, d := range res.Deleted {
		ui.Info("Deleted Tailscale device: %s", d.DisplayName())
	}
	for _, d := range res.WouldDelete {
		ui.Info("Would delete Tailscale device: %s", d.DisplayName())
	}
	for _, d := range res.Skipped {
		if dryRun {
			ui.Info("Would skip online Tailscale device: %s", d.DisplayName())
		} else {
			ui.Info("Skipped online Tailscale device: %s", d.DisplayName())
		}
	}
	ui.Info("Tailscale cleanup: matched=%d deleted=%d would-delete=%d skipped=%d", len(res.Matched), len(res.Deleted), len(res.WouldDelete), len(res.Skipped))
}
