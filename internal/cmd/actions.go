package cmd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/mitchell-wallace/rover/internal/ansible"
	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/sizes"
	"github.com/mitchell-wallace/rover/internal/tailscale"
	"github.com/mitchell-wallace/rover/internal/ui"
)

// Overridable for testing. Production code leaves these at their defaults.
var (
	tsFindPeer   = tailscale.FindPeer
	tsGetAuthKey = tailscale.GetAuthKey

	// restoreConnectivityPollCount controls how many times restoreConnectivity
	// polls for the Tailscale peer. Override in tests to avoid sleeps.
	restoreConnectivityPollCount = 12
	restoreConnectivityPollWait  = 5 * time.Second
)

// syncConnection persists the latest connection snapshot into state.
func (a *appContext) syncConnection(info azure.Info) {
	a.state.Connection = configConnFrom(info)
	if info.VMSize != "" {
		a.state.Connection.VMSize = info.VMSize
	}
	_ = a.state.Save()
}

// scrubKnownHosts removes any stale host keys for host (both the plain and the
// custom-port "[host]:port" forms) from the user's known_hosts. Rover VMs reuse
// a deterministic FQDN across recreate with a fresh host key, so a redeploy is a
// genuinely new host and dropping the old key is correct — it keeps interactive
// `rover ssh` (which still verifies host keys) from failing on the change.
func scrubKnownHosts(host string, port int) {
	if host == "" {
		return
	}
	for _, target := range []string{host, fmt.Sprintf("[%s]:%d", host, port)} {
		_ = exec.Command("ssh-keygen", "-R", target).Run()
	}
}

// tailscaleReady reports whether Rover can join the VM to the tailnet and verify
// it: credentials must be configured (TS_AUTHKEY or OAuth) AND the local
// tailscale backend must be running so the peer can be confirmed and public SSH
// closed. A PeerNotFoundError (VM not up yet) still means the local backend is
// usable; only ErrNotInstalled/ErrNotRunning mean it isn't.
func tailscaleReady(st *config.State) bool {
	if os.Getenv("TS_AUTHKEY") == "" && !st.HasTSOAuth() {
		return false
	}
	_, err := tsFindPeer(st.TSHostname())
	if err == nil {
		return true
	}
	var notFound *tailscale.PeerNotFoundError
	return errors.As(err, &notFound)
}

// restoreConnectivity ensures the user can reach the VM after a start/resize.
// If public SSH was locked down (Tailscale-only) but Tailscale is offline, it
// tries to re-authenticate Tailscale inside the VM via Azure Run Command. If
// that fails or credentials are unavailable, it opens public SSH as a fallback.
func restoreConnectivity(a *appContext) error {
	if !a.state.PublicSSHClosed {
		return nil
	}

	ui.Info("Public SSH is locked down — restoring Tailscale connectivity...")

	var authKey string
	if key := os.Getenv("TS_AUTHKEY"); key != "" {
		authKey = key
	} else if a.state.HasTSOAuth() {
		ui.Info("Generating Tailscale auth key...")
		key, err := tsGetAuthKey(a.state.TSClientID(), a.state.TSClientSecret(), a.state.TSTagSlice())
		if err != nil {
			ui.Warn("Failed to generate Tailscale auth key: %v", err)
		} else {
			authKey = key
		}
	}

	if authKey != "" {
		ui.Info("Re-authenticating Tailscale inside the VM...")
		script := fmt.Sprintf(
			`sudo tailscale up --authkey='%s' --ssh --hostname='%s' --advertise-tags='%s' 2>&1 || true`,
			authKey, a.state.TSHostname(), a.state.TSTags(),
		)
		if err := a.azure.RunCommand(script); err != nil {
			ui.Warn("Tailscale re-auth via Azure Run Command failed: %v", err)
		}

		ui.Info("Waiting for Tailscale peer to come online...")
		tshost := a.state.TSHostname()
		for i := 0; i < restoreConnectivityPollCount; i++ {
			time.Sleep(restoreConnectivityPollWait)
			if peer, err := tsFindPeer(tshost); err == nil && peer.Online {
				ui.Info("Tailscale re-authenticated — VM reachable via 'rover connect'.")
				return nil
			}
		}
		ui.Warn("Tailscale peer did not come online after 60s.")
	}

	ui.Warn("Opening public SSH as fallback (Tailscale not available).")
	if err := a.azure.SetPublicSSH(true); err != nil {
		return fmt.Errorf("failed to open public SSH: %w", err)
	}
	a.state.PublicSSHClosed = false
	_ = a.state.Save()
	ui.Info("Public SSH opened on port %d. Run 'rover provision' to re-establish Tailscale.", a.state.SSHPort())
	return nil
}

