package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/mitchell-wallace/rover/internal/ansible"
	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/shellsafe"
	"github.com/mitchell-wallace/rover/internal/sizes"
	"github.com/mitchell-wallace/rover/internal/tailscale"
	"github.com/mitchell-wallace/rover/internal/ui"
)

func (a *appContext) syncConnection(info azure.Info) error {
	a.state.Connection = configConnFrom(info)
	if info.VMSize != "" {
		a.state.Connection.VMSize = info.VMSize
	}
	return a.state.Save()
}

func scrubKnownHosts(host string, port int) {
	if host == "" {
		return
	}
	for _, target := range []string{host, fmt.Sprintf("[%s]:%d", host, port)} {
		_ = exec.Command("ssh-keygen", "-R", target).Run()
	}
}

func tailscaleReady(a *appContext) bool {
	if a == nil || a.conn == nil {
		return false
	}
	return a.conn.Ready()
}

func reauthenticateTailscale(ctx context.Context, a *appContext) bool {
	if a == nil || a.conn == nil {
		return false
	}
	return a.conn.Reauthenticate(ctx)
}

func restoreConnectivity(ctx context.Context, a *appContext) error {
	if a == nil || a.conn == nil {
		return fmt.Errorf("connectivity service not configured")
	}
	return a.conn.Restore(ctx)
}

func sanitizeAuthKey(key string) string {
	clean, stripped := shellsafe.AuthKey(key)
	if stripped {
		ui.Warn("Auth key contained unexpected characters — they were stripped. Use only alphanumeric, '-', or '_'.")
	}
	return clean
}

func doUp(a *appContext, family, size string, assumeYes, noProvision bool) error {
	family = sizes.NormalizeFamily(family)
	if err := sizes.Validate(family, size); err != nil {
		return err
	}
	if err := config.ValidateAdminUsername(a.state.AdminUsername); err != nil {
		return fmt.Errorf("%w (fix with 'rover config --edit')", err)
	}
	profile, _ := sizes.Get(family, size)
	ui.Info("Selected family: %s", sizes.DescribeFamily(family))
	ui.Info("Selected size: %s", profile.Describe())
	ui.Info("Destination: %s / %s in %s as user %q (disk %d GiB)",
		a.state.ResourceGroup, a.state.VMName, a.state.Location, a.state.AdminUsername, a.state.DiskGB())

	current, err := a.azure.Status()
	fresh := err == nil && !current.Exists
	if err == nil && current.Running() && current.VMSize != "" && current.VMSize != profile.SKU {
		ui.Warn("A VM is already running as %s. Rover manages one VM at a time;", current.VMSize)
		ui.Warn("continuing will redeploy/resize it in place to %s.", profile.SKU)
	}

	willProvision := fresh && !noProvision
	if willProvision && !a.conn.Ready() {
		ui.Warn("Tailscale isn't configured/connected locally, so the new VM won't join your")
		ui.Warn("tailnet and public SSH can't be auto-closed — it will stay open on port %d.", a.state.SSHPort())
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
		a.state.PublicSSHClosed = false
	}

	info, err := a.azure.Up(family, size)
	if err != nil {
		return err
	}
	a.state.Family = family
	a.state.Size = size
	if err := a.syncConnection(info); err != nil {
		return err
	}

	if fresh {
		scrubKnownHosts(info.FQDN, a.state.SSHPort())
		scrubKnownHosts(info.PublicIP, a.state.SSHPort())
	}

	fmt.Println()
	ui.Info("VM is up: %s (%s)", info.VMName, info.PowerState)
	printInfo(info)

	if !fresh {
		fmt.Println()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := restoreConnectivity(ctx, a); err != nil {
			return err
		}
	}

	if willProvision {
		fmt.Println()
		ui.Info("New VM — provisioning automatically (pass --no-provision to skip)...")
		return doProvision(a)
	}

	fmt.Println("\nNext steps:")
	fmt.Println("  rover provision   # configure the host with Ansible (Docker, dune, zsh, ...)")
	fmt.Println("  rover ssh         # connect")
	fmt.Println("  rover down        # deallocate to stop compute billing")
	ui.Warn("Cost: the VM bills while running; disk + public IP persist after 'down'.")
	return nil
}

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
	if del {
		if current, serr := a.azure.Status(); serr == nil && current.Running() {
			ui.Info("Running pre-delete Tailscale logout inside the VM...")
			if err := a.azure.RunCommand(tailscaleLogoutScript()); err != nil {
				ui.Warn("Tailscale logout inside VM failed: %v", err)
			}
		} else if serr != nil {
			ui.Warn("Could not check VM state before teardown: %v", serr)
		}
	}

	info, err := a.azure.Down(del, true)
	if err != nil {
		return err
	}

	if del {
		if a.state.HasTSOAuth() {
			ui.Info("Cleaning up Rover Tailscale devices...")
			if _, err := doTailscaleCleanup(a, true, false); err != nil {
				ui.Warn("Tailscale device cleanup failed: %v", err)
			}
		} else {
			ui.Warn("Tailscale OAuth credentials not configured; skipping control-plane device cleanup.")
		}
		a.state.Connection = stateZeroConn()
		a.state.AnsibleApplied = false
		a.state.PublicSSHClosed = false
		if err := a.state.Save(); err != nil {
			return fmt.Errorf("save state after delete: %w", err)
		}
		ui.Info("All Rover resources deleted. Cost stops.")
	} else {
		if err := a.syncConnection(info); err != nil {
			return err
		}
		ui.Info("VM deallocated. Resume later with 'rover up'.")
		ui.Warn("Cost: OS disk and static public IP still incur small charges. 'rover down --delete' removes everything.")
	}
	return nil
}

