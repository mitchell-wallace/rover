package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/connectivity"
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
	restartFn      func() (azure.Info, error)
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

func (m *mockAzureClient) Restart() (azure.Info, error) {
	if m.restartFn != nil {
		return m.restartFn()
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

type mockTailscaleClient struct {
	findPeerFn       func(string) (*tailscale.Peer, error)
	pingPeerFn       func(*tailscale.Peer) bool
	getAuthKeyFn     func(string, string, []string) (string, error)
	connectFn        func(string, string, ...string) error
	cleanupDevicesFn func(string, string, []string, string, bool, bool) (tailscale.CleanupResult, error)
}

func (m *mockTailscaleClient) FindPeer(host string) (*tailscale.Peer, error) {
	if m.findPeerFn != nil {
		return m.findPeerFn(host)
	}
	return nil, &tailscale.PeerNotFoundError{Host: host}
}

func (m *mockTailscaleClient) PingPeer(p *tailscale.Peer) bool {
	if m.pingPeerFn != nil {
		return m.pingPeerFn(p)
	}
	return false
}

func (m *mockTailscaleClient) GetAuthKey(clientID, secret string, tags []string) (string, error) {
	if m.getAuthKeyFn != nil {
		return m.getAuthKeyFn(clientID, secret, tags)
	}
	return "", nil
}

func (m *mockTailscaleClient) Connect(user, host string, extra ...string) error {
	if m.connectFn != nil {
		return m.connectFn(user, host, extra...)
	}
	return nil
}

func (m *mockTailscaleClient) CleanupDevices(clientID, secret string, tags []string, hostname string, deleteOnline, dryRun bool) (tailscale.CleanupResult, error) {
	if m.cleanupDevicesFn != nil {
		return m.cleanupDevicesFn(clientID, secret, tags, hostname, deleteOnline, dryRun)
	}
	return tailscale.CleanupResult{}, nil
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
	conn := connectivity.New(st, mock, &mockTailscaleClient{})
	conn.Run = func(string, ...string) error { return nil }
	return &appContext{
		state: st,
		azure: mock,
		conn:  conn,
	}
}

func mockTS(t *testing.T, a *appContext) *mockTailscaleClient {
	t.Helper()
	ts, ok := a.conn.TS.(*mockTailscaleClient)
	if !ok {
		t.Fatalf("unexpected tailscale client %T", a.conn.TS)
	}
	return ts
}

func stubTSPing(t *testing.T, a *appContext, reachable bool) {
	t.Helper()
	mockTS(t, a).pingPeerFn = func(*tailscale.Peer) bool { return reachable }
}

func setFastPoll(a *appContext, count int) {
	a.conn.Poll = connectivity.PollConfig{Count: count, Wait: time.Millisecond}
}

func setFastReconnect(a *appContext, maxConsecutive int) {
	a.conn.Reconnect = connectivity.ReconnectConfig{
		MaxConsecutive: maxConsecutive,
		BaseWait:       time.Millisecond,
		MaxWait:        time.Millisecond,
		HealthyAfter:   time.Hour,
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

	if err := restoreConnectivity(context.Background(), a); err != nil {
		t.Fatalf("restoreConnectivity: %v", err)
	}
}

func TestRestoreConnectivity_TailscaleReauthSuccess(t *testing.T) {
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
	stubTSPing(t, a, true)
	setFastPoll(a, 5)
	ts := mockTS(t, a)
	// Simulate: first FindPeer call returns offline, second returns online.
	// Matches real behavior: tailscale up takes a few seconds to register.
	callCount := 0
	ts.findPeerFn = func(_ string) (*tailscale.Peer, error) {
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
	ts.getAuthKeyFn = func(_, _ string, _ []string) (string, error) {
		return "tskey-auth-fake-key", nil
	}

	if err := restoreConnectivity(context.Background(), a); err != nil {
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
	setFastPoll(a, 2)
	ts := mockTS(t, a)
	// Simulate Tailscale peer that never comes online (e.g. tailscaled broken).
	ts.findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return &tailscale.Peer{
			HostName: "rover-vm",
			Online:   false,
		}, nil
	}
	ts.getAuthKeyFn = func(_, _ string, _ []string) (string, error) {
		return "tskey-auth-fake-key", nil
	}

	if err := restoreConnectivity(context.Background(), a); err != nil {
		t.Fatalf("restoreConnectivity: %v", err)
	}

	if !setSSHCalled {
		t.Error("SetPublicSSH was not called as fallback")
	}
	if a.state.PublicSSHClosed != false {
		t.Error("PublicSSHClosed should be false after opening public SSH")
	}
}

func TestRestoreConnectivity_TailscaleOnlineButUnreachable_OpensPublicSSH(t *testing.T) {
	setSSHCalled := false
	mock := &mockAzureClient{
		runCommandFn: func(_ string) error { return nil },
		setPublicSSHFn: func(allowed bool) error {
			if !allowed {
				t.Error("SetPublicSSH called with allowed=false; expected true")
			}
			setSSHCalled = true
			return nil
		},
	}
	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = true
	stubTSPing(t, a, false)
	setFastPoll(a, 2)
	ts := mockTS(t, a)
	ts.findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return &tailscale.Peer{
			HostName:     "rover-vm",
			DNSName:      "rover-vm.tail94a70e.ts.net.",
			Online:       true,
			TailscaleIPs: []string{"100.88.25.46"},
		}, nil
	}
	ts.getAuthKeyFn = func(_, _ string, _ []string) (string, error) {
		return "tskey-auth-fake-key", nil
	}

	if err := restoreConnectivity(context.Background(), a); err != nil {
		t.Fatalf("restoreConnectivity: %v", err)
	}
	if !setSSHCalled {
		t.Error("SetPublicSSH should be called when online peer is not reachable")
	}
	if a.state.PublicSSHClosed {
		t.Error("PublicSSHClosed should be false after opening public SSH")
	}
}

func TestRestoreConnectivity_NoTailscaleCreds_OpensPublicSSH(t *testing.T) {
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
	setFastPoll(a, 0)
	t.Setenv("TS_AUTHKEY", "")

	if err := restoreConnectivity(context.Background(), a); err != nil {
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
	setSSHCalled := false
	mock := &mockAzureClient{
		setPublicSSHFn: func(_ bool) error {
			setSSHCalled = true
			return nil
		},
	}

	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = true
	setFastPoll(a, 1)
	ts := mockTS(t, a)
	ts.findPeerFn = func(host string) (*tailscale.Peer, error) {
		return nil, &tailscale.PeerNotFoundError{Host: host}
	}
	ts.getAuthKeyFn = func(_, _ string, _ []string) (string, error) {
		return "", errors.New("oauth failed: invalid client")
	}

	if err := restoreConnectivity(context.Background(), a); err != nil {
		t.Fatalf("restoreConnectivity: %v", err)
	}

	if !setSSHCalled {
		t.Error("SetPublicSSH should be called when auth key generation fails")
	}
}

func TestRestoreConnectivity_SetPublicSSHError_ReturnsError(t *testing.T) {
	mock := &mockAzureClient{
		setPublicSSHFn: func(_ bool) error {
			return errors.New("network error")
		},
	}

	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = true
	a.state.TailscaleClientID = ""
	a.state.TailscaleClientSecret = ""
	setFastPoll(a, 0)
	t.Setenv("TS_AUTHKEY", "")

	err := restoreConnectivity(context.Background(), a)
	if err == nil {
		t.Fatal("expected error when SetPublicSSH fails")
	}
	if !contains(err.Error(), "failed to open public SSH") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRestoreConnectivity_TailscalePeerNotFound_OpensPublicSSH(t *testing.T) {
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
	setFastPoll(a, 2)
	ts := mockTS(t, a)
	// Simulate peer completely absent from tailnet (e.g. deleted during long deallocation).
	ts.findPeerFn = func(host string) (*tailscale.Peer, error) {
		return nil, &tailscale.PeerNotFoundError{Host: host}
	}
	ts.getAuthKeyFn = func(_, _ string, _ []string) (string, error) {
		return "tskey-auth-fake-key", nil
	}

	if err := restoreConnectivity(context.Background(), a); err != nil {
		t.Fatalf("restoreConnectivity: %v", err)
	}

	if !setSSHCalled {
		t.Error("SetPublicSSH should be called when peer is not found")
	}
}

func TestRestoreConnectivity_TSAuthKeyEnv(t *testing.T) {
	// When TS_AUTHKEY env is set, it should be used directly (not OAuth).
	getAuthKeyCalled := false
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
	stubTSPing(t, a, true)
	setFastPoll(a, 1)
	ts := mockTS(t, a)
	ts.getAuthKeyFn = func(_, _ string, _ []string) (string, error) {
		getAuthKeyCalled = true
		return "should-not-be-used", nil
	}
	ts.findPeerFn = func(host string) (*tailscale.Peer, error) {
		return &tailscale.Peer{HostName: host, Online: true}, nil
	}

	if err := restoreConnectivity(context.Background(), a); err != nil {
		t.Fatalf("restoreConnectivity: %v", err)
	}

	if getAuthKeyCalled {
		t.Error("GetAuthKey should not be called when TS_AUTHKEY env is set")
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
	a := newTestAppContext(t, &mockAzureClient{})
	a.state.TailscaleClientID = ""
	a.state.TailscaleClientSecret = ""
	if tailscaleReady(a) {
		t.Error("tailscaleReady should return false with no credentials")
	}
}

func TestTailscaleReady_WithCreds_PeerOnline(t *testing.T) {
	a := newTestAppContext(t, &mockAzureClient{})
	mockTS(t, a).findPeerFn = func(host string) (*tailscale.Peer, error) {
		return &tailscale.Peer{HostName: host, Online: true}, nil
	}

	a.state.TailscaleClientID = "id"
	a.state.TailscaleClientSecret = "secret"
	if !tailscaleReady(a) {
		t.Error("tailscaleReady should return true when peer is online")
	}
}

func TestTailscaleReady_WithCreds_PeerNotFound(t *testing.T) {
	a := newTestAppContext(t, &mockAzureClient{})
	mockTS(t, a).findPeerFn = func(host string) (*tailscale.Peer, error) {
		return nil, &tailscale.PeerNotFoundError{Host: host}
	}

	a.state.TailscaleClientID = "id"
	a.state.TailscaleClientSecret = "secret"
	if !tailscaleReady(a) {
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
	return strings.Contains(s, sub)
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
	stubTSPing(t, a, true)
	setFastPoll(a, 10)
	ts := mockTS(t, a)
	ts.getAuthKeyFn = func(_, _ string, _ []string) (string, error) {
		return "tskey-auth-rover-test", nil
	}
	// Simulate real sequence:
	// - Before up: tailscale shows peer offline
	// - After tailscale up inside VM: peer comes online after 2 polls
	pollCount := 0
	ts.findPeerFn = func(_ string) (*tailscale.Peer, error) {
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

	if err := restoreConnectivity(context.Background(), a); err != nil {
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

// ---------------------------------------------------------------------------
// syncConnection error propagation tests
// ---------------------------------------------------------------------------

func TestSyncConnection_SavesState(t *testing.T) {
	mock := &mockAzureClient{}
	a := newTestAppContext(t, mock)

	info := azure.Info{
		Exists:     true,
		PublicIP:   "1.2.3.4",
		FQDN:       "rover-vm.australiaeast.cloudapp.azure.com",
		VMSize:     "Standard_B2als_v2",
		PowerState: "VM running",
	}
	if err := a.syncConnection(info); err != nil {
		t.Fatalf("syncConnection: %v", err)
	}
	if a.state.Connection.PublicIP != "1.2.3.4" {
		t.Errorf("Connection.PublicIP = %q, want 1.2.3.4", a.state.Connection.PublicIP)
	}
	if a.state.Connection.VMSize != "Standard_B2als_v2" {
		t.Errorf("Connection.VMSize = %q, want Standard_B2als_v2", a.state.Connection.VMSize)
	}
}

// ---------------------------------------------------------------------------
// doDown state persistence tests
// ---------------------------------------------------------------------------

func TestDoDown_Delete_SavesState(t *testing.T) {
	mock := &mockAzureClient{
		downFn: func(_, _ bool) (azure.Info, error) {
			return azure.Info{}, nil
		},
		statusFn: func() (azure.Info, error) {
			return azure.Info{Exists: true, PowerState: "VM running"}, nil
		},
		runCommandFn: func(_ string) error { return nil },
	}

	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = true
	a.state.AnsibleApplied = true

	if err := doDown(a, true, true); err != nil {
		t.Fatalf("doDown: %v", err)
	}
	if a.state.PublicSSHClosed {
		t.Error("PublicSSHClosed should be false after delete")
	}
	if a.state.AnsibleApplied {
		t.Error("AnsibleApplied should be false after delete")
	}
	if a.state.Connection.Exists {
		t.Error("Connection.Exists should be false after delete")
	}
}

func TestDoDown_Deallocate_SyncsConnection(t *testing.T) {
	mock := &mockAzureClient{
		downFn: func(_, _ bool) (azure.Info, error) {
			return azure.Info{
				Exists:     true,
				PowerState: "VM deallocated",
				PublicIP:   "1.2.3.4",
			}, nil
		},
	}

	a := newTestAppContext(t, mock)
	if err := doDown(a, false, true); err != nil {
		t.Fatalf("doDown: %v", err)
	}
	if a.state.Connection.PowerState != "VM deallocated" {
		t.Errorf("Connection.PowerState = %q, want VM deallocated", a.state.Connection.PowerState)
	}
}

// ---------------------------------------------------------------------------
// doDisk state persistence tests
// ---------------------------------------------------------------------------

func TestDoDisk_AlreadyCorrectSize_SavesState(t *testing.T) {
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{Exists: true, DiskSizeGB: 50}, nil
		},
	}
	a := newTestAppContext(t, mock)
	if err := doDisk(a, 50, true); err != nil {
		t.Fatalf("doDisk: %v", err)
	}
	if a.state.DiskSizeGB != 50 {
		t.Errorf("DiskSizeGB = %d, want 50", a.state.DiskSizeGB)
	}
}

func TestDoDisk_NoVM_RecordsSize(t *testing.T) {
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{Exists: false}, nil
		},
	}
	a := newTestAppContext(t, mock)
	if err := doDisk(a, 100, true); err != nil {
		t.Fatalf("doDisk: %v", err)
	}
	if a.state.DiskSizeGB != 100 {
		t.Errorf("DiskSizeGB = %d, want 100", a.state.DiskSizeGB)
	}
}

func TestDoDisk_CannotShrink(t *testing.T) {
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{Exists: true, DiskSizeGB: 100}, nil
		},
	}
	a := newTestAppContext(t, mock)
	err := doDisk(a, 50, true)
	if err == nil {
		t.Fatal("expected error when shrinking disk")
	}
	if !contains(err.Error(), "cannot shrink") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoDisk_MinimumSize(t *testing.T) {
	a := newTestAppContext(t, &mockAzureClient{})
	err := doDisk(a, 10, true)
	if err == nil {
		t.Fatal("expected error for disk size below minimum")
	}
	if !contains(err.Error(), "at least 30 GiB") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// restoreConnectivity context cancellation tests
// ---------------------------------------------------------------------------

func TestRestoreConnectivity_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := &mockAzureClient{
		runCommandFn: func(_ string) error { return nil },
		setPublicSSHFn: func(_ bool) error {
			return nil
		},
	}
	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = true
	setFastPoll(a, 100)
	ts := mockTS(t, a)
	ts.findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return &tailscale.Peer{HostName: "rover-vm", Online: false}, nil
	}
	ts.getAuthKeyFn = func(_, _ string, _ []string) (string, error) {
		return "tskey-auth-test", nil
	}

	if err := restoreConnectivity(ctx, a); err != nil {
		t.Fatalf("restoreConnectivity with cancelled context: %v", err)
	}
}

// ---------------------------------------------------------------------------
// doRestart tests
// ---------------------------------------------------------------------------

func TestDoRestart_NoVM(t *testing.T) {
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{Exists: false}, nil
		},
	}
	a := newTestAppContext(t, mock)
	err := doRestart(a)
	if err == nil {
		t.Fatal("expected error when no VM")
	}
	if !contains(err.Error(), "no VM provisioned") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoRestart_VMNotRunning(t *testing.T) {
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{Exists: true, PowerState: "VM deallocated"}, nil
		},
	}
	a := newTestAppContext(t, mock)
	err := doRestart(a)
	if err == nil {
		t.Fatal("expected error when VM is not running")
	}
	if !contains(err.Error(), "not running") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoRestart_RestartsAndSyncsConnection(t *testing.T) {
	calledRestart := false
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{Exists: true, PowerState: "VM running", VMName: "rover-vm"}, nil
		},
		restartFn: func() (azure.Info, error) {
			calledRestart = true
			return azure.Info{
				Exists:     true,
				PowerState: "VM running",
				VMName:     "rover-vm",
				VMSize:     "Standard_B2as_v2",
				PublicIP:   "1.2.3.4",
				FQDN:       "rover-vm.australiaeast.cloudapp.azure.com",
			}, nil
		},
	}
	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = false

	if err := doRestart(a); err != nil {
		t.Fatalf("doRestart: %v", err)
	}
	if !calledRestart {
		t.Fatal("Restart was not called")
	}
	if a.state.Connection.PowerState != "VM running" {
		t.Errorf("Connection.PowerState = %q, want VM running", a.state.Connection.PowerState)
	}
	if a.state.Connection.FQDN != "rover-vm.australiaeast.cloudapp.azure.com" {
		t.Errorf("Connection.FQDN = %q", a.state.Connection.FQDN)
	}
}

