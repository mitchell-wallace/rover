package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/mitchell-wallace/rover/internal/ansible"
	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/sizes"
	"github.com/mitchell-wallace/rover/internal/tailscale"
	"github.com/mitchell-wallace/rover/internal/ui"
)

// syncConnection persists the latest connection snapshot into state.
func (a *appContext) syncConnection(info azure.Info) {
	a.state.Connection = configConnFrom(info)
	if info.VMSize != "" {
		a.state.Connection.VMSize = info.VMSize
	}
	_ = a.state.Save()
}

// doUp provisions/redeploys the VM at the given size.
func doUp(a *appContext, size string, assumeYes bool) error {
	if err := sizes.Validate(size); err != nil {
		return err
	}
	if err := config.ValidateAdminUsername(a.state.AdminUsername); err != nil {
		return fmt.Errorf("%w (fix with 'rover config --edit')", err)
	}
	profile, _ := sizes.Get(size)
	ui.Info("Selected size: %s", profile.Describe())
	ui.Info("Destination: %s / %s in %s as user %q (disk %d GiB)",
		a.state.ResourceGroup, a.state.VMName, a.state.Location, a.state.AdminUsername, a.state.DiskGB())

	current, err := a.azure.Status()
	if err == nil && current.Running() && current.VMSize != "" && current.VMSize != profile.SKU {
		ui.Warn("A VM is already running as %s. Rover manages one VM at a time;", current.VMSize)
		ui.Warn("continuing will redeploy/resize it in place to %s.", profile.SKU)
	}

	ok, err := ui.Confirm(
		"Start/redeploy the Rover VM?",
		fmt.Sprintf("This creates Azure resources and incurs compute charges while the VM runs (size %s).", size),
		true,
	)
	if err != nil {
		return err
	}
	if !ok && !assumeYes {
		return fmt.Errorf("aborted")
	}

	info, err := a.azure.Up(size)
	if err != nil {
		return err
	}
	a.state.Size = size
	a.syncConnection(info)

	fmt.Println()
	ui.Info("VM is up: %s (%s)", info.VMName, info.PowerState)
	printInfo(info)
	fmt.Println("\nNext steps:")
	fmt.Println("  rover provision   # configure the host with Ansible (Docker, dune, zsh, ...)")
	fmt.Println("  rover ssh         # connect")
	fmt.Println("  rover down        # deallocate to stop compute billing")
	ui.Warn("Cost: the VM bills while running; disk + public IP persist after 'down'.")
	return nil
}

