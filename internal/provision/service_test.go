// This file intentionally exceeds the soft test-file budget to keep the
// provision fakes, process-env assertions, and verify/lockdown stderr checks
// together because the D14 TS_AUTHKEY regression only shows up at that boundary.
package provision

import (
	"context"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/mitchell-wallace/rover/internal/ansible"
	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/tailscale"
	"github.com/mitchell-wallace/rover/internal/telemetry"
)

var _ AzureProvisioner = (*fakeAzure)(nil)
var _ tailscale.Client = (*fakeTailscale)(nil)

type fakeAzure struct {
	info              azure.Info
	setPublicSSHCalls []bool
}

func (f *fakeAzure) Info() (azure.Info, error) {
	return f.info, nil
}

func (f *fakeAzure) SetPublicSSH(allowed bool) error {
	f.setPublicSSHCalls = append(f.setPublicSSHCalls, allowed)
	return nil
}

type peerResult struct {
	peer *tailscale.Peer
	err  error
}

type fakeTailscale struct {
	findPeerCalls   []string
	findPeerResults []peerResult
	pingResult      bool

	getAuthKeyCalls int
	authKey         string
	authKeyErr      error
}

func (f *fakeTailscale) FindPeer(host string) (*tailscale.Peer, error) {
	f.findPeerCalls = append(f.findPeerCalls, host)
	if len(f.findPeerResults) == 0 {
		return nil, &tailscale.PeerNotFoundError{Host: host}
	}
	idx := len(f.findPeerCalls) - 1
	if idx >= len(f.findPeerResults) {
		idx = len(f.findPeerResults) - 1
	}
	res := f.findPeerResults[idx]
	return res.peer, res.err
}

func (f *fakeTailscale) PingPeer(*tailscale.Peer) bool { return f.pingResult }

func (f *fakeTailscale) GetAuthKey(_, _ string, _ []string) (string, error) {
	f.getAuthKeyCalls++
	return f.authKey, f.authKeyErr
}

func (f *fakeTailscale) Connect(string, string, ...string) error {
	return nil
}

func (f *fakeTailscale) CleanupDevices(string, string, []string, string, bool, bool) (tailscale.CleanupResult, error) {
	return tailscale.CleanupResult{}, nil
}

type fakeAnsible struct {
	params      []ansible.Params
	envAuthKeys []string
	err         error
}

func (f *fakeAnsible) run(p ansible.Params) error {
	f.params = append(f.params, p)
	f.envAuthKeys = append(f.envAuthKeys, os.Getenv("TS_AUTHKEY"))
	return f.err
}

type fakeWaiter struct {
	hosts []string
	ports []int
}

type recordingTelemetry struct {
	provisions  []telemetry.ProvisionEvent
	diagnostics []telemetry.DiagnosticEvent
}

func (*recordingTelemetry) RecordUp(telemetry.UpEvent) {}

func (r *recordingTelemetry) RecordProvision(event telemetry.ProvisionEvent) {
	r.provisions = append(r.provisions, event)
}

func (r *recordingTelemetry) RecordDiagnostic(event telemetry.DiagnosticEvent) {
	r.diagnostics = append(r.diagnostics, event)
}

func (f *fakeWaiter) wait(_ context.Context, host string, port int) {
	f.hosts = append(f.hosts, host)
	f.ports = append(f.ports, port)
}

func TestRunRejectsVMNotRunning(t *testing.T) {
	unsetEnv(t, "TS_AUTHKEY")
	az := &fakeAzure{info: runningInfo()}
	az.info.PowerState = "VM deallocated"
	s, runner, waiter := newTestService(t, az, nil)

	err := s.Run(context.Background())
	if err == nil || err.Error() != `VM is "VM deallocated", not running; run 'rover up' to start it` {
		t.Fatalf("Run error = %v", err)
	}
	if len(runner.params) != 0 {
		t.Fatalf("Ansible runs = %d, want 0", len(runner.params))
	}
	if len(waiter.hosts) != 0 {
		t.Fatalf("Wait calls = %d, want 0", len(waiter.hosts))
	}
}

