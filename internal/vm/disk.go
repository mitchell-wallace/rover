package vm

import (
	"fmt"

	"github.com/mitchell-wallace/rover/internal/stateutil"
	"github.com/mitchell-wallace/rover/internal/ui"
)

// Disk records or applies the requested OS disk size.
func (s *Service) Disk(gb int, assumeYes bool) error {
	if gb < 30 {
		return fmt.Errorf("disk size must be at least 30 GiB")
	}
	current, err := s.Azure.Status()
	if err != nil {
		return err
	}
	if !current.Exists {
		s.State.DiskSizeGB = gb
		if err := s.State.Save(); err != nil {
			return err
		}
		ui.Info("No VM yet — recorded disk size %d GiB for the next 'rover up'.", gb)
		return nil
	}
	if current.DiskSizeGB > 0 && gb < current.DiskSizeGB {
		return fmt.Errorf("OS disks cannot shrink (current %d GiB, requested %d GiB)", current.DiskSizeGB, gb)
	}
	if current.DiskSizeGB == gb {
		ui.Info("Disk already %d GiB; nothing to do.", gb)
		s.State.DiskSizeGB = gb
		if err := s.State.Save(); err != nil {
			return err
		}
		return nil
	}

	ok, err := ui.Confirm(
		fmt.Sprintf("Resize OS disk %d → %d GiB?", current.DiskSizeGB, gb),
		"The VM will be deallocated during the resize (brief downtime) and restarted if it was running.",
		true,
	)
	if err != nil {
		return err
	}
	if !ok && !assumeYes {
		return fmt.Errorf("aborted")
	}

	info, err := s.Azure.ResizeDisk(gb)
	if err != nil {
		return err
	}
	s.State.DiskSizeGB = gb
	if err := stateutil.SyncConnection(s.State, info); err != nil {
		return err
	}
	ui.Info("OS disk is now %d GiB. The root filesystem auto-grows on boot.", gb)
	return nil
}

// Status prints the current VM status and syncs persisted connection info.
func (s *Service) Status() error {
	info, err := s.Azure.Status()
	if err != nil {
		return err
	}
	if !info.Exists {
		fmt.Printf("Rover VM: not provisioned (resource group %s, region %s)\n", s.State.ResourceGroup, s.State.Location)
		fmt.Println("Run 'rover up [small|medium|large]' to create one.")
		return nil
	}
	if err := stateutil.SyncConnection(s.State, info); err != nil {
		return err
	}
	fmt.Printf("Rover VM: %s (%s)\n", info.VMName, info.PowerState)
	printInfo(info)
	if s.State.AnsibleApplied {
		fmt.Println("  provisioned: yes (Ansible applied)")
	} else {
		fmt.Println("  provisioned: no — run 'rover provision'")
	}
	return nil
}

// SSH opens an SSH session through Azure's SSH wrapper. Interactive sessions
// attach to tmux by default; one-off commands always bypass tmux.
func (s *Service) SSH(opts SSHOptions, extra ...string) error {
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
	useTmux := s.State.SSHTmux() && !opts.NoTmux && len(extra) == 0
	return s.Azure.SSH(useTmux, extra...)
}
