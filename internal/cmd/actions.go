package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mitchell-wallace/rover/internal/ansible"
	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/sizes"
	"github.com/mitchell-wallace/rover/internal/tailscale"
	"github.com/mitchell-wallace/rover/internal/ui"
)

var (
	tsFindPeer   = tailscale.FindPeer
	tsGetAuthKey = tailscale.GetAuthKey
	tsConnect    = tailscale.Connect
	tsPingPeer   = tailscale.PingPeer

	restoreConnectivityPollCount = 12
	restoreConnectivityPollWait  = 5 * time.Second

	runRemoteCommandFn func(name string, args ...string) error
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

func reauthenticateTailscale(ctx context.Context, a *appContext) bool {
	var authKey string
	if key := os.Getenv("TS_AUTHKEY"); key != "" {
		authKey = sanitizeAuthKey(key)
	} else if a.state.HasTSOAuth() {
		ui.Info("Generating Tailscale auth key...")
		key, err := tsGetAuthKey(a.state.TSClientID(), a.state.TSClientSecret(), a.state.TSTagSlice())
		if err != nil {
			ui.Warn("Failed to generate Tailscale auth key: %v", err)
		} else {
			authKey = sanitizeAuthKey(key)
		}
	}

	if authKey != "" {
		ui.Info("Re-authenticating Tailscale inside the VM...")
		script := buildReauthScript(authKey, a.state.TSHostname(), a.state.TSTags())
		if err := a.azure.RunCommand(script); err != nil {
			reportRunCommandFailure(err)
		}

		ui.Info("Waiting for Tailscale peer to come online...")
		tshost := a.state.TSHostname()
		for i := 0; i < restoreConnectivityPollCount; i++ {
			select {
			case <-ctx.Done():
				ui.Warn("Cancelled while waiting for Tailscale peer.")
				return false
			default:
			}
			time.Sleep(restoreConnectivityPollWait)
			if peer, err := tsFindPeer(tshost); err == nil && tsPingPeer(peer) {
				ui.Info("Tailscale re-authenticated — VM reachable via 'rover connect'.")
				return true
			}
		}
		ui.Warn("Tailscale peer did not become reachable after %s.", connectivityWaitBudget())
	}

	return false
}

func restoreConnectivity(ctx context.Context, a *appContext) error {
	if !a.state.PublicSSHClosed {
		return nil
	}

	ui.Info("Public SSH is locked down — restoring Tailscale connectivity...")
	if reauthenticateTailscale(ctx, a) {
		return nil
	}

	ui.Warn("Opening public SSH as fallback (Tailscale not available).")
	if err := a.azure.SetPublicSSH(true); err != nil {
		return fmt.Errorf("failed to open public SSH: %w", err)
	}
	a.state.PublicSSHClosed = false
	if err := a.state.Save(); err != nil {
		return fmt.Errorf("save state after opening public SSH: %w", err)
	}
	ui.Info("Public SSH opened on port %d. Run 'rover provision' to re-establish Tailscale.", a.state.SSHPort())
	return nil
}

func sanitizeAuthKey(key string) string {
	var b strings.Builder
	var stripped bool
	for _, r := range key {
		if isSafeAuthKeyChar(r) {
			b.WriteRune(r)
		} else {
			stripped = true
		}
	}
	if stripped {
		ui.Warn("Auth key contained unexpected characters — they were stripped. Use only alphanumeric, '-', or '_'.")
	}
	return b.String()
}

func isSafeAuthKeyChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
}

func sanitizeShellArg(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == ':' || r == '.' {
			return r
		}
		return -1
	}, s)
}

// buildReauthScript returns the Run Command script used to repair Tailscale
// inside the VM. The daemon is restarted first so a wedged tailscaled (alive on
// its socket but not establishing a data plane) reloads its existing node
// credentials instead of minting a duplicate node. Every invocation is bounded
// by timeout(1) so a stuck daemon cannot pin the Run Command extension for
// Azure's ~90 minute script ceiling, and the final `tailscale up` does not
// swallow its exit code so real failures propagate to the caller.
func buildReauthScript(authKey, hostname, tags string) string {
	return fmt.Sprintf(`if ! command -v tailscale >/dev/null 2>&1; then
  echo 'tailscale CLI not installed on VM' >&2
  exit 127
fi
sudo timeout 60s systemctl restart tailscaled 2>&1 || true
sleep 3
sudo timeout 120s tailscale up --authkey='%s' --ssh --hostname='%s' --advertise-tags='%s'`,
		authKey, sanitizeShellArg(hostname), sanitizeShellArg(tags))
}

