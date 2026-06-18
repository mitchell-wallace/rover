package cmd

import (
	"strings"
	"testing"

	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/connectivity"
	"github.com/mitchell-wallace/rover/internal/tailscale"
)

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
