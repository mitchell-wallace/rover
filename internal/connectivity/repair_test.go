package connectivity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mitchell-wallace/rover/internal/tailscale"
)

func TestRestoreConnectivity_PublicSSHOpen_NoAction(t *testing.T) {
	az := &fakeAzure{
		setPublicSSHFn: func(bool) error {
			t.Fatal("SetPublicSSH should not be called when public SSH is already open")
			return nil
		},
		runCommandFn: func(string) error {
			t.Fatal("RunCommand should not be called when public SSH is already open")
			return nil
		},
	}
	s := newTestService(t, az, nil)
	s.State.PublicSSHClosed = false

	requireNoErr(t, s.Restore(context.Background()))
}

func TestRestoreConnectivity_TailscaleReauthSuccess(t *testing.T) {
	az := &fakeAzure{setPublicSSHFn: func(bool) error {
		t.Fatal("SetPublicSSH should not be called when Tailscale re-auth succeeds")
		return nil
	}}
	ts := &fakeTailscale{
		authKey: "tskey-auth-fake-key",
		findPeerResults: []peerResult{
			{peer: offlinePeer()},
			{peer: onlinePeer()},
		},
		pingResult: true,
	}
	s := newTestService(t, az, ts)
	s.State.PublicSSHClosed = true

	requireNoErr(t, s.Restore(context.Background()))

	if !s.State.PublicSSHClosed {
		t.Fatal("PublicSSHClosed should remain true when Tailscale re-auth succeeds")
	}
	if len(az.runCommandScripts) != 1 {
		t.Fatalf("RunCommand calls = %d, want 1", len(az.runCommandScripts))
	}
	for _, flag := range []string{
		"--authkey='tskey-auth-fake-key'",
		"--ssh",
		"--hostname='rover-vm'",
		"--advertise-tags='tag:rover'",
	} {
		requireContains(t, az.runCommandScripts[0], flag)
	}
}

func TestRestoreConnectivity_TailscaleNeverComesOnline_OpensPublicSSH(t *testing.T) {
	az := &fakeAzure{}
	ts := &fakeTailscale{
		authKey:         "tskey-auth-fake-key",
		findPeerResults: []peerResult{{peer: offlinePeer()}},
	}
	s := newTestService(t, az, ts)
	s.State.PublicSSHClosed = true

	requireNoErr(t, s.Restore(context.Background()))

	if len(az.setPublicSSHCalls) != 1 || !az.setPublicSSHCalls[0] {
		t.Fatalf("SetPublicSSH calls = %v, want [true]", az.setPublicSSHCalls)
	}
	if s.State.PublicSSHClosed {
		t.Fatal("PublicSSHClosed should be false after opening public SSH")
	}
}

func TestRestoreConnectivity_TailscaleOnlineButUnreachable_OpensPublicSSH(t *testing.T) {
	az := &fakeAzure{}
	ts := &fakeTailscale{
		authKey:         "tskey-auth-fake-key",
		findPeerResults: []peerResult{{peer: onlinePeer()}},
		pingResult:      false,
	}
	s := newTestService(t, az, ts)
	s.State.PublicSSHClosed = true

	requireNoErr(t, s.Restore(context.Background()))

	if len(az.setPublicSSHCalls) != 1 || !az.setPublicSSHCalls[0] {
		t.Fatalf("SetPublicSSH calls = %v, want [true]", az.setPublicSSHCalls)
	}
	if s.State.PublicSSHClosed {
		t.Fatal("PublicSSHClosed should be false after opening public SSH")
	}
}

func TestRestoreConnectivity_NoTailscaleCreds_OpensPublicSSH(t *testing.T) {
	az := &fakeAzure{}
	ts := &fakeTailscale{getAuthKeyFn: func(string, string, []string) (string, error) {
		t.Fatal("GetAuthKey should not be called when OAuth is not configured")
		return "", nil
	}}
	s := newTestService(t, az, ts)
	s.State.PublicSSHClosed = true
	s.State.TailscaleClientID = ""
	s.State.TailscaleClientSecret = ""

	requireNoErr(t, s.Restore(context.Background()))

	if len(az.setPublicSSHCalls) != 1 || !az.setPublicSSHCalls[0] {
		t.Fatalf("SetPublicSSH calls = %v, want [true]", az.setPublicSSHCalls)
	}
	if len(az.runCommandScripts) != 0 {
		t.Fatalf("RunCommand should not be called without auth credentials")
	}
}

func TestRestoreConnectivity_AuthKeyGenerationFails_OpensPublicSSH(t *testing.T) {
	az := &fakeAzure{}
	s := newTestService(t, az, &fakeTailscale{authKeyErr: errors.New("oauth failed: invalid client")})
	s.State.PublicSSHClosed = true

	requireNoErr(t, s.Restore(context.Background()))

	if len(az.setPublicSSHCalls) != 1 || !az.setPublicSSHCalls[0] {
		t.Fatalf("SetPublicSSH calls = %v, want [true]", az.setPublicSSHCalls)
	}
	if len(az.runCommandScripts) != 0 {
		t.Fatalf("RunCommand should not be called when auth key generation fails")
	}
}

func TestRestoreConnectivity_SetPublicSSHError_ReturnsError(t *testing.T) {
	az := &fakeAzure{setPublicSSHErr: errors.New("network error")}
	s := newTestService(t, az, nil)
	s.State.PublicSSHClosed = true
	s.State.TailscaleClientID = ""
	s.State.TailscaleClientSecret = ""

	requireErrContains(t, s.Restore(context.Background()), "failed to open public SSH")
}