// reportRunCommandFailure surfaces a RunCommand error to the user. When the
// azure boundary classified it (conflict/transient/guest-failure), the captured
// guest output is printed so the user sees the real cause (invalid auth key,
// unauthorized tag, tailscaled down) instead of a bare exit code.
func reportRunCommandFailure(err error) {
	var rcErr *azure.RunCommandError
	if !errors.As(err, &rcErr) {
		ui.Warn("Tailscale re-auth via Azure Run Command failed: %v", err)
		return
	}
	attempt := "attempt"
	if rcErr.Attempts != 1 {
		attempt = "attempts"
	}
	ui.Warn("Tailscale re-auth via Azure Run Command failed (%s after %d %s).", rcErr.Kind, rcErr.Attempts, attempt)
	for _, line := range strings.Split(strings.TrimSpace(rcErr.Output), "\n") {
		if line != "" {
			fmt.Println("  " + line)
		}
	}
}

// connectivityWaitBudget is the total poll duration used in the
// "did not become reachable after ..." warning, derived from the poll knobs so
// the message stays accurate if the knobs change.
func connectivityWaitBudget() time.Duration {
	return (time.Duration(restoreConnectivityPollCount) * restoreConnectivityPollWait).Round(time.Second)
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
		key, err := tsGetAuthKey(a.state.TSClientID(), a.state.TSClientSecret(), a.state.TSTagSlice())
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
	if peer, err := tsFindPeer(tshost); err == nil && peer.Online {
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
	if !tsPingPeer(peer) {
		ui.Warn("%q is online in Tailscale but not reachable on the data plane.", host)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if reauthenticateTailscale(ctx, a) {
			repairedPeer, err := tsFindPeer(host)
			if err == nil && repairedPeer.Online && tsPingPeer(repairedPeer) {
				target := repairedPeer.Target()
				ui.Info("Connecting over Tailscale to %s@%s...", a.state.AdminUsername, target)
				return tsConnect(a.state.AdminUsername, target, extra...)
			}
		}
		// Re-auth did not restore the data plane. A VM reboot restarts
		// tailscaled and clears a wedged node, so offer that in-process before
		// giving up — this is the documented escape hatch for this exact
		// failure mode.
		if ok, cerr := ui.Confirm(
			"Restart the VM to repair Tailscale?",
			"A reboot restarts the Tailscale daemon inside the VM, which usually restores the data plane. rover reconnects automatically afterward.",
			false,
		); cerr == nil && ok {
			if rerr := doRestart(a); rerr == nil {
				if repairedPeer, ferr := tsFindPeer(host); ferr == nil && repairedPeer.Online && tsPingPeer(repairedPeer) {
					target := repairedPeer.Target()
					ui.Info("Connecting over Tailscale to %s@%s...", a.state.AdminUsername, target)
					return tsConnect(a.state.AdminUsername, target, extra...)
				}
			} else {
				ui.Warn("Restart attempt failed: %v", rerr)
			}
		}
		ui.Info("Run 'rover restart' to repair Tailscale or temporarily open public SSH.")
		return fmt.Errorf("%q is not reachable over Tailscale", host)
	}

	target := peer.Target()
	ui.Info("Connecting over Tailscale to %s@%s...", a.state.AdminUsername, target)
	return tsConnect(a.state.AdminUsername, target, extra...)
}

func doCommand(a *appContext, args []string) error {
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

	cmdStr := strings.Join(args, " ")
	runFn := runRemoteCommand
	if runRemoteCommandFn != nil {
		runFn = runRemoteCommandFn
	}

	if peer, perr := tsFindPeer(a.state.TSHostname()); perr == nil && peer != nil {
		if tsPingPeer(peer) {
			target := peer.Target()
			ui.Info("Running over Tailscale (%s): %s", target, cmdStr)
			return runFn("tailscale",
				"ssh", a.state.AdminUsername+"@"+target, "--", cmdStr)
		}
		if peer.Online {
			ui.Warn("Tailscale peer is online but unreachable.")
			if a.state.PublicSSHClosed {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				if err := restoreConnectivity(ctx, a); err != nil {
					return err
				}
				if repairedPeer, err := tsFindPeer(a.state.TSHostname()); err == nil && tsPingPeer(repairedPeer) {
					target := repairedPeer.Target()
					ui.Info("Running over Tailscale (%s): %s", target, cmdStr)
					return runFn("tailscale",
						"ssh", a.state.AdminUsername+"@"+target, "--", cmdStr)
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
		"-p", strconv.Itoa(a.state.SSHPort()),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
	}
	if pk := a.state.PrivateKeyPath(); pk != "" {
		sshArgs = append(sshArgs, "-i", pk)
	}
	sshArgs = append(sshArgs, a.state.AdminUsername+"@"+host, "--", cmdStr)
	return runFn("ssh", sshArgs...)
}

func runRemoteCommand(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
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
