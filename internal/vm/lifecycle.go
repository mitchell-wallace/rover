package vm

import (
	"context"
	"fmt"
	"time"

	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/sizes"
	"github.com/mitchell-wallace/rover/internal/stateutil"
	"github.com/mitchell-wallace/rover/internal/ui"
)

// Up starts or redeploys the Rover VM and optionally provisions a fresh VM.
func (s *Service) Up(ctx context.Context, family, size string, assumeYes, noProvision bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	family = sizes.NormalizeFamily(family)
	if err := sizes.Validate(family, size); err != nil {
		return err
	}
	if err := config.ValidateAdminUsername(s.State.AdminUsername); err != nil {
		return fmt.Errorf("%w (fix with 'rover config --edit')", err)
	}
	profile, _ := sizes.Get(family, size)
	ui.Info("Selected family: %s", sizes.DescribeFamily(family))
	ui.Info("Selected size: %s", profile.Describe())
	ui.Info("Destination: %s / %s in %s as user %q (disk %d GiB)",
		s.State.ResourceGroup, s.State.VMName, s.State.Location, s.State.AdminUsername, s.State.DiskGB())

	current, err := s.Azure.Status()
	fresh := err == nil && !current.Exists
	if err == nil && current.Running() && current.VMSize != "" && current.VMSize != profile.SKU {
		ui.Warn("A VM is already running as %s. Rover manages one VM at a time;", current.VMSize)
		ui.Warn("continuing will redeploy/resize it in place to %s.", profile.SKU)
	}

	willProvision := fresh && !noProvision
	if willProvision && !s.Conn.Ready() {
		ui.Warn("Tailscale isn't configured/connected locally, so the new VM won't join your")
		ui.Warn("tailnet and public SSH can't be auto-closed — it will stay open on port %d.", s.State.SSHPort())
		ok, cerr := ui.Confirm(
			"Continue creating a public-SSH-only VM?",
			"For automatic lockdown, set Tailscale OAuth ('rover config --edit') or TS_AUTHKEY and run 'tailscale up' first.",
			false,
		)
		if cerr != nil {
			return cerr
		}
		if !ok && !assumeYes {
			return fmt.Errorf("aborted; configure Tailscale then re-run 'rover up'")
		}
	}

	ok, err := ui.Confirm(
		"Start/redeploy the Rover VM?",
		fmt.Sprintf("This creates Azure resources and incurs compute charges while the VM runs (%s %s).", family, size),
		true,
	)
	if err != nil {
		return err
	}
	if !ok && !assumeYes {
		return fmt.Errorf("aborted")
	}

	if fresh {
		s.State.PublicSSHClosed = false
	}

	info, err := s.Azure.Up(family, size)
	if err != nil {
		return err
	}
	s.State.Family = family
	s.State.Size = size
	if err := stateutil.SyncConnection(s.State, info); err != nil {
		return err
	}

	if fresh {
		scrubKnownHosts(info.FQDN, s.State.SSHPort())
		scrubKnownHosts(info.PublicIP, s.State.SSHPort())
	}

	fmt.Println()
	ui.Info("VM is up: %s (%s)", info.VMName, info.PowerState)
	printInfo(info)

	if !fresh {
		fmt.Println()
		restoreCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if err := s.Conn.Restore(restoreCtx); err != nil {
			return err
		}
	}

	if willProvision {
		fmt.Println()
		ui.Info("New VM — provisioning automatically (pass --no-provision to skip)...")
		return s.Provision.Run(ctx)
	}

	fmt.Println("\nNext steps:")
	fmt.Println("  rover provision   # configure the host with Ansible (Docker, dune, zsh, ...)")
	fmt.Println("  rover ssh         # connect")
	fmt.Println("  rover down        # deallocate to stop compute billing")
	ui.Warn("Cost: the VM bills while running; disk + public IP persist after 'down'.")
	return nil
}

// Down deallocates the VM or deletes all Rover Azure resources.
func (s *Service) Down(_ context.Context, del, assumeYes bool) error {
	if del {
		ok := assumeYes
		if !ok {
			var err error
			ok, err = ui.Confirm(
				"Delete ALL Rover resources?",
				fmt.Sprintf("This deletes resource group %q including the VM, disks, and public IP. Data is lost.", s.State.ResourceGroup),
				false,
			)
			if err != nil {
				return err
			}
		}
		if !ok {
			return fmt.Errorf("aborted; pass --yes to confirm non-interactively")
		}
	} else {
		ui.Info("Deallocating VM to stop compute billing (disk + IP remain).")
	}
	if del {
		if current, serr := s.Azure.Status(); serr == nil && current.Running() {
			ui.Info("Running pre-delete Tailscale logout inside the VM...")
			if err := s.Azure.RunCommand(tailscaleLogoutScript()); err != nil {
				ui.Warn("Tailscale logout inside VM failed: %v", err)
			}
		} else if serr != nil {
			ui.Warn("Could not check VM state before teardown: %v", serr)
		}
	}

	info, err := s.Azure.Down(del, true)
	if err != nil {
		return err
	}

	if del {
		if s.State.HasTSOAuth() {
			ui.Info("Cleaning up Rover Tailscale devices...")
			if _, err := s.CleanupTailscaleDevices(true, false); err != nil {
				ui.Warn("Tailscale device cleanup failed: %v", err)
			}
		} else {
			ui.Warn("Tailscale OAuth credentials not configured; skipping control-plane device cleanup.")
		}
		s.State.Connection = stateutil.ZeroConnection()
		s.State.AnsibleApplied = false
		s.State.PublicSSHClosed = false
		if err := s.State.Save(); err != nil {
			return fmt.Errorf("save state after delete: %w", err)
		}
		ui.Info("All Rover resources deleted. Cost stops.")
	} else {
		if err := stateutil.SyncConnection(s.State, info); err != nil {
			return err
		}
		ui.Info("VM deallocated. Resume later with 'rover up'.")
		ui.Warn("Cost: OS disk and static public IP still incur small charges. 'rover down --delete' removes everything.")
	}
	return nil
}

// Restart reboots a running VM and restores connectivity afterward.
func (s *Service) Restart(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	current, err := s.Azure.Status()
	if err != nil {
		return err
	}
	if !current.Exists {
		return fmt.Errorf("no VM provisioned; run 'rover up' first")
	}
	if !current.Running() {
		return fmt.Errorf("VM is %q, not running; run 'rover up' to start it", current.PowerState)
	}

	ui.Info("Restarting VM %s (%s)...", current.VMName, current.PowerState)
	info, err := s.Azure.Restart()
	if err != nil {
		return err
	}
	if err := stateutil.SyncConnection(s.State, info); err != nil {
		return err
	}

	fmt.Println()
	ui.Info("VM restarted: %s (%s)", info.VMName, info.PowerState)
	printInfo(info)

	fmt.Println()
	restoreCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := s.Conn.Restore(restoreCtx); err != nil {
		return err
	}
	return nil
}