func doRestart(a *appContext) error {
	current, err := a.azure.Status()
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
	info, err := a.azure.Restart()
	if err != nil {
		return err
	}
	if err := a.syncConnection(info); err != nil {
		return err
	}

	fmt.Println()
	ui.Info("VM restarted: %s (%s)", info.VMName, info.PowerState)
	printInfo(info)

	fmt.Println()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := restoreConnectivity(ctx, a); err != nil {
		return err
	}
	return nil
}

func tailscaleLogoutScript() string {
	return `if command -v tailscale >/dev/null 2>&1; then
  tailscale logout || true
  systemctl stop tailscaled || true
  systemctl disable tailscaled || true
fi`
}

func doTailscaleCleanup(a *appContext, deleteOnline, dryRun bool) (tailscale.CleanupResult, error) {
	if !a.state.HasTSOAuth() {
		return tailscale.CleanupResult{}, fmt.Errorf("tailscale OAuth credentials not configured; set them with 'rover config --edit'")
	}
	res, err := a.conn.TS.CleanupDevices(
		a.state.TSClientID(),
		a.state.TSClientSecret(),
		a.state.TSTagSlice(),
		a.state.TSHostname(),
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

func doDisk(a *appContext, gb int, assumeYes bool) error {
	if gb < 30 {
		return fmt.Errorf("disk size must be at least 30 GiB")
	}
	current, err := a.azure.Status()
	if err != nil {
		return err
	}
	if !current.Exists {
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
		if err := a.state.Save(); err != nil {
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

	info, err := a.azure.ResizeDisk(gb)
	if err != nil {
		return err
	}
	a.state.DiskSizeGB = gb
	if err := a.syncConnection(info); err != nil {
		return err
	}
	ui.Info("OS disk is now %d GiB. The root filesystem auto-grows on boot.", gb)
	return nil
}

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
	if err := a.syncConnection(info); err != nil {
		return err
	}
	fmt.Printf("Rover VM: %s (%s)\n", info.VMName, info.PowerState)
	printInfo(info)
	if a.state.AnsibleApplied {
		fmt.Println("  provisioned: yes (Ansible applied)")
	} else {
		fmt.Println("  provisioned: no — run 'rover provision'")
	}
	return nil
}

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

func waitForSSH(ctx context.Context, host string, port int) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	deadline := time.Now().Add(5 * time.Minute)
	announced := false
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		default:
		}
		dialer := net.Dialer{Timeout: 5 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			if announced {
				ui.Info("SSH is up.")
			}
			return
		}
		if !announced {
			ui.Info("Waiting for SSH on port %d (the VM may still be booting)...", port)
			announced = true
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

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

	var authKey string
	var usingOAuth bool
	if key := os.Getenv("TS_AUTHKEY"); key != "" {
		authKey = sanitizeAuthKey(key)
		ui.Info("TS_AUTHKEY detected in environment — VM will join your tailnet as %q.", a.state.TSHostname())
	} else if a.state.HasTSOAuth() {
		ui.Info("Generating Tailscale auth key via OAuth client for hostname %q...", a.state.TSHostname())
		key, err := a.conn.TS.GetAuthKey(a.state.TSClientID(), a.state.TSClientSecret(), a.state.TSTagSlice())
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
	tshost := a.state.TSHostname()
	if peer, err := a.conn.TS.FindPeer(tshost); err == nil && peer.Online {
		target := peer.Target()
		ui.Info("Tailscale connection active. Provisioning over Tailscale (%s)...", target)
		host = target
	} else {
		ui.Info("Provisioning %s (%s) over public IP with Ansible...", info.VMName, host)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	waitForSSH(ctx, host, a.state.SSHPort())

	err = ansible.Provision(ansible.Params{
		Host:       host,
		User:       a.state.AdminUsername,
		PrivateKey: a.state.PrivateKeyPath(),
		AssetDir:   a.assetDir,
		ExtraVars: map[string]string{
			"ansible_port":       strconv.Itoa(a.state.SSHPort()),
			"tailscale_hostname": a.state.TSHostname(),
			"tailscale_tags":     a.state.TSTags(),
		},
	})
	if err != nil {
		return err
	}
	a.state.AnsibleApplied = true
	if err := a.syncConnection(info); err != nil {
		return err
	}
	ui.Info("Provisioning complete.")

	if authKey != "" || usingOAuth {
		ui.Info("Verifying Tailscale connection to VM...")
		if peer, err := a.conn.TS.FindPeer(tshost); err == nil && peer.Online {
			ui.Info("Tailscale connection verified.")
			if a.state.PublicSSHClosed {
				ui.Info("Public SSH already closed — VM reachable only over Tailscale.")
			} else {
				ui.Info("Locking down: closing public SSH (VM stays reachable over Tailscale)...")
				if err := a.azure.SetPublicSSH(false); err != nil {
					ui.Warn("Failed to close public SSH: %v — public SSH left OPEN on port %d.", err, a.state.SSHPort())
				} else {
					a.state.PublicSSHClosed = true
					if err := a.state.Save(); err != nil {
						ui.Warn("Failed to save state after closing public SSH: %v", err)
					} else {
						ui.Info("Public SSH closed. The VM is now reachable only over Tailscale ('rover connect').")
					}
				}
			}
		} else {
			ui.Warn("Tailscale verification failed: peer offline or not found — keeping public SSH OPEN on port %d.", a.state.SSHPort())
		}
	}

	ui.Info("Connect with 'rover ssh' (or 'rover connect' if Tailscale is active) and run 'dune'.")
	return nil
}

func doConnect(a *appContext, extra ...string) error {
	if a == nil || a.conn == nil {
		return fmt.Errorf("connectivity service not configured")
	}
	if a.conn.Restart == nil {
		a.conn.Restart = func() error { return doRestart(a) }
	}
	return a.conn.Connect(context.Background(), extra...)
}

func doCommand(a *appContext, args []string) error {
	if a == nil || a.conn == nil {
		return fmt.Errorf("connectivity service not configured")
	}
	return a.conn.RunCommand(context.Background(), args)
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
