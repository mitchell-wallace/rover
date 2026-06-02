package cmd

import (
	"fmt"

	"github.com/mitchell-wallace/rover/internal/ansible"
	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/sizes"
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
	profile, _ := sizes.Get(size)
	ui.Info("Selected size: %s", profile.Describe())

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

// doDown deallocates the VM, or deletes the resource group when delete is true.
func doDown(a *appContext, delete, assumeYes bool) error {
	if delete {
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

	info, err := a.azure.Down(delete, true)
	if err != nil {
		return err
	}

	if delete {
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

	ui.Info("Provisioning %s (%s) with Ansible...", info.VMName, info.Host())
	err = ansible.Provision(ansible.Params{
		Host:       info.Host(),
		User:       a.state.AdminUsername,
		PrivateKey: a.state.PrivateKeyPath(),
		AssetDir:   a.assetDir,
	})
	if err != nil {
		return err
	}
	a.state.AnsibleApplied = true
	a.syncConnection(info)
	ui.Info("Provisioning complete. Connect with 'rover ssh' and run 'dune'.")
	return nil
}

func printInfo(info azure.Info) {
	fmt.Printf("  size:        %s\n", info.VMSize)
	fmt.Printf("  region:      %s\n", info.Location)
	fmt.Printf("  public IP:   %s\n", info.PublicIP)
	fmt.Printf("  fqdn:        %s\n", info.FQDN)
	fmt.Printf("  private IP:  %s\n", info.PrivateIP)
	fmt.Printf("  ssh target:  %s\n", info.SSHTarget)
}