func TestDoRestart_RestoresConnectivityWhenPublicSSHClosed(t *testing.T) {
	var runCommandScript string
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{Exists: true, PowerState: "VM running", VMName: "rover-vm"}, nil
		},
		restartFn: func() (azure.Info, error) {
			return azure.Info{Exists: true, PowerState: "VM running", VMName: "rover-vm"}, nil
		},
		runCommandFn: func(script string) error {
			runCommandScript = script
			return nil
		},
	}
	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = true
	stubTSPing(t, a, true)
	setFastPoll(a, 1)
	ts := mockTS(t, a)
	ts.findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return &tailscale.Peer{HostName: "rover-vm", Online: true}, nil
	}
	ts.getAuthKeyFn = func(_, _ string, _ []string) (string, error) {
		return "tskey-auth-good", nil
	}

	if err := doRestart(a); err != nil {
		t.Fatalf("doRestart: %v", err)
	}
	if runCommandScript == "" {
		t.Fatal("RunCommand was not called to restore Tailscale")
	}
	if !contains(runCommandScript, "tailscale up") {
		t.Errorf("RunCommand script missing tailscale up: %s", runCommandScript)
	}
}

// ---------------------------------------------------------------------------
// doCommand tests
// ---------------------------------------------------------------------------

func TestDoCommand_NoVM(t *testing.T) {
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{Exists: false}, nil
		},
	}
	a := newTestAppContext(t, mock)
	err := doCommand(a, []string{"ls"})
	if err == nil {
		t.Fatal("expected error when no VM")
	}
	if !contains(err.Error(), "no VM provisioned") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoCommand_VMNotRunning(t *testing.T) {
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{Exists: true, PowerState: "VM deallocated"}, nil
		},
	}
	a := newTestAppContext(t, mock)
	err := doCommand(a, []string{"ls"})
	if err == nil {
		t.Fatal("expected error when VM not running")
	}
	if !contains(err.Error(), "not running") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoCommand_TailscalePreferred(t *testing.T) {
	var calledName string
	var calledArgs []string
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{
				Exists:     true,
				PowerState: "VM running",
				PublicIP:   "1.2.3.4",
				FQDN:       "rover-vm.australiaeast.cloudapp.azure.com",
			}, nil
		},
	}
	a := newTestAppContext(t, mock)
	stubTSPing(t, a, true)
	mockTS(t, a).findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return &tailscale.Peer{
			HostName:     "rover-vm",
			DNSName:      "rover-vm.tail94a70e.ts.net.",
			Online:       true,
			TailscaleIPs: []string{"100.88.25.46"},
		}, nil
	}
	a.conn.Run = func(name string, args ...string) error {
		calledName = name
		calledArgs = args
		return nil
	}

	if err := doCommand(a, []string{"ls", "-la"}); err != nil {
		t.Fatalf("doCommand: %v", err)
	}

	if calledName != "tailscale" {
		t.Errorf("expected tailscale, got %q", calledName)
	}
	expectedTarget := "testuser@rover-vm.tail94a70e.ts.net"
	if len(calledArgs) < 3 || calledArgs[0] != "ssh" || calledArgs[1] != expectedTarget {
		t.Errorf("unexpected args: %v", calledArgs)
	}
	// Last arg should be the command string
	if calledArgs[len(calledArgs)-1] != "ls -la" {
		t.Errorf("expected command 'ls -la', got %q", calledArgs[len(calledArgs)-1])
	}
}

