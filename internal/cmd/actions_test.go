package cmd

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/tailscale"
)

// ---------------------------------------------------------------------------
// Real-backend verification notes (update when re-verifying against live Azure)
// ---------------------------------------------------------------------------
//
// Last verified: 2026-06-10 against Azure subscription in australiaeast.
//
// Azure responses captured from `az` CLI:
//
//   vm_power_state (deallocated):
//     az vm get-instance-view -g rover-rg -n rover-vm \
//       --query "instanceView.statuses[?starts_with(code,'PowerState/')].displayStatus|[0]" -o tsv
//     → "VM deallocated"
//
//   vm_power_state (running):
//     → "VM running"
//
//   vm show (SKU):
//     az vm show -g rover-rg -n rover-vm --query hardwareProfile.vmSize -o tsv
//     → "Standard_B2als_v2"
//
//   NSG rule:
//     az network nsg rule show -g rover-rg --nsg-name rover-vm-nsg -n allow-ssh \
//       --query access -o tsv
//     → "Deny" (when Tailscale lockdown active)
//     → "Allow" (when public SSH open)
//
// Tailscale responses captured from `tailscale` CLI:
//
//   tailscale status --json (peer shape, online):
//     {
//       "HostName": "rover-vm",
//       "DNSName": "rover-vm.tail94a70e.ts.net.",
//       "Online": true,
//       "TailscaleIPs": ["100.88.25.46"]
//     }
//
//   tailscale status --json (peer shape, offline after deallocation):
//     { "Online": false }
//
//   Inside VM after deallocation+restart:
//     tailscale status → "Logged out.\nLog in at: https://login.tailscale.com/a/...\n"
//
// Bicep deployment error when redeploying existing VM:
//   {"error":{"code":"PropertyChangeNotAllowed","message":"Changing property
//    'osProfile.customData' is not allowed."}}
//
// ---------------------------------------------------------------------------

// mockAzureClient implements azureProvider for testing.
type mockAzureClient struct {
	upFn           func(family, size string) (azure.Info, error)
	downFn         func(del, yes bool) (azure.Info, error)
	statusFn       func() (azure.Info, error)
	resizeDiskFn   func(gb int) (azure.Info, error)
	infoFn         func() (azure.Info, error)
	sshFn          func(extra ...string) error
	setPublicSSHFn func(allowed bool) error
	runCommandFn   func(script string) error
}

func (m *mockAzureClient) Up(family, size string) (azure.Info, error) {
	if m.upFn != nil {
		return m.upFn(family, size)
	}
	return azure.Info{}, nil
}

func (m *mockAzureClient) Down(del, yes bool) (azure.Info, error) {
	if m.downFn != nil {
		return m.downFn(del, yes)
	}
	return azure.Info{}, nil
}

func (m *mockAzureClient) Status() (azure.Info, error) {
	if m.statusFn != nil {
		return m.statusFn()
	}
	return azure.Info{}, nil
}

func (m *mockAzureClient) ResizeDisk(gb int) (azure.Info, error) {
	if m.resizeDiskFn != nil {
		return m.resizeDiskFn(gb)
	}
	return azure.Info{}, nil
}

func (m *mockAzureClient) Info() (azure.Info, error) {
	if m.infoFn != nil {
		return m.infoFn()
	}
	return azure.Info{}, nil
}

func (m *mockAzureClient) SSH(extra ...string) error {
	if m.sshFn != nil {
		return m.sshFn(extra...)
	}
	return nil
}

func (m *mockAzureClient) SetPublicSSH(allowed bool) error {
	if m.setPublicSSHFn != nil {
		return m.setPublicSSHFn(allowed)
	}
	return nil
}

func (m *mockAzureClient) RunCommand(script string) error {
	if m.runCommandFn != nil {
		return m.runCommandFn(script)
	}
	return nil
}

// newTestAppContext creates an appContext with a temp config dir and mock Azure client.
func newTestAppContext(t *testing.T, mock *mockAzureClient) *appContext {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st := config.Default()
	st.AdminUsername = "testuser"
	st.TailscaleClientID = "fake-client-id"
	st.TailscaleClientSecret = "fake-client-secret"
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}
	return &appContext{
		state: st,
		azure: mock,
	}
}

