package vm

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/tailscale"
)

var _ AzureLifecycle = (*fakeAzure)(nil)
var _ tailscale.Client = (*fakeTailscale)(nil)
var _ connRestorer = (*recordingConn)(nil)
var _ provisioner = (*recordingProvisioner)(nil)

type upCall struct {
	family string
	size   string
}

type downCall struct {
	del bool
	yes bool
}

type fakeAzure struct {
	upCalls []upCall
	upInfo  azure.Info
	upErr   error

	downCalls []downCall
	downInfo  azure.Info
	downErr   error

	statusCalls int
	statusInfo  azure.Info
	statusErr   error

	restartCalls int
	restartInfo  azure.Info
	restartErr   error

	resizeDiskCalls []int
	resizeDiskInfo  azure.Info
	resizeDiskErr   error

	sshCalls [][]string
	sshErr   error

	runCommandScripts []string
	runCommandErr     error
}

func (f *fakeAzure) Up(family, size string) (azure.Info, error) {
	f.upCalls = append(f.upCalls, upCall{family: family, size: size})
	return f.upInfo, f.upErr
}

func (f *fakeAzure) Down(del, yes bool) (azure.Info, error) {
	f.downCalls = append(f.downCalls, downCall{del: del, yes: yes})
	return f.downInfo, f.downErr
}

func (f *fakeAzure) Status() (azure.Info, error) {
	f.statusCalls++
	return f.statusInfo, f.statusErr
}

func (f *fakeAzure) Restart() (azure.Info, error) {
	f.restartCalls++
	return f.restartInfo, f.restartErr
}

func (f *fakeAzure) ResizeDisk(gb int) (azure.Info, error) {
	f.resizeDiskCalls = append(f.resizeDiskCalls, gb)
	return f.resizeDiskInfo, f.resizeDiskErr
}

func (f *fakeAzure) SSH(extra ...string) error {
	f.sshCalls = append(f.sshCalls, append([]string(nil), extra...))
	return f.sshErr
}

func (f *fakeAzure) RunCommand(_ context.Context, script string) error {
	f.runCommandScripts = append(f.runCommandScripts, script)
	return f.runCommandErr
}

type cleanupCall struct {
	clientID     string
	secret       string
	tags         []string
	hostname     string
	deleteOnline bool
	dryRun       bool
}

type fakeTailscale struct {
	cleanupCalls  []cleanupCall
	cleanupResult tailscale.CleanupResult
	cleanupErr    error
}

func (f *fakeTailscale) FindPeer(host string) (*tailscale.Peer, error) {
	return nil, &tailscale.PeerNotFoundError{Host: host}
}

func (f *fakeTailscale) PingPeer(*tailscale.Peer) bool { return false }

func (f *fakeTailscale) GetAuthKey(string, string, []string) (string, error) {
	return "", nil
}

func (f *fakeTailscale) Connect(string, string, ...string) error { return nil }

func (f *fakeTailscale) CleanupDevices(clientID, secret string, tags []string, hostname string, deleteOnline, dryRun bool) (tailscale.CleanupResult, error) {
	f.cleanupCalls = append(f.cleanupCalls, cleanupCall{
		clientID:     clientID,
		secret:       secret,
		tags:         append([]string(nil), tags...),
		hostname:     hostname,
		deleteOnline: deleteOnline,
		dryRun:       dryRun,
	})
	return f.cleanupResult, f.cleanupErr
}

type recordingConn struct {
	ready        bool
	readyCalls   int
	restoreCalls int
	restoreErr   error
}

func (r *recordingConn) Ready() bool {
	r.readyCalls++
	return r.ready
}

func (r *recordingConn) Restore(context.Context) error {
	r.restoreCalls++
	return r.restoreErr
}

type recordingProvisioner struct {
	runCalls int
	runErr   error
}

func (r *recordingProvisioner) Run(context.Context) error {
	r.runCalls++
	return r.runErr
}

type testHarness struct {
	svc  *Service
	st   *config.State
	az   *fakeAzure
	ts   *fakeTailscale
	conn *recordingConn
	prov *recordingProvisioner
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	forceNonInteractive(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TS_AUTHKEY", "")
	t.Setenv("TS_OAUTH_CLIENT_ID", "")
	t.Setenv("TS_OAUTH_CLIENT_SECRET", "")

	st := config.Default()
	st.AdminUsername = "testuser"
	st.SSHPublicKey = "/tmp/rover-test-key.pub"
	st.SSHPrivateKey = "/tmp/rover-test-key"
	st.TailscaleClientID = "fake-client-id"
	st.TailscaleClientSecret = "fake-client-secret"
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

	az := &fakeAzure{
		upInfo:      runningVMInfo(),
		restartInfo: runningVMInfo(),
	}
	ts := &fakeTailscale{}
	conn := &recordingConn{ready: true}
	prov := &recordingProvisioner{}
	svc := &Service{
		State:     st,
		Azure:     az,
		TS:        ts,
		Conn:      conn,
		Provision: prov,
	}
	return &testHarness{svc: svc, st: st, az: az, ts: ts, conn: conn, prov: prov}
}

func forceNonInteractive(t *testing.T) {
	t.Helper()
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdin: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = old
		_ = r.Close()
	})
}

func runningVMInfo() azure.Info {
	return azure.Info{
		Exists:     true,
		PowerState: "VM running",
		VMName:     "rover-vm",
		VMSize:     "Standard_B2as_v2",
		DiskSizeGB: 30,
	}
}