func TestDoCommand_SSHFallback(t *testing.T) {
	var calledName string
	var calledArgs []string
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{
				Exists:     true,
				PowerState: "VM running",
				PublicIP:   "1.2.3.4",
				FQDN:       "rover-vm.australiaeast.cloudapp.azure.com",
				SSHTarget:  "testuser@rover-vm.australiaeast.cloudapp.azure.com",
			}, nil
		},
	}
	a := newTestAppContext(t, mock)
	mockTS(t, a).findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return nil, &tailscale.PeerNotFoundError{Host: "rover-vm"}
	}
	a.conn.Run = func(name string, args ...string) error {
		calledName = name
		calledArgs = args
		return nil
	}

	if err := doCommand(a, []string{"uname", "-a"}); err != nil {
		t.Fatalf("doCommand: %v", err)
	}

	if calledName != "ssh" {
		t.Errorf("expected ssh, got %q", calledName)
	}
	// Should include -p, port, -o options, and the command
	argsStr := strings.Join(calledArgs, " ")
	if !contains(argsStr, "29472") {
		t.Errorf("expected port 29472 in args: %v", calledArgs)
	}
	if !contains(argsStr, "BatchMode=yes") {
		t.Errorf("expected BatchMode=yes in args: %v", calledArgs)
	}
	if calledArgs[len(calledArgs)-1] != "uname -a" {
		t.Errorf("expected command 'uname -a', got %q", calledArgs[len(calledArgs)-1])
	}
}