// ---------------------------------------------------------------------------
// restoreConnectivity tests
// ---------------------------------------------------------------------------

func TestRestoreConnectivity_PublicSSHOpen_NoAction(t *testing.T) {
	mock := &mockAzureClient{
		setPublicSSHFn: func(_ bool) error {
			t.Fatal("SetPublicSSH should not be called when public SSH is already open")
			return nil
		},
	}
	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = false

	if err := restoreConnectivity(a); err != nil {
		t.Fatalf("restoreConnectivity: %v", err)
	}
}

func TestRestoreConnectivity_TailscaleReauthSuccess(t *testing.T) {
	origFindPeer := tsFindPeer
	origGetAuthKey := tsGetAuthKey
	origPollCount := restoreConnectivityPollCount
	origPollWait := restoreConnectivityPollWait
	defer func() {
		tsFindPeer = origFindPeer
		tsGetAuthKey = origGetAuthKey
		restoreConnectivityPollCount = origPollCount
		restoreConnectivityPollWait = origPollWait
	}()

	// Simulate: first FindPeer call returns offline, second returns online.
	// Matches real behavior: tailscale up takes a few seconds to register.
	callCount := 0
	tsFindPeer = func(_ string) (*tailscale.Peer, error) {
		callCount++
		if callCount >= 2 {
			// Real response shape captured 2026-06-10:
			// HostName:"rover-vm", DNSName:"rover-vm.tail94a70e.ts.net.",
			// Online:true, TailscaleIPs:["100.88.25.46"]
			return &tailscale.Peer{
				HostName:     "rover-vm",
				DNSName:      "rover-vm.tail94a70e.ts.net.",
				Online:       true,
				TailscaleIPs: []string{"100.88.25.46"},
			}, nil
		}
		return &tailscale.Peer{
			HostName: "rover-vm",
			DNSName:  "rover-vm.tail94a70e.ts.net.",
			Online:   false,
		}, nil
	}

	tsGetAuthKey = func(_, _ string, _ []string) (string, error) {
		return "tskey-auth-fake-key", nil
	}

	restoreConnectivityPollCount = 5
	restoreConnectivityPollWait = 1 * time.Millisecond

	var runCommandScript string
	mock := &mockAzureClient{
		runCommandFn: func(script string) error {
			runCommandScript = script
			return nil
		},
		setPublicSSHFn: func(_ bool) error {
			t.Fatal("SetPublicSSH should not be called when Tailscale re-auth succeeds")
			return nil
		},
	}

	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = true

	if err := restoreConnectivity(a); err != nil {
		t.Fatalf("restoreConnectivity: %v", err)
	}

	if a.state.PublicSSHClosed != true {
		t.Error("PublicSSHClosed should remain true when Tailscale re-auth succeeds")
	}
	if runCommandScript == "" {
		t.Fatal("RunCommand was never called")
	}
	// Verify the tailscale up command includes the correct flags
	expectedFlags := []string{"--authkey='tskey-auth-fake-key'", "--ssh", "--hostname='rover-vm'", "--advertise-tags='tag:rover'"}
	for _, flag := range expectedFlags {
		if !contains(runCommandScript, flag) {
			t.Errorf("RunCommand script missing %q\ngot: %s", flag, runCommandScript)
		}
	}
}

