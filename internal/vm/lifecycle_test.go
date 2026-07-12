package vm

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/tailscale"
	"github.com/mitchell-wallace/rover/internal/telemetry"
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

type sshCall struct {
	tmux  bool
	extra []string
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

	sshCalls []sshCall
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

func (f *fakeAzure) SSH(tmux bool, extra ...string) error {
	f.sshCalls = append(f.sshCalls, sshCall{tmux: tmux, extra: append([]string(nil), extra...)})
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
	runCalls            int
	runErr              error
	resizeSwapfileCalls int
	resizeSwapfileErr   error
}

type recordingTelemetry struct {
	up          []telemetry.UpEvent
	provision   []telemetry.ProvisionEvent
	diagnostics []telemetry.DiagnosticEvent
}

func (r *recordingTelemetry) RecordUp(event telemetry.UpEvent) {
	r.up = append(r.up, event)
}

func (r *recordingTelemetry) RecordProvision(event telemetry.ProvisionEvent) {
	r.provision = append(r.provision, event)
}

func (r *recordingTelemetry) RecordDiagnostic(event telemetry.DiagnosticEvent) {
	r.diagnostics = append(r.diagnostics, event)
}

func (r *recordingProvisioner) Run(context.Context) error {
	r.runCalls++
	return r.runErr
}

func (r *recordingProvisioner) ResizeSwapfile(context.Context) error {
	r.resizeSwapfileCalls++
	return r.resizeSwapfileErr
}

type testHarness struct {
	svc  *Service
	st   *config.State
	az   *fakeAzure
	ts   *fakeTailscale
	conn *recordingConn
	prov *recordingProvisioner
	tel  *recordingTelemetry
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
	tel := &recordingTelemetry{}
	svc := &Service{
		State:     st,
		Azure:     az,
		TS:        ts,
		Conn:      conn,
		Provision: prov,
		Telemetry: tel,
	}
	return &testHarness{svc: svc, st: st, az: az, ts: ts, conn: conn, prov: prov, tel: tel}
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

func TestSSH_TmuxSelection(t *testing.T) {
	tests := []struct {
		name     string
		config   bool
		options  SSHOptions
		extra    []string
		wantTmux bool
	}{
		{name: "default interactive session", config: true, wantTmux: true},
		{name: "per invocation opt out", config: true, options: SSHOptions{NoTmux: true}},
		{name: "persistent opt out", config: false},
		{name: "remote command", config: true, extra: []string{"uname", "-a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHarness(t)
			h.az.statusInfo = runningVMInfo()
			h.st.SSHSettings().Tmux = tt.config

			requireNoErr(t, h.svc.SSH(tt.options, tt.extra...))

			requireEqual(t, len(h.az.sshCalls), 1)
			requireEqual(t, h.az.sshCalls[0].tmux, tt.wantTmux)
			requireEqual(t, strings.Join(h.az.sshCalls[0].extra, "\x00"), strings.Join(tt.extra, "\x00"))
		})
	}
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