func TestDoCommand_SSHFallback_TailscaleOffline(t *testing.T) {
	var calledName string
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{
				Exists:     true,
				PowerState: "VM running",
				FQDN:       "rover-vm.australiaeast.cloudapp.azure.com",
			}, nil
		},
	}
	a := newTestAppContext(t, mock)
	mockTS(t, a).findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return &tailscale.Peer{HostName: "rover-vm", Online: false}, nil
	}
	a.conn.Run = func(name string, _ ...string) error {
		calledName = name
		return nil
	}

	if err := doCommand(a, []string{"ls"}); err != nil {
		t.Fatalf("doCommand: %v", err)
	}

	if calledName != "ssh" {
		t.Errorf("expected ssh fallback when Tailscale peer is offline, got %q", calledName)
	}
}

func TestDoCommand_SSHFallback_TailscaleOnlineButUnreachable(t *testing.T) {
	var calledName string
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{
				Exists:     true,
				PowerState: "VM running",
				FQDN:       "rover-vm.australiaeast.cloudapp.azure.com",
			}, nil
		},
	}
	a := newTestAppContext(t, mock)
	stubTSPing(t, a, false)
	mockTS(t, a).findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return &tailscale.Peer{HostName: "rover-vm", Online: true}, nil
	}
	a.conn.Run = func(name string, _ ...string) error {
		calledName = name
		return nil
	}

	if err := doCommand(a, []string{"ls"}); err != nil {
		t.Fatalf("doCommand: %v", err)
	}

	if calledName != "ssh" {
		t.Errorf("expected ssh fallback when Tailscale is unreachable, got %q", calledName)
	}
}