func TestRestoreConnectivity_TailscaleNeverComesOnline_OpensPublicSSH(t *testing.T) {
	origFindPeer := tsFindPeer
	origGetAuthKey := tsGetAuthKey
	origPollCount := restoreConnectivityPollCount
	origPollWait := restoreConnectivityPollWait
	defer func() {
		tsFindPeer = origFindPeer
		tsGetAuthKey = origGetAuthKey
		restoreConnectivityPollCount = origPollCount
		restoreConnectivityPollWait = origPollWait
	}()

	// Simulate Tailscale peer that never comes online (e.g. tailscaled broken).
	tsFindPeer = func(_ string) (*tailscale.Peer, error) {
		return &tailscale.Peer{
			HostName: "rover-vm",
			Online:   false,
		}, nil
	}

	tsGetAuthKey = func(_, _ string, _ []string) (string, error) {
		return "tskey-auth-fake-key", nil
	}

	restoreConnectivityPollCount = 2
	restoreConnectivityPollWait = 1 * time.Millisecond

	setSSHCalled := false
	mock := &mockAzureClient{
		runCommandFn: func(_ string) error {
			return nil
		},
		setPublicSSHFn: func(allowed bool) error {
			if !allowed {
				t.Error("SetPublicSSH called with allowed=false; expected true (opening SSH)")
			}
			setSSHCalled = true
			return nil
		},
	}

	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = true

	if err := restoreConnectivity(a); err != nil {
		t.Fatalf("restoreConnectivity: %v", err)
	}

	if !setSSHCalled {
		t.Error("SetPublicSSH was not called as fallback")
	}
	if a.state.PublicSSHClosed != false {
		t.Error("PublicSSHClosed should be false after opening public SSH")
	}
}

func TestRestoreConnectivity_NoTailscaleCreds_OpensPublicSSH(t *testing.T) {
	origPollCount := restoreConnectivityPollCount
	origPollWait := restoreConnectivityPollWait
	defer func() {
		restoreConnectivityPollCount = origPollCount
		restoreConnectivityPollWait = origPollWait
	}()

	restoreConnectivityPollCount = 0
	restoreConnectivityPollWait = 0

	setSSHCalled := false
	mock := &mockAzureClient{
		setPublicSSHFn: func(_ bool) error {
			setSSHCalled = true
			return nil
		},
	}

	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = true
	a.state.TailscaleClientID = ""
	a.state.TailscaleClientSecret = ""
	t.Setenv("TS_AUTHKEY", "")

	if err := restoreConnectivity(a); err != nil {
		t.Fatalf("restoreConnectivity: %v", err)
	}

	if !setSSHCalled {
		t.Error("SetPublicSSH should be called when no Tailscale creds available")
	}
	if a.state.PublicSSHClosed != false {
		t.Error("PublicSSHClosed should be false after opening public SSH")
	}
}

func TestRestoreConnectivity_AuthKeyGenerationFails_OpensPublicSSH(t *testing.T) {
	origFindPeer := tsFindPeer
	origGetAuthKey := tsGetAuthKey
	origPollCount := restoreConnectivityPollCount
	origPollWait := restoreConnectivityPollWait
	defer func() {
		tsFindPeer = origFindPeer
		tsGetAuthKey = origGetAuthKey
		restoreConnectivityPollCount = origPollCount
		restoreConnectivityPollWait = origPollWait
	}()

	tsFindPeer = func(host string) (*tailscale.Peer, error) {
		return nil, &tailscale.PeerNotFoundError{Host: host}
	}

	tsGetAuthKey = func(_, _ string, _ []string) (string, error) {
		return "", errors.New("oauth failed: invalid client")
	}

	restoreConnectivityPollCount = 1
	restoreConnectivityPollWait = 1 * time.Millisecond

	setSSHCalled := false
	mock := &mockAzureClient{
		setPublicSSHFn: func(_ bool) error {
			setSSHCalled = true
			return nil
		},
	}

	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = true

	if err := restoreConnectivity(a); err != nil {
		t.Fatalf("restoreConnectivity: %v", err)
	}

	if !setSSHCalled {
		t.Error("SetPublicSSH should be called when auth key generation fails")
	}
}