func TestRunAuthKeyResolution(t *testing.T) {
	tests := []struct {
		name           string
		envKey         *string
		oauth          bool
		oauthKey       string
		wantEnvAtCall  string
		wantOAuthCalls int
	}{
		{
			name:           "env beats oauth",
			envKey:         strPtr("tskey'from$env"),
			oauth:          true,
			oauthKey:       "tskey-oauth-unused",
			wantEnvAtCall:  "tskeyfromenv",
			wantOAuthCalls: 0,
		},
		{
			name:           "oauth when env absent",
			oauth:          true,
			oauthKey:       "tskey.oauth$raw",
			wantEnvAtCall:  "tskeyoauthraw",
			wantOAuthCalls: 1,
		},
		{
			name:           "none when no credentials",
			oauth:          false,
			wantEnvAtCall:  "",
			wantOAuthCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envKey != nil {
				setEnv(t, "TS_AUTHKEY", *tt.envKey)
			} else {
				unsetEnv(t, "TS_AUTHKEY")
			}
			ts := &fakeTailscale{authKey: tt.oauthKey}
			s, runner, _ := newTestService(t, &fakeAzure{info: runningInfo()}, ts)
			if !tt.oauth {
				s.State.TailscaleClientID = ""
				s.State.TailscaleClientSecret = ""
			}

			requireNoErr(t, s.Run(context.Background()))

			if len(runner.params) != 1 {
				t.Fatalf("Ansible runs = %d, want 1", len(runner.params))
			}
			if got := runner.envAuthKeys[0]; got != tt.wantEnvAtCall {
				t.Fatalf("TS_AUTHKEY at Ansible call = %q, want %q", got, tt.wantEnvAtCall)
			}
			if _, ok := runner.params[0].ExtraVars["TS_AUTHKEY"]; ok {
				t.Fatal("TS_AUTHKEY should reach Ansible via environment, not ansible.Params")
			}
			if got := ts.getAuthKeyCalls; got != tt.wantOAuthCalls {
				t.Fatalf("GetAuthKey calls = %d, want %d", got, tt.wantOAuthCalls)
			}
			if tt.wantEnvAtCall != "" {
				if _, ok := os.LookupEnv("TS_AUTHKEY"); ok {
					t.Fatal("TS_AUTHKEY should be unset after Run returns")
				}
			}
		})
	}
}

func TestRunOAuthFailureDoesNotProvision(t *testing.T) {
	unsetEnv(t, "TS_AUTHKEY")
	ts := &fakeTailscale{authKeyErr: errors.New("oauth unavailable")}
	s, runner, waiter := newTestService(t, &fakeAzure{info: runningInfo()}, ts)

	err := s.Run(context.Background())
	if err == nil || err.Error() != "generate tailscale auth key: oauth unavailable" {
		t.Fatalf("Run error = %v", err)
	}
	if ts.getAuthKeyCalls != 1 {
		t.Fatalf("GetAuthKey calls = %d, want 1", ts.getAuthKeyCalls)
	}
	if len(ts.findPeerCalls) != 0 {
		t.Fatalf("FindPeer calls = %d, want 0", len(ts.findPeerCalls))
	}
	if len(waiter.hosts) != 0 {
		t.Fatalf("Wait calls = %d, want 0", len(waiter.hosts))
	}
	if len(runner.params) != 0 {
		t.Fatalf("Ansible runs = %d, want 0", len(runner.params))
	}
}

func TestRunRecordsProvisionOutcomeAndClassifiedDiagnostic(t *testing.T) {
	unsetEnv(t, "TS_AUTHKEY")
	s, runner, _ := newTestService(t, &fakeAzure{info: runningInfo()}, nil)
	s.State.TailscaleClientID = ""
	s.State.TailscaleClientSecret = ""
	runner.err = errors.New("provider error for /home/private/account-resource")
	recorder := &recordingTelemetry{}
	s.Telemetry = recorder

	err := s.Run(context.Background())
	if err == nil {
		t.Fatal("Run returned nil, want Ansible failure")
	}
	if len(recorder.provisions) != 1 || recorder.provisions[0].Mode != "full" || recorder.provisions[0].Success {
		t.Fatalf("provision events = %#v", recorder.provisions)
	}
	want := telemetry.DiagnosticEvent{Command: "provision", Category: "ansible_failure"}
	if len(recorder.diagnostics) != 1 || recorder.diagnostics[0] != want {
		t.Fatalf("diagnostics = %#v, want %#v", recorder.diagnostics, want)
	}
}