func TestUp_FreshCreateTailscaleNotReadyDeclinedConfirmAborts(t *testing.T) {
	h := newTestHarness(t)
	h.conn.ready = false
	h.az.statusInfo = azure.Info{Exists: false}

	err := h.svc.Up(context.Background(), "burstable", "small", false, false)

	requireErrContains(t, err, "configure Tailscale")
	requireEqual(t, len(h.az.upCalls), 0)
	requireEqual(t, h.prov.runCalls, 0)
	requireEqual(t, h.conn.readyCalls, 1)
}

func TestUp_FreshCreateAutoProvisionsUnlessNoProvision(t *testing.T) {
	t.Run("auto provisions", func(t *testing.T) {
		h := newTestHarness(t)
		h.az.statusInfo = azure.Info{Exists: false}

		requireNoErr(t, h.svc.Up(context.Background(), "burstable", "small", true, false))

		requireEqual(t, len(h.az.upCalls), 1)
		requireEqual(t, h.az.upCalls[0].family, "burstable")
		requireEqual(t, h.az.upCalls[0].size, "small")
		requireEqual(t, h.prov.runCalls, 1)
		requireEqual(t, h.st.Connection.Exists, true)
	})

	t.Run("skips with no provision", func(t *testing.T) {
		h := newTestHarness(t)
		h.conn.ready = false
		h.az.statusInfo = azure.Info{Exists: false}

		requireNoErr(t, h.svc.Up(context.Background(), "burstable", "small", true, true))

		requireEqual(t, len(h.az.upCalls), 1)
		requireEqual(t, h.prov.runCalls, 0)
		requireEqual(t, h.conn.readyCalls, 0)
	})
}

func TestUp_ExistingVMRestoresConnectivity(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = azure.Info{Exists: true, PowerState: "VM deallocated", VMSize: "Standard_B2as_v2"}

	requireNoErr(t, h.svc.Up(context.Background(), "burstable", "small", true, false))

	requireEqual(t, len(h.az.upCalls), 1)
	requireEqual(t, h.conn.restoreCalls, 1)
	requireEqual(t, h.prov.runCalls, 0)
}

func TestDown_Delete_SavesState(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = azure.Info{Exists: true, PowerState: "VM running"}
	h.st.PublicSSHClosed = true
	h.st.AnsibleApplied = true
	h.st.Connection = config.Connection{Exists: true, PowerState: "VM running"}

	requireNoErr(t, h.svc.Down(context.Background(), true, true))

	requireEqual(t, len(h.az.downCalls), 1)
	requireEqual(t, h.az.downCalls[0].del, true)
	requireEqual(t, len(h.az.runCommandScripts), 1)
	requireContains(t, h.az.runCommandScripts[0], "tailscale logout")
	requireEqual(t, len(h.ts.cleanupCalls), 1)
	requireEqual(t, h.ts.cleanupCalls[0].deleteOnline, true)
	requireEqual(t, h.st.PublicSSHClosed, false)
	requireEqual(t, h.st.AnsibleApplied, false)
	requireEqual(t, h.st.Connection.Exists, false)
	reloaded, err := config.Load()
	requireNoErr(t, err)
	requireEqual(t, reloaded.PublicSSHClosed, false)
	requireEqual(t, reloaded.AnsibleApplied, false)
	requireEqual(t, reloaded.Connection.Exists, false)
}

func TestDown_Deallocate_SyncsConnection(t *testing.T) {
	h := newTestHarness(t)
	h.az.downInfo = azure.Info{
		Exists:     true,
		PowerState: "VM deallocated",
		PublicIP:   "1.2.3.4",
	}

	requireNoErr(t, h.svc.Down(context.Background(), false, true))

	requireEqual(t, len(h.az.downCalls), 1)
	requireEqual(t, h.az.downCalls[0].del, false)
	requireEqual(t, h.st.Connection.PowerState, "VM deallocated")
	requireEqual(t, h.st.Connection.PublicIP, "1.2.3.4")
	requireEqual(t, len(h.ts.cleanupCalls), 0)
}

func TestRestart_NoVM(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = azure.Info{Exists: false}

	err := h.svc.Restart(context.Background())

	requireErrContains(t, err, "no VM provisioned")
	requireEqual(t, h.az.restartCalls, 0)
}

func TestRestart_VMNotRunning(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = azure.Info{Exists: true, PowerState: "VM deallocated"}

	err := h.svc.Restart(context.Background())

	requireErrContains(t, err, "not running")
	requireEqual(t, h.az.restartCalls, 0)
}

func TestRestart_RestartsAndSyncsConnection(t *testing.T) {
	h := newTestHarness(t)
	h.az.statusInfo = azure.Info{Exists: true, PowerState: "VM running", VMName: "rover-vm"}
	h.az.restartInfo = azure.Info{
		Exists:     true,
		PowerState: "VM running",
		VMName:     "rover-vm",
		VMSize:     "Standard_B2as_v2",
		FQDN:       "rover-vm.australiaeast.cloudapp.azure.com",
	}

	requireNoErr(t, h.svc.Restart(context.Background()))

	requireEqual(t, h.az.restartCalls, 1)
	requireEqual(t, h.st.Connection.PowerState, "VM running")
	requireEqual(t, h.st.Connection.FQDN, "rover-vm.australiaeast.cloudapp.azure.com")
}

func TestRestart_RestoresConnectivityWhenPublicSSHClosed(t *testing.T) {
	h := newTestHarness(t)
	h.st.PublicSSHClosed = true
	h.az.statusInfo = azure.Info{Exists: true, PowerState: "VM running", VMName: "rover-vm"}

	requireNoErr(t, h.svc.Restart(context.Background()))

	requireEqual(t, h.conn.restoreCalls, 1)
}

func requireNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func requireErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want containing %q", err, want)
	}
}

func requireContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected %q to contain %q", got, want)
	}
}

func requireEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}