func TestRestoreConnectivity_SetPublicSSHError_ReturnsError(t *testing.T) {
	origPollCount := restoreConnectivityPollCount
	origPollWait := restoreConnectivityPollWait
	defer func() {
		restoreConnectivityPollCount = origPollCount
		restoreConnectivityPollWait = origPollWait
	}()

	restoreConnectivityPollCount = 0
	restoreConnectivityPollWait = 0

	mock := &mockAzureClient{
		setPublicSSHFn: func(_ bool) error {
			return errors.New("network error")
		},
	}

	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = true
	a.state.TailscaleClientID = ""
	a.state.TailscaleClientSecret = ""
	t.Setenv("TS_AUTHKEY", "")

	err := restoreConnectivity(a)
	if err == nil {
		t.Fatal("expected error when SetPublicSSH fails")
	}
	if !contains(err.Error(), "failed to open public SSH") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRestoreConnectivity_TailscalePeerNotFound_OpensPublicSSH(t *testing.T) {
	origFindPeer := tsFindPeer
	origGetAuthKey := tsGetAuthKey
	origPollCount := restoreConnectivityPollCount
	origPollWait := restoreConnectivityPollWait
	defer func() {
		tsFindPeer = origFindPeer
		tsGetAuthKey = origGetAuthKey
		restoreConnectivityPollCount = origPollCount
		restoreConnectivityPollWait = origPollWait
	}()

	// Simulate peer completely absent from tailnet (e.g. deleted during long deallocation).
	tsFindPeer = func(host string) (*tailscale.Peer, error) {
		return nil, &tailscale.PeerNotFoundError{Host: host}
	}

	tsGetAuthKey = func(_, _ string, _ []string) (string, error) {
		return "tskey-auth-fake-key", nil
	}

	restoreConnectivityPollCount = 2
	restoreConnectivityPollWait = 1 * time.Millisecond

	setSSHCalled := false
	mock := &mockAzureClient{
		runCommandFn: func(_ string) error { return nil },
		setPublicSSHFn: func(_ bool) error {
			setSSHCalled = true
			return nil
		},
	}

	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = true

	if err := restoreConnectivity(a); err != nil {
		t.Fatalf("restoreConnectivity: %v", err)
	}

	if !setSSHCalled {
		t.Error("SetPublicSSH should be called when peer is not found")
	}
}

func TestRestoreConnectivity_TSAuthKeyEnv(t *testing.T) {
	origFindPeer := tsFindPeer
	origGetAuthKey := tsGetAuthKey
	origPollCount := restoreConnectivityPollCount
	origPollWait := restoreConnectivityPollWait
	defer func() {
		tsFindPeer = origFindPeer
		tsGetAuthKey = origGetAuthKey
		restoreConnectivityPollCount = origPollCount
		restoreConnectivityPollWait = origPollWait
	}()

	// When TS_AUTHKEY env is set, it should be used directly (not OAuth).
	getAuthKeyCalled := false
	tsGetAuthKey = func(_, _ string, _ []string) (string, error) {
		getAuthKeyCalled = true
		return "should-not-be-used", nil
	}

	tsFindPeer = func(host string) (*tailscale.Peer, error) {
		return &tailscale.Peer{HostName: host, Online: true}, nil
	}

	restoreConnectivityPollCount = 1
	restoreConnectivityPollWait = 1 * time.Millisecond

	var runCommandScript string
	mock := &mockAzureClient{
		runCommandFn: func(script string) error {
			runCommandScript = script
			return nil
		},
	}

	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = true
	t.Setenv("TS_AUTHKEY", "tskey-auth-from-env")

	if err := restoreConnectivity(a); err != nil {
		t.Fatalf("restoreConnectivity: %v", err)
	}

	if getAuthKeyCalled {
		t.Error("tsGetAuthKey should not be called when TS_AUTHKEY env is set")
	}
	if !contains(runCommandScript, "tskey-auth-from-env") {
		t.Errorf("RunCommand should use TS_AUTHKEY env value; got script: %s", runCommandScript)
	}
}

// ---------------------------------------------------------------------------
// tailscaleReady tests
// ---------------------------------------------------------------------------

func TestTailscaleReady_NoCreds(t *testing.T) {
	t.Setenv("TS_AUTHKEY", "")
	st := config.Default()
	st.TailscaleClientID = ""
	st.TailscaleClientSecret = ""
	if tailscaleReady(st) {
		t.Error("tailscaleReady should return false with no credentials")
	}
}

func TestTailscaleReady_WithCreds_PeerOnline(t *testing.T) {
	orig := tsFindPeer
	defer func() { tsFindPeer = orig }()

	tsFindPeer = func(host string) (*tailscale.Peer, error) {
		return &tailscale.Peer{HostName: host, Online: true}, nil
	}

	st := config.Default()
	st.TailscaleClientID = "id"
	st.TailscaleClientSecret = "secret"
	if !tailscaleReady(st) {
		t.Error("tailscaleReady should return true when peer is online")
	}
}

func TestTailscaleReady_WithCreds_PeerNotFound(t *testing.T) {
	orig := tsFindPeer
	defer func() { tsFindPeer = orig }()

	tsFindPeer = func(host string) (*tailscale.Peer, error) {
		return nil, &tailscale.PeerNotFoundError{Host: host}
	}

	st := config.Default()
	st.TailscaleClientID = "id"
	st.TailscaleClientSecret = "secret"
	if !tailscaleReady(st) {
		t.Error("tailscaleReady should return true when peer is not found (VM may not exist yet)")
	}
}

// ---------------------------------------------------------------------------
// azure.Info.Running() tests
// ---------------------------------------------------------------------------

func TestInfoRunning(t *testing.T) {
	// Real Azure response: "VM running" (verified 2026-06-10)
	running := azure.Info{Exists: true, PowerState: "VM running"}
	if !running.Running() {
		t.Error(`"VM running" should be Running()`)
	}
	deallocated := azure.Info{Exists: true, PowerState: "VM deallocated"}
	if deallocated.Running() {
		t.Error(`"VM deallocated" should not be Running()`)
	}
	stopped := azure.Info{Exists: true, PowerState: "VM stopped"}
	if stopped.Running() {
		t.Error(`"VM stopped" should not be Running()`)
	}
	absent := azure.Info{Exists: false, PowerState: ""}
	if absent.Running() {
		t.Error("non-existent VM should not be Running()")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && containsStr(s, sub)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// End-to-end scenario: down → up → restoreConnectivity
//
// Simulates the exact bug scenario from the original report:
//   1. VM exists, was deallocated by `rover down`
//   2. `rover up` starts the VM
//   3. Public SSH is locked down, Tailscale is logged out
//   4. restoreConnectivity re-auths Tailscale
//   5. User can connect
// ---------------------------------------------------------------------------

func TestRestoreConnectivity_FullDownUpCycle(t *testing.T) {
	origFindPeer := tsFindPeer
	origGetAuthKey := tsGetAuthKey
	origPollCount := restoreConnectivityPollCount
	origPollWait := restoreConnectivityPollWait
	defer func() {
		tsFindPeer = origFindPeer
		tsGetAuthKey = origGetAuthKey
		restoreConnectivityPollCount = origPollCount
		restoreConnectivityPollWait = origPollWait
	}()

	tsGetAuthKey = func(_, _ string, _ []string) (string, error) {
		return "tskey-auth-rover-test", nil
	}

	// Simulate real sequence:
	// - Before up: tailscale shows peer offline
	// - After tailscale up inside VM: peer comes online after 2 polls
	pollCount := 0
	tsFindPeer = func(_ string) (*tailscale.Peer, error) {
		pollCount++
		if pollCount >= 3 {
			return &tailscale.Peer{
				HostName:     "rover-vm",
				DNSName:      "rover-vm.tail94a70e.ts.net.",
				Online:       true,
				TailscaleIPs: []string{"100.88.25.46"},
			}, nil
		}
		return &tailscale.Peer{
			HostName: "rover-vm",
			DNSName:  "rover-vm.tail94a70e.ts.net.",
			Online:   false,
		}, nil
	}

	restoreConnectivityPollCount = 10
	restoreConnectivityPollWait = 1 * time.Millisecond

	var commands []string
	mock := &mockAzureClient{
		runCommandFn: func(script string) error {
			commands = append(commands, fmt.Sprintf("run-command: %s", script))
			return nil
		},
		setPublicSSHFn: func(allowed bool) error {
			commands = append(commands, fmt.Sprintf("set-public-ssh: %v", allowed))
			return nil
		},
	}

	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = true

	if err := restoreConnectivity(a); err != nil {
		t.Fatalf("restoreConnectivity: %v", err)
	}

	if len(commands) != 1 {
		t.Fatalf("expected 1 command (run-command only), got %d: %v", len(commands), commands)
	}
	if !contains(commands[0], "tailscale up") {
		t.Errorf("expected tailscale up command, got: %s", commands[0])
	}
	if a.state.PublicSSHClosed != true {
		t.Error("PublicSSHClosed should stay true — Tailscale succeeded, no need to open SSH")
	}
}