func TestRunHostSelection(t *testing.T) {
	tests := []struct {
		name         string
		info         azure.Info
		result       peerResult
		wantHost     string
		wantInfoLine string
	}{
		{
			name:         "online tailscale peer",
			info:         runningInfo(),
			result:       peerResult{peer: onlinePeer()},
			wantHost:     "rover-vm.tailnet.test",
			wantInfoLine: "==> Tailscale connection active. Provisioning over Tailscale (rover-vm.tailnet.test)...",
		},
		{
			name: "public ip when no tailscale peer",
			info: func() azure.Info {
				info := runningInfo()
				info.FQDN = ""
				return info
			}(),
			result:       peerResult{err: &tailscale.PeerNotFoundError{Host: "rover-vm"}},
			wantHost:     "203.0.113.10",
			wantInfoLine: "==> Provisioning rover-vm (203.0.113.10) over public IP with Ansible...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnv(t, "TS_AUTHKEY")
			ts := &fakeTailscale{findPeerResults: []peerResult{tt.result}}
			s, runner, waiter := newTestService(t, &fakeAzure{info: tt.info}, ts)
			s.State.TailscaleClientID = ""
			s.State.TailscaleClientSecret = ""

			output, err := captureStderr(t, func() error {
				return s.Run(context.Background())
			})
			requireNoErr(t, err)

			if len(waiter.hosts) != 1 || waiter.hosts[0] != tt.wantHost {
				t.Fatalf("Wait hosts = %+v, want %q", waiter.hosts, tt.wantHost)
			}
			if len(runner.params) != 1 || runner.params[0].Host != tt.wantHost {
				t.Fatalf("Ansible host = %+v, want %q", runner.params, tt.wantHost)
			}
			requireLine(t, outputLines(output), tt.wantInfoLine)
		})
	}
}

const (
	lineVerify        = "==> Verifying Tailscale connection to VM..."
	lineVerified      = "==> Tailscale connection verified."
	lineLockdown      = "==> Locking down: closing public SSH (VM stays reachable over Tailscale)..."
	lineLocked        = "==> Public SSH closed. The VM is now reachable only over Tailscale ('rover connect')."
	lineAlreadyClosed = "==> Public SSH already closed — VM reachable only over Tailscale."
	lineVerifyFailed  = "[warn] Tailscale verification failed: peer offline, not found, or unreachable — keeping public SSH OPEN on port 2222."
)

func TestRunPassesTimezoneAndLocale(t *testing.T) {
	unsetEnv(t, "TS_AUTHKEY")
	s, runner, _ := newTestService(t, &fakeAzure{info: runningInfo()}, nil)
	s.Timezone = "America/New_York"
	s.Locale = "en_US.UTF-8"

	requireNoErr(t, s.Run(context.Background()))

	if len(runner.params) != 1 {
		t.Fatalf("Ansible runs = %d, want 1", len(runner.params))
	}
	extra := runner.params[0].ExtraVars
	if extra["rover_timezone"] != "America/New_York" {
		t.Errorf("rover_timezone = %q, want America/New_York", extra["rover_timezone"])
	}
	if extra["rover_locale"] != "en_US.UTF-8" {
		t.Errorf("rover_locale = %q, want en_US.UTF-8", extra["rover_locale"])
	}
	if s.State.Timezone != "America/New_York" {
		t.Errorf("State.Timezone = %q, want America/New_York", s.State.Timezone)
	}
	if s.State.Locale != "en_US.UTF-8" {
		t.Errorf("State.Locale = %q, want en_US.UTF-8", s.State.Locale)
	}
}

func TestRunPostProvisionVerifyAndLockdown(t *testing.T) {
	tests := []struct {
		name             string
		initialClosed    bool
		findPeerResults  []peerResult
		pingResult       bool
		wantSetPublicSSH []bool
		wantPublicClosed bool
		wantExactLines   []string
	}{
		{
			name: "verified closes public ssh",
			findPeerResults: []peerResult{
				{peer: onlinePeer()},
				{peer: onlinePeer()},
			},
			pingResult:       true,
			wantSetPublicSSH: []bool{false},
			wantPublicClosed: true,
			wantExactLines:   []string{lineVerify, lineVerified, lineLockdown, lineLocked},
		},
		{
			name: "verification failure leaves public ssh open",
			findPeerResults: []peerResult{
				{peer: onlinePeer()},
				{peer: offlinePeer()},
			},
			wantPublicClosed: false,
			wantExactLines:   []string{lineVerify, lineVerifyFailed},
		},
		{
			name: "online but unpingable leaves public ssh open",
			findPeerResults: []peerResult{
				{peer: onlinePeer()},
				{peer: onlinePeer()},
			},
			pingResult:       false,
			wantPublicClosed: false,
			wantExactLines:   []string{lineVerify, lineVerifyFailed},
		},
		{
			name:          "already closed is not closed again",
			initialClosed: true,
			findPeerResults: []peerResult{
				{peer: onlinePeer()},
				{peer: onlinePeer()},
			},
			pingResult:       true,
			wantPublicClosed: true,
			wantExactLines:   []string{lineVerify, lineVerified, lineAlreadyClosed},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, "TS_AUTHKEY", "tskey-auth-lock")
			az := &fakeAzure{info: runningInfo()}
			ts := &fakeTailscale{findPeerResults: tt.findPeerResults, pingResult: tt.pingResult}
			s, _, _ := newTestService(t, az, ts)
			s.State.PublicSSHClosed = tt.initialClosed

			output, err := captureStderr(t, func() error {
				return s.Run(context.Background())
			})
			requireNoErr(t, err)

			if !sameBools(az.setPublicSSHCalls, tt.wantSetPublicSSH) {
				t.Fatalf("SetPublicSSH calls = %v, want %v", az.setPublicSSHCalls, tt.wantSetPublicSSH)
			}
			if s.State.PublicSSHClosed != tt.wantPublicClosed {
				t.Fatalf("PublicSSHClosed = %v, want %v", s.State.PublicSSHClosed, tt.wantPublicClosed)
			}
			requireLinesInOrder(t, outputLines(output), tt.wantExactLines)
		})
	}
}