// doDown deallocates the VM, or deletes the resource group when del is true.
func doDown(a *appContext, del, assumeYes bool) error {
	if del {
		ok := assumeYes
		if !ok {
			var err error
			ok, err = ui.Confirm(
				"Delete ALL Rover resources?",
				fmt.Sprintf("This deletes resource group %q including the VM, disks, and public IP. Data is lost.", a.state.ResourceGroup),
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

	info, err := a.azure.Down(del, true)
	if err != nil {
		return err
	}

	if del {
		a.state.Connection = stateZeroConn()
		a.state.AnsibleApplied = false
		_ = a.state.Save()
		ui.Info("All Rover resources deleted. Cost stops.")
	} else {
		a.syncConnection(info)
		ui.Info("VM deallocated. Resume later with 'rover up'.")
		ui.Warn("Cost: OS disk and static public IP still incur small charges. 'rover down --delete' removes everything.")
	}
	return nil
}

// doDisk grows the OS disk to gb GiB, preserving the disk and its data. The new
// size is persisted so subsequent `up` deploys keep it.
func doDisk(a *appContext, gb int, assumeYes bool) error {
	if gb < 30 {
		return fmt.Errorf("disk size must be at least 30 GiB")
	}
	current, err := a.azure.Status()
	if err != nil {
		return err
	}
	if !current.Exists {
		// No VM yet: just record the desired size for the next `up`.
		a.state.DiskSizeGB = gb
		if err := a.state.Save(); err != nil {
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
		a.state.DiskSizeGB = gb
		_ = a.state.Save()
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

	info, err := a.azure.ResizeDisk(gb)
	if err != nil {
		return err
	}
	a.state.DiskSizeGB = gb
	a.syncConnection(info)
	ui.Info("OS disk is now %d GiB. The root filesystem auto-grows on boot.", gb)
	return nil
}

// doStatus prints current VM status and refreshes cached connection info.
func doStatus(a *appContext) error {
	info, err := a.azure.Status()
	if err != nil {
		return err
	}
	if !info.Exists {
		fmt.Printf("Rover VM: not provisioned (resource group %s, region %s)\n", a.state.ResourceGroup, a.state.Location)
		fmt.Println("Run 'rover up [small|medium|large]' to create one.")
		return nil
	}
	a.syncConnection(info)
	fmt.Printf("Rover VM: %s (%s)\n", info.VMName, info.PowerState)
	printInfo(info)
	if a.state.AnsibleApplied {
		fmt.Println("  provisioned: yes (Ansible applied)")
	} else {
		fmt.Println("  provisioned: no — run 'rover provision'")
	}
	return nil
}

// doSSH opens an interactive SSH session.
func doSSH(a *appContext, extra ...string) error {
	info, err := a.azure.Status()
	if err != nil {
		return err
	}
	if !info.Exists {
		return fmt.Errorf("no VM provisioned; run 'rover up' first")
	}
	if !info.Running() {
		return fmt.Errorf("VM is %q, not running; run 'rover up' to start it", info.PowerState)
	}
	return a.azure.SSH(extra...)
}

// doProvision runs the Ansible playbook against the live VM.
func doProvision(a *appContext) error {
	info, err := a.azure.Info()
	if err != nil {
		return err
	}
	if !info.Exists {
		return fmt.Errorf("no VM provisioned; run 'rover up' first")
	}
	if !info.Running() {
		return fmt.Errorf("VM is %q, not running; run 'rover up' to start it", info.PowerState)
	}

	if os.Getenv("TS_AUTHKEY") != "" {
		ui.Info("TS_AUTHKEY detected — VM will join your tailnet as %q.", a.state.TSHostname())
	} else {
		ui.Info("TS_AUTHKEY not set — skipping Tailscale (set it to enable 'rover connect').")
	}

	ui.Info("Provisioning %s (%s) with Ansible...", info.VMName, info.Host())
	err = ansible.Provision(ansible.Params{
		Host:       info.Host(),
		User:       a.state.AdminUsername,
		PrivateKey: a.state.PrivateKeyPath(),
		AssetDir:   a.assetDir,
		ExtraVars: map[string]string{
			"tailscale_hostname": a.state.TSHostname(),
			"tailscale_tags":     a.state.TSTags(),
		},
	})
	if err != nil {
		return err
	}
	a.state.AnsibleApplied = true
	a.syncConnection(info)
	ui.Info("Provisioning complete. Connect with 'rover ssh' and run 'dune'.")
	return nil
}

// doConnect connects to the VM over Tailscale if it is online in the tailnet.
func doConnect(a *appContext, extra ...string) error {
	host := a.state.TSHostname()
	peer, err := tailscale.FindPeer(host)
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

	target := peer.Target()
	ui.Info("Connecting over Tailscale to %s@%s...", a.state.AdminUsername, target)
	return tailscale.Connect(a.state.AdminUsername, target, extra...)
}

func printInfo(info azure.Info) {
	fmt.Printf("  size:        %s\n", info.VMSize)
	if info.DiskSizeGB > 0 {
		fmt.Printf("  disk:        %d GiB\n", info.DiskSizeGB)
	}
	fmt.Printf("  region:      %s\n", info.Location)
	fmt.Printf("  public IP:   %s\n", info.PublicIP)
	fmt.Printf("  fqdn:        %s\n", info.FQDN)
	fmt.Printf("  private IP:  %s\n", info.PrivateIP)
	fmt.Printf("  ssh target:  %s\n", info.SSHTarget)
}