func TestRestoreConnectivity_TailscalePeerNotFound_OpensPublicSSH(t *testing.T) {
	az := &fakeAzure{}
	ts := &fakeTailscale{
		authKey: "tskey-auth-fake-key",
		findPeerResults: []peerResult{
			{err: &tailscale.PeerNotFoundError{Host: "rover-vm"}},
		},
	}
	s := newTestService(t, az, ts)
	s.State.PublicSSHClosed = true

	requireNoErr(t, s.Restore(context.Background()))

	if len(az.setPublicSSHCalls) != 1 || !az.setPublicSSHCalls[0] {
		t.Fatalf("SetPublicSSH calls = %v, want [true]", az.setPublicSSHCalls)
	}
}

func TestRestoreConnectivity_TSAuthKeyEnv(t *testing.T) {
	az := &fakeAzure{}
	ts := &fakeTailscale{
		findPeerResults: []peerResult{{peer: onlinePeer()}},
		pingResult:      true,
		getAuthKeyFn: func(string, string, []string) (string, error) {
			t.Fatal("GetAuthKey should not be called when TS_AUTHKEY is set")
			return "", nil
		},
	}
	s := newTestService(t, az, ts)
	t.Setenv("TS_AUTHKEY", "tskey-auth-from-env")
	s.State.PublicSSHClosed = true

	requireNoErr(t, s.Restore(context.Background()))
	requireContains(t, az.runCommandScripts[0], "tskey-auth-from-env")
}

func TestRestoreConnectivity_FullDownUpCycle(t *testing.T) {
	az := &fakeAzure{}
	ts := &fakeTailscale{
		authKey: "tskey-auth-rover-test",
		findPeerResults: []peerResult{
			{peer: offlinePeer()},
			{peer: onlinePeer()},
		},
		pingResult: true,
	}
	s := newTestService(t, az, ts)
	s.State.PublicSSHClosed = true

	requireNoErr(t, s.Restore(context.Background()))

	if len(az.runCommandScripts) != 1 || len(az.setPublicSSHCalls) != 0 {
		t.Fatalf("commands: run=%d setPublicSSH=%v, want one run-command only", len(az.runCommandScripts), az.setPublicSSHCalls)
	}
	requireContains(t, az.runCommandScripts[0], "tailscale up")
	if !s.State.PublicSSHClosed {
		t.Fatal("PublicSSHClosed should stay true when Tailscale repair succeeds")
	}
}

func TestRestoreConnectivity_ContextCancelled(t *testing.T) {
	az := &fakeAzure{}
	ts := &fakeTailscale{
		authKey:         "tskey-auth-test",
		findPeerResults: []peerResult{{peer: offlinePeer()}},
	}
	s := newTestService(t, az, ts)
	s.State.PublicSSHClosed = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	requireNoErr(t, s.Restore(ctx))

	if len(az.setPublicSSHCalls) != 1 || !az.setPublicSSHCalls[0] {
		t.Fatalf("SetPublicSSH calls = %v, want [true]", az.setPublicSSHCalls)
	}
	if len(ts.findPeerCalls) != 0 {
		t.Fatalf("FindPeer should not be polled after cancellation, got %v", ts.findPeerCalls)
	}
}

func TestReauthenticate_RunCommandScriptContainsTailscaleUp(t *testing.T) {
	az := &fakeAzure{}
	s := newTestService(t, az, &fakeTailscale{
		authKey:         "tskey-auth-good",
		findPeerResults: []peerResult{{peer: onlinePeer()}},
		pingResult:      true,
	})

	if !s.Reauthenticate(context.Background()) {
		t.Fatal("Reauthenticate should succeed")
	}
	requireContains(t, az.runCommandScripts[0], "tailscale up")
}

func TestReauthenticate_SanitizesAuthKey(t *testing.T) {
	az := &fakeAzure{}
	s := newTestService(t, az, &fakeTailscale{
		authKey:         "tskey-auth-good;rm -rf /",
		findPeerResults: []peerResult{{peer: onlinePeer()}},
		pingResult:      true,
	})

	if !s.Reauthenticate(context.Background()) {
		t.Fatal("Reauthenticate should succeed")
	}
	if contains(az.runCommandScripts[0], "rm -rf") {
		t.Fatalf("auth key was not sanitized: %s", az.runCommandScripts[0])
	}
}

func TestBuildReauthScript(t *testing.T) {
	script := buildReauthScript("tskey-auth-ABC123", "rover-vm", "tag:rover")

	requireContains(t, script, "timeout 120s tailscale up")
	requireContains(t, script, "systemctl restart tailscaled")
	lines := strings.Split(strings.TrimSpace(script), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if contains(last, "|| true") {
		t.Fatalf("final tailscale-up line must propagate failures, got: %s", last)
	}
	for _, want := range []string{"tskey-auth-ABC123", "rover-vm", "tag:rover"} {
		requireContains(t, script, want)
	}
	if contains(script, "--force-reauth") {
		t.Fatalf("script must not use --force-reauth: %s", script)
	}
}

func TestBuildReauthScript_SanitizesShellMetachars(t *testing.T) {
	script := buildReauthScript("k", "rover-vm'; rm -rf /; echo '", "tag:rover")
	if contains(script, "rm -rf") {
		t.Fatalf("shell metacharacters in hostname were not stripped: %s", script)
	}
}