func newTestService(t *testing.T, az *fakeAzure, ts *fakeTailscale) (*Service, *fakeAnsible, *fakeWaiter) {
	t.Helper()
	if az == nil {
		az = &fakeAzure{info: runningInfo()}
	}
	if ts == nil {
		ts = &fakeTailscale{}
	}
	runner := &fakeAnsible{}
	waiter := &fakeWaiter{}
	s := New(newTestState(t), az, ts, "/tmp/rover-assets")
	s.Ansible = runner.run
	s.Wait = waiter.wait
	return s, runner, waiter
}

func newTestState(t *testing.T) *config.State {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TS_OAUTH_CLIENT_ID", "")
	t.Setenv("TS_OAUTH_CLIENT_SECRET", "")
	st := config.Default()
	st.AdminUsername = "testuser"
	st.SSHPublicKey = "/tmp/rover-test-key.pub"
	st.SSHPrivateKey = "/tmp/rover-test-key"
	st.SSHListenPort = 2222
	st.TailscaleClientID = "fake-client-id"
	st.TailscaleClientSecret = "fake-client-secret"
	st.TailscaleHostname = "rover-vm"
	st.TailscaleTags = "tag:rover"
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}
	return st
}

func runningInfo() azure.Info {
	return azure.Info{
		Exists:     true,
		PowerState: "VM running",
		VMName:     "rover-vm",
		VMSize:     "Standard_B2als_v2",
		PublicIP:   "203.0.113.10",
		FQDN:       "rover.example.test",
	}
}

func onlinePeer() *tailscale.Peer {
	return &tailscale.Peer{
		HostName:     "rover-vm",
		DNSName:      "rover-vm.tailnet.test.",
		Online:       true,
		TailscaleIPs: []string{"100.64.0.1"},
	}
}

func offlinePeer() *tailscale.Peer {
	p := onlinePeer()
	p.Online = false
	return p
}

func strPtr(s string) *string {
	return &s
}

func requireNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func setEnv(t *testing.T, key, value string) {
	t.Helper()
	old, hadOld := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
	t.Cleanup(func() {
		restoreEnv(key, old, hadOld)
	})
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, hadOld := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		restoreEnv(key, old, hadOld)
	})
}

func restoreEnv(key, old string, hadOld bool) {
	if hadOld {
		_ = os.Setenv(key, old)
		return
	}
	_ = os.Unsetenv(key)
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func captureStderr(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stderr = w
	runErr := fn()
	_ = w.Close()
	os.Stderr = old
	out, readErr := io.ReadAll(r)
	_ = r.Close()
	if readErr != nil {
		t.Fatalf("read stderr: %v", readErr)
	}
	return ansiRE.ReplaceAllString(string(out), ""), runErr
}

func outputLines(output string) []string {
	output = strings.TrimRight(output, "\n")
	if output == "" {
		return nil
	}
	return strings.Split(output, "\n")
}

func requireLine(t *testing.T, lines []string, want string) {
	t.Helper()
	for _, line := range lines {
		if line == want {
			return
		}
	}
	t.Fatalf("missing exact output line %q in:\n%s", want, strings.Join(lines, "\n"))
}

func requireLinesInOrder(t *testing.T, lines, want []string) {
	t.Helper()
	start := 0
	for _, line := range want {
		found := false
		for i := start; i < len(lines); i++ {
			if lines[i] == line {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing ordered output line %q in:\n%s", line, strings.Join(lines, "\n"))
		}
	}
}

func sameBools(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