func TestDoCommand_RepairsClosedPublicSSHWhenTailscaleUnreachable(t *testing.T) {
	repaired := false
	var calledName string
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{
				Exists:     true,
				PowerState: "VM running",
				FQDN:       "rover-vm.australiaeast.cloudapp.azure.com",
			}, nil
		},
		runCommandFn: func(_ string) error {
			repaired = true
			return nil
		},
	}
	a := newTestAppContext(t, mock)
	a.state.PublicSSHClosed = true
	setFastPoll(a, 1)
	ts := mockTS(t, a)
	ts.findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return &tailscale.Peer{HostName: "rover-vm", Online: true}, nil
	}
	ts.getAuthKeyFn = func(_, _ string, _ []string) (string, error) {
		return "tskey-auth-good", nil
	}
	ts.pingPeerFn = func(*tailscale.Peer) bool {
		return repaired
	}
	a.conn.Run = func(name string, _ ...string) error {
		calledName = name
		return nil
	}

	if err := doCommand(a, []string{"ls"}); err != nil {
		t.Fatalf("doCommand: %v", err)
	}

	if calledName != "tailscale" {
		t.Errorf("expected tailscale after repair, got %q", calledName)
	}
}

func TestDoCommand_CommandFailure(t *testing.T) {
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{
				Exists:     true,
				PowerState: "VM running",
				FQDN:       "rover-vm.australiaeast.cloudapp.azure.com",
			}, nil
		},
	}
	a := newTestAppContext(t, mock)
	mockTS(t, a).findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return nil, &tailscale.PeerNotFoundError{Host: "rover-vm"}
	}
	a.conn.Run = func(_ string, _ ...string) error {
		return errors.New("exit status 1")
	}

	err := doCommand(a, []string{"false"})
	if err == nil {
		t.Fatal("expected error when remote command fails")
	}
	if !contains(err.Error(), "exit status 1") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoCommand_StatusError(t *testing.T) {
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{}, errors.New("az cli not found")
		},
	}
	a := newTestAppContext(t, mock)
	err := doCommand(a, []string{"ls"})
	if err == nil {
		t.Fatal("expected error when status fails")
	}
	if !contains(err.Error(), "az cli not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoCommand_EmptyHost_SSHFallback(t *testing.T) {
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{
				Exists:     true,
				PowerState: "VM running",
			}, nil
		},
	}
	a := newTestAppContext(t, mock)
	mockTS(t, a).findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return nil, &tailscale.PeerNotFoundError{Host: "rover-vm"}
	}
	a.conn.Run = func(_ string, _ ...string) error {
		return nil
	}

	err := doCommand(a, []string{"ls"})
	if err == nil {
		t.Fatal("expected error when no connection target available")
	}
	if !contains(err.Error(), "no connection target") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoCommand_TailscaleNotInstalled_FallsBackToSSH(t *testing.T) {
	var calledName string
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{
				Exists:     true,
				PowerState: "VM running",
				FQDN:       "rover-vm.australiaeast.cloudapp.azure.com",
			}, nil
		},
	}
	a := newTestAppContext(t, mock)
	mockTS(t, a).findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return nil, tailscale.ErrNotInstalled
	}
	a.conn.Run = func(name string, _ ...string) error {
		calledName = name
		return nil
	}

	if err := doCommand(a, []string{"ls"}); err != nil {
		t.Fatalf("doCommand: %v", err)
	}
	if calledName != "ssh" {
		t.Errorf("expected ssh fallback when tailscale not installed, got %q", calledName)
	}
}

