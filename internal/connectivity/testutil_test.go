package connectivity

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/tailscale"
)

var _ AzureControl = (*fakeAzure)(nil)
var _ tailscale.Client = (*fakeTailscale)(nil)

type fakeAzure struct {
	status      azure.Info
	statusErr   error
	statusFn    func() (azure.Info, error)
	statusCalls int

	setPublicSSHCalls []bool
	setPublicSSHErr   error
	setPublicSSHFn    func(bool) error

	runCommandCtxs    []context.Context
	runCommandScripts []string
	runCommandErr     error
	runCommandFn      func(context.Context, string) error
}

func (f *fakeAzure) Status() (azure.Info, error) {
	f.statusCalls++
	if f.statusFn != nil {
		return f.statusFn()
	}
	return f.status, f.statusErr
}

func (f *fakeAzure) SetPublicSSH(allowed bool) error {
	f.setPublicSSHCalls = append(f.setPublicSSHCalls, allowed)
	if f.setPublicSSHFn != nil {
		return f.setPublicSSHFn(allowed)
	}
	return f.setPublicSSHErr
}

func (f *fakeAzure) RunCommand(ctx context.Context, script string) error {
	f.runCommandCtxs = append(f.runCommandCtxs, ctx)
	f.runCommandScripts = append(f.runCommandScripts, script)
	if f.runCommandFn != nil {
		return f.runCommandFn(ctx, script)
	}
	return f.runCommandErr
}

type peerResult struct {
	peer *tailscale.Peer
	err  error
}

type authKeyCall struct {
	clientID string
	secret   string
	tags     []string
}

type connectCall struct {
	user  string
	host  string
	extra []string
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
	findPeerCalls   []string
	findPeerResults []peerResult
	findPeerFn      func(string) (*tailscale.Peer, error)

	pingPeerCalls []*tailscale.Peer
	pingResult    bool
	pingPeerFn    func(*tailscale.Peer) bool

	getAuthKeyCalls []authKeyCall
	authKey         string
	authKeyErr      error
	getAuthKeyFn    func(string, string, []string) (string, error)

	connectCalls []connectCall
	connectErrs  []error
	connectFn    func(string, string, ...string) error

	cleanupCalls  []cleanupCall
	cleanupResult tailscale.CleanupResult
	cleanupErr    error
}

func (f *fakeTailscale) FindPeer(host string) (*tailscale.Peer, error) {
	f.findPeerCalls = append(f.findPeerCalls, host)
	if f.findPeerFn != nil {
		return f.findPeerFn(host)
	}
	if len(f.findPeerResults) == 0 {
		return nil, nil
	}
	idx := len(f.findPeerCalls) - 1
	if idx >= len(f.findPeerResults) {
		idx = len(f.findPeerResults) - 1
	}
	res := f.findPeerResults[idx]
	return res.peer, res.err
}

func (f *fakeTailscale) PingPeer(p *tailscale.Peer) bool {
	f.pingPeerCalls = append(f.pingPeerCalls, p)
	if f.pingPeerFn != nil {
		return f.pingPeerFn(p)
	}
	return f.pingResult
}

func (f *fakeTailscale) GetAuthKey(clientID, secret string, tags []string) (string, error) {
	f.getAuthKeyCalls = append(f.getAuthKeyCalls, authKeyCall{
		clientID: clientID,
		secret:   secret,
		tags:     append([]string(nil), tags...),
	})
	if f.getAuthKeyFn != nil {
		return f.getAuthKeyFn(clientID, secret, tags)
	}
	return f.authKey, f.authKeyErr
}

func (f *fakeTailscale) Connect(user, host string, extra ...string) error {
	f.connectCalls = append(f.connectCalls, connectCall{
		user:  user,
		host:  host,
		extra: append([]string(nil), extra...),
	})
	if f.connectFn != nil {
		return f.connectFn(user, host, extra...)
	}
	if len(f.connectErrs) == 0 {
		return nil
	}
	idx := len(f.connectCalls) - 1
	if idx >= len(f.connectErrs) {
		idx = len(f.connectErrs) - 1
	}
	return f.connectErrs[idx]
}

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

type commandCall struct {
	name string
	args []string
}

func recordCommandRunner(calls *[]commandCall, err error) CommandRunner {
	return func(name string, args ...string) error {
		*calls = append(*calls, commandCall{name: name, args: append([]string(nil), args...)})
		return err
	}
}

func newTestService(t *testing.T, az *fakeAzure, ts *fakeTailscale) *Service {
	t.Helper()
	st := newTestState(t)
	if az == nil {
		az = &fakeAzure{}
	}
	if ts == nil {
		ts = &fakeTailscale{}
	}
	s := New(st, az, ts)
	s.Run = func(string, ...string) error { return nil }
	s.Poll = PollConfig{Count: 2, Wait: time.Millisecond}
	s.Reconnect = ReconnectConfig{
		MaxConsecutive: 2,
		BaseWait:       time.Millisecond,
		MaxWait:        time.Millisecond,
		HealthyAfter:   time.Hour,
	}
	return s
}

func newTestState(t *testing.T) *config.State {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TS_AUTHKEY", "")
	t.Setenv("TS_OAUTH_CLIENT_ID", "")
	t.Setenv("TS_OAUTH_CLIENT_SECRET", "")

	st := config.Default()
	st.AdminUsername = "testuser"
	st.TailscaleClientID = "fake-client-id"
	st.TailscaleClientSecret = "fake-client-secret"
	st.SSHPublicKey = "/tmp/rover-test-key.pub"
	st.SSHPrivateKey = "/tmp/rover-test-key"
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}
	return st
}

func runningInfo() azure.Info {
	return azure.Info{
		Exists:     true,
		PowerState: "VM running",
		PublicIP:   "1.2.3.4",
		FQDN:       "rover-vm.australiaeast.cloudapp.azure.com",
		SSHTarget:  "testuser@rover-vm.australiaeast.cloudapp.azure.com",
	}
}

func onlinePeer() *tailscale.Peer {
	return &tailscale.Peer{
		HostName:     "rover-vm",
		DNSName:      "rover-vm.tail94a70e.ts.net.",
		Online:       true,
		TailscaleIPs: []string{"100.88.25.46"},
	}
}

func offlinePeer() *tailscale.Peer {
	return &tailscale.Peer{
		HostName: "rover-vm",
		DNSName:  "rover-vm.tail94a70e.ts.net.",
		Online:   false,
	}
}

func requireErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q", want)
	}
	if !contains(err.Error(), want) {
		t.Fatalf("error = %q, want containing %q", err, want)
	}
}

func requireNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func requireEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func requireContains(t *testing.T, got, want string) {
	t.Helper()
	if !contains(got, want) {
		t.Fatalf("expected %q to contain %q", got, want)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