// doUp provisions/redeploys the VM at the given family/size. On a fresh create it
// auto-provisions (unless noProvision) and, when Tailscale is ready, locks the VM
// down to Tailscale-only SSH.
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

	// A fresh create auto-provisions and then locks down to Tailscale-only SSH.
	// If Tailscale isn't ready that lockdown can't engage, so warn and confirm
	// before creating a VM that will stay reachable on the public SSH port.
	willProvision := fresh && !noProvision
	if willProvision && !tailscaleReady(a.state) {
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

	// A fresh VM needs public SSH open for the bootstrap provision; clear any
	// stale lockdown flag left in state (e.g. from a prior, now-deleted VM).
	if fresh {
		a.state.PublicSSHClosed = false
	}

	info, err := a.azure.Up(family, size)
	if err != nil {
		return err
	}
	a.state.Family = family
	a.state.Size = size
	a.syncConnection(info)

	// A freshly created VM presents a new host key on the same FQDN; drop stale
	// known_hosts entries so 'rover ssh' connects without a verification failure.
	// Starting or resizing an existing VM does not change the host key.
	if fresh {
		scrubKnownHosts(info.FQDN, a.state.SSHPort())
		scrubKnownHosts(info.PublicIP, a.state.SSHPort())
	}

	fmt.Println()
	ui.Info("VM is up: %s (%s)", info.VMName, info.PowerState)
	printInfo(info)

	if !fresh {
		fmt.Println()
		if err := restoreConnectivity(a); err != nil {
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
		_ = a.state.Save()
		ui.Info("All Rover resources deleted. Cost stops.")
	} else {
		a.syncConnection(info)
		ui.Info("VM deallocated. Resume later with 'rover up'.")
		ui.Warn("Cost: OS disk and static public IP still incur small charges. 'rover down --delete' removes everything.")
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
	res, err := tailscale.CleanupDevices(
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

// waitForSSH polls host:port until it accepts a TCP connection, up to a few
// minutes, so provisioning a just-created VM doesn't fail before cloud-init has
// moved sshd onto Rover's custom port. It's best-effort: on timeout it returns
// and lets Ansible (which also waits) surface any real failure.
func waitForSSH(host string, port int) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	deadline := time.Now().Add(5 * time.Minute)
	announced := false
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
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
		time.Sleep(5 * time.Second)
	}
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

	// 1. Resolve Tailscale Key (check env, then OAuth client)
	var authKey string
	var usingOAuth bool
	if key := os.Getenv("TS_AUTHKEY"); key != "" {
		authKey = key
		ui.Info("TS_AUTHKEY detected in environment — VM will join your tailnet as %q.", a.state.TSHostname())
	} else if a.state.HasTSOAuth() {
		ui.Info("Generating Tailscale auth key via OAuth client for hostname %q...", a.state.TSHostname())
		key, err := tsGetAuthKey(a.state.TSClientID(), a.state.TSClientSecret(), a.state.TSTagSlice())
		if err != nil {
			return fmt.Errorf("generate tailscale auth key: %w", err)
		}
		authKey = key
		usingOAuth = true
	} else {
		ui.Info("Tailscale credentials not set (TS_AUTHKEY or OAuth client ID/secret) — skipping Tailscale.")
	}

	if authKey != "" {
		_ = os.Setenv("TS_AUTHKEY", authKey)
		defer func() { _ = os.Unsetenv("TS_AUTHKEY") }()
	}

	host := info.Host()
	tshost := a.state.TSHostname()
	if peer, err := tsFindPeer(tshost); err == nil && peer.Online {
		target := peer.Target()
		ui.Info("Tailscale connection active. Provisioning over Tailscale (%s)...", target)
		host = target
	} else {
		ui.Info("Provisioning %s (%s) over public IP with Ansible...", info.VMName, host)
	}

	// A freshly created VM reports "running" before cloud-init has moved sshd onto
	// Rover's custom port, so the port refuses connections for a short window.
	// Wait for it to open before handing off to Ansible (which also retries via
	// wait_for_connection, but this gives clearer feedback while booting).
	waitForSSH(host, a.state.SSHPort())

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
	a.syncConnection(info)
	ui.Info("Provisioning complete.")

	// 3. Verify Tailscale is active and, if so, automatically lock the VM down to
	// Tailscale-only SSH (close the public SSH port). This is intentionally not a
	// prompt — the decision to require Tailscale was made at 'rover up' time.
	if authKey != "" || usingOAuth {
		ui.Info("Verifying Tailscale connection to VM...")
		if peer, err := tsFindPeer(tshost); err == nil && peer.Online {
			ui.Info("Tailscale connection verified.")
			if a.state.PublicSSHClosed {
				ui.Info("Public SSH already closed — VM reachable only over Tailscale.")
			} else {
				ui.Info("Locking down: closing public SSH (VM stays reachable over Tailscale)...")
				if err := a.azure.SetPublicSSH(false); err != nil {
					ui.Warn("Failed to close public SSH: %v — public SSH left OPEN on port %d.", err, a.state.SSHPort())
				} else {
					a.state.PublicSSHClosed = true
					_ = a.state.Save()
					ui.Info("Public SSH closed. The VM is now reachable only over Tailscale ('rover connect').")
				}
			}
		} else {
			ui.Warn("Tailscale verification failed: peer offline or not found — keeping public SSH OPEN on port %d.", a.state.SSHPort())
		}
	}

	ui.Info("Connect with 'rover ssh' (or 'rover connect' if Tailscale is active) and run 'dune'.")
	return nil
}

// doConnect connects to the VM over Tailscale if it is online in the tailnet.
func doConnect(a *appContext, extra ...string) error {
	host := a.state.TSHostname()
	peer, err := tsFindPeer(host)
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