func TestDoCommand_EmptyArgs(t *testing.T) {
	var calledArgs []string
	mock := &mockAzureClient{
		statusFn: func() (azure.Info, error) {
			return azure.Info{
				Exists:     true,
				PowerState: "VM running",
				FQDN:       "rover-vm.australiaeast.cloudapp.azure.com",
			}, nil
		},
	}
	a := newTestAppContext(t, mock)
	mockTS(t, a).findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return nil, &tailscale.PeerNotFoundError{Host: "rover-vm"}
	}
	a.conn.Run = func(_ string, args ...string) error {
		calledArgs = args
		return nil
	}

	if err := doCommand(a, []string{}); err != nil {
		t.Fatalf("doCommand with empty args: %v", err)
	}
	lastArg := calledArgs[len(calledArgs)-1]
	if lastArg != "" {
		t.Errorf("expected empty command string, got %q", lastArg)
	}
}

// ---------------------------------------------------------------------------
// doConnect tests
// ---------------------------------------------------------------------------

func TestDoConnect_PeerOnline(t *testing.T) {
	a := newTestAppContext(t, &mockAzureClient{})
	stubTSPing(t, a, true)
	mockTS(t, a).findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return &tailscale.Peer{
			HostName:     "rover-vm",
			DNSName:      "rover-vm.tail94a70e.ts.net.",
			Online:       true,
			TailscaleIPs: []string{"100.88.25.46"},
		}, nil
	}

	connectCalled := false
	mockTS(t, a).connectFn = func(_, _ string, _ ...string) error {
		connectCalled = true
		return nil
	}

	if err := doConnect(a); err != nil {
		t.Fatalf("doConnect with online peer: %v", err)
	}
	if !connectCalled {
		t.Error("Tailscale Connect was not called")
	}
}

