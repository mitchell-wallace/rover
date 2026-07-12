package provision

import (
	"context"
	"fmt"
	"time"

	"github.com/mitchell-wallace/rover/internal/ansible"
	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/telemetry"
	"github.com/mitchell-wallace/rover/internal/ui"
)

// ResizeSwapfile runs only the swapfile playbook against the current VM. It is
// intentionally separate from Run so compute resizes never trigger a full
// provisioning pass.
func (s *Service) ResizeSwapfile(ctx context.Context) (err error) {
	started := time.Now()
	phase := "vm_info"
	defer func() {
		sink := s.telemetrySink()
		sink.RecordProvision(telemetry.ProvisionEvent{Mode: "swapfile", Success: err == nil, Duration: time.Since(started)})
		if err != nil {
			sink.RecordDiagnostic(telemetry.DiagnosticEvent{Command: "provision", Category: phase + "_failure"})
		}
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	info, err := s.Azure.Info()
	if err != nil {
		return err
	}
	phase = "vm_state"
	if err := requireRunning(info); err != nil {
		return err
	}

	host := info.Host()
	if peer, findErr := s.TS.FindPeer(s.State.TSHostname()); findErr == nil && peer.Online {
		host = peer.Target()
		ui.Info("Tailscale connection active. Updating swapfile over Tailscale (%s)...", host)
	} else {
		ui.Info("Updating swapfile on %s (%s) over public IP...", info.VMName, host)
	}
	s.waiter()(ctx, host, s.State.SSHPort())
	phase = "ansible"
	if err := s.ansible()(ansible.Params{
		Host:       host,
		User:       s.State.AdminUsername,
		PrivateKey: s.State.PrivateKeyPath(),
		AssetDir:   s.AssetDir,
		Playbook:   "swapfile.yml",
		ExtraVars: map[string]string{
			"ansible_port": fmt.Sprint(s.State.SSHPort()),
		},
	}); err != nil {
		return err
	}
	ui.Info("Swapfile update complete.")
	return nil
}

func requireRunning(info azure.Info) error {
	if !info.Exists {
		return fmt.Errorf("no VM provisioned; run 'rover up' first")
	}
	if !info.Running() {
		return fmt.Errorf("VM is %q, not running; run 'rover up' to start it", info.PowerState)
	}
	return nil
}