func TestDoConnect_ReconnectsAfterDroppedSession(t *testing.T) {
	a := newTestAppContext(t, &mockAzureClient{})
	setFastReconnect(a, 2)
	ts := mockTS(t, a)
	ts.findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return &tailscale.Peer{
			HostName:     "rover-vm",
			DNSName:      "rover-vm.tail94a70e.ts.net.",
			Online:       true,
			TailscaleIPs: []string{"100.88.25.46"},
		}, nil
	}
	ts.pingPeerFn = func(*tailscale.Peer) bool { return true }

	connectCalls := 0
	ts.connectFn = func(_, _ string, _ ...string) error {
		connectCalls++
		if connectCalls == 1 {
			return errors.New("connection timed out")
		}
		return nil
	}

	if err := doConnect(a); err != nil {
		t.Fatalf("doConnect with dropped session: %v", err)
	}
	if connectCalls != 2 {
		t.Fatalf("expected initial connect plus one reconnect, got %d calls", connectCalls)
	}
}

func TestDoConnect_CapsRapidReconnectFailures(t *testing.T) {
	a := newTestAppContext(t, &mockAzureClient{})
	setFastReconnect(a, 2)
	ts := mockTS(t, a)
	ts.findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return &tailscale.Peer{
			HostName: "rover-vm",
			DNSName:  "rover-vm.tail94a70e.ts.net.",
			Online:   true,
		}, nil
	}
	ts.pingPeerFn = func(*tailscale.Peer) bool { return true }

	connectCalls := 0
	ts.connectFn = func(_, _ string, _ ...string) error {
		connectCalls++
		return errors.New("permission denied")
	}

	err := doConnect(a)
	if err == nil {
		t.Fatal("expected error after capped reconnect failures")
	}
	if !contains(err.Error(), "after 2 reconnect attempts") {
		t.Fatalf("unexpected error: %v", err)
	}
	if connectCalls != 3 {
		t.Fatalf("expected initial connect plus two reconnect attempts, got %d calls", connectCalls)
	}
}

func TestDoConnect_PeerOffline(t *testing.T) {
	a := newTestAppContext(t, &mockAzureClient{})
	mockTS(t, a).findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return &tailscale.Peer{HostName: "rover-vm", Online: false}, nil
	}

	err := doConnect(a)
	if err == nil {
		t.Fatal("expected error when peer is offline")
	}
	if !contains(err.Error(), "offline") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoConnect_PeerOnlineButUnreachable(t *testing.T) {
	t.Setenv("TS_AUTHKEY", "")
	a := newTestAppContext(t, &mockAzureClient{})
	a.state.TailscaleClientID = ""
	a.state.TailscaleClientSecret = ""
	stubTSPing(t, a, false)
	mockTS(t, a).findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return &tailscale.Peer{HostName: "rover-vm", Online: true}, nil
	}

	err := doConnect(a)
	if err == nil {
		t.Fatal("expected error when online peer is not reachable")
	}
	if !contains(err.Error(), "not reachable") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoConnect_RepairsOnlineButUnreachablePeer(t *testing.T) {
	repaired := false
	var runCommandScript string
	mock := &mockAzureClient{
		runCommandFn: func(script string) error {
			runCommandScript = script
			repaired = true
			return nil
		},
	}

	var connectedUser, connectedHost string
	a := newTestAppContext(t, mock)
	setFastPoll(a, 1)
	ts := mockTS(t, a)
	ts.findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return &tailscale.Peer{
			HostName:     "rover-vm",
			DNSName:      "rover-vm.tail94a70e.ts.net.",
			Online:       true,
			TailscaleIPs: []string{"100.88.25.46"},
		}, nil
	}
	ts.getAuthKeyFn = func(_, _ string, _ []string) (string, error) {
		return "tskey-auth-good", nil
	}
	ts.pingPeerFn = func(*tailscale.Peer) bool {
		return repaired
	}
	ts.connectFn = func(user, host string, _ ...string) error {
		connectedUser = user
		connectedHost = host
		return nil
	}

	if err := doConnect(a); err != nil {
		t.Fatalf("doConnect: %v", err)
	}
	if !contains(runCommandScript, "tailscale up") {
		t.Fatalf("expected remote tailscale up, got script: %s", runCommandScript)
	}
	if connectedUser != "testuser" || connectedHost != "rover-vm.tail94a70e.ts.net" {
		t.Errorf("unexpected connection target %s@%s", connectedUser, connectedHost)
	}
}

func TestDoConnect_PeerNotFound(t *testing.T) {
	a := newTestAppContext(t, &mockAzureClient{})
	mockTS(t, a).findPeerFn = func(host string) (*tailscale.Peer, error) {
		return nil, &tailscale.PeerNotFoundError{Host: host}
	}

	err := doConnect(a)
	if err == nil {
		t.Fatal("expected error when peer not found")
	}
	if !contains(err.Error(), "not reachable") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoConnect_TailscaleNotInstalled(t *testing.T) {
	a := newTestAppContext(t, &mockAzureClient{})
	mockTS(t, a).findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return nil, tailscale.ErrNotInstalled
	}

	err := doConnect(a)
	if err == nil {
		t.Fatal("expected error when tailscale not installed")
	}
	if !contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoConnect_TailscaleNotRunning(t *testing.T) {
	a := newTestAppContext(t, &mockAzureClient{})
	mockTS(t, a).findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return nil, tailscale.ErrNotRunning
	}

	err := doConnect(a)
	if err == nil {
		t.Fatal("expected error when tailscale not running")
	}
	if !contains(err.Error(), "not connected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoConnect_GenericError(t *testing.T) {
	a := newTestAppContext(t, &mockAzureClient{})
	mockTS(t, a).findPeerFn = func(_ string) (*tailscale.Peer, error) {
		return nil, errors.New("unexpected network error")
	}

	err := doConnect(a)
	if err == nil {
		t.Fatal("expected error on generic FindPeer failure")
	}
	if !contains(err.Error(), "unexpected network error") {
		t.Errorf("unexpected error: %v", err)
	}
}
