package connectivity

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/tailscale"
)

func TestDoCommand_NoVM(t *testing.T) {
	s := newTestService(t, &fakeAzure{status: azure.Info{Exists: false}}, nil)

	requireErrContains(t, s.RunCommand(context.Background(), []string{"ls"}), "no VM provisioned")
}

func TestDoCommand_VMNotRunning(t *testing.T) {
	s := newTestService(t, &fakeAzure{status: azure.Info{Exists: true, PowerState: "VM deallocated"}}, nil)

	requireErrContains(t, s.RunCommand(context.Background(), []string{"ls"}), "not running")
}

func TestDoCommand_TailscalePreferred(t *testing.T) {
	var calls []commandCall
	s := newTestService(t, &fakeAzure{status: runningInfo()}, &fakeTailscale{
		findPeerResults: []peerResult{{peer: onlinePeer()}},
		pingResult:      true,
	})
	s.Run = recordCommandRunner(&calls, nil)

	requireNoErr(t, s.RunCommand(context.Background(), []string{"ls", "-la"}))

	requireEqual(t, calls[0].name, "tailscale")
	want := []string{"ssh", "testuser@rover-vm.tail94a70e.ts.net", "--", "ls -la"}
	if !reflect.DeepEqual(calls[0].args, want) {
		t.Fatalf("args = %v, want %v", calls[0].args, want)
	}
}

func TestDoCommand_SSHFallback(t *testing.T) {
	var calls []commandCall
	s := newTestService(t, &fakeAzure{status: runningInfo()}, &fakeTailscale{
		findPeerResults: []peerResult{{err: &tailscale.PeerNotFoundError{Host: "rover-vm"}}},
	})
	s.Run = recordCommandRunner(&calls, nil)

	requireNoErr(t, s.RunCommand(context.Background(), []string{"uname", "-a"}))

	requireEqual(t, calls[0].name, "ssh")
	args := strings.Join(calls[0].args, " ")
	requireContains(t, args, "29472")
	requireContains(t, args, "BatchMode=yes")
	requireEqual(t, calls[0].args[len(calls[0].args)-1], "uname -a")
}

func TestDoCommand_SSHFallback_TailscaleOffline(t *testing.T) {
	var calls []commandCall
	s := newTestService(t, &fakeAzure{status: runningInfo()}, &fakeTailscale{
		findPeerResults: []peerResult{{peer: offlinePeer()}},
	})
	s.Run = recordCommandRunner(&calls, nil)

	requireNoErr(t, s.RunCommand(context.Background(), []string{"ls"}))
	requireEqual(t, calls[0].name, "ssh")
}

func TestDoCommand_SSHFallback_TailscaleOnlineButUnreachable(t *testing.T) {
	var calls []commandCall
	s := newTestService(t, &fakeAzure{status: runningInfo()}, &fakeTailscale{
		findPeerResults: []peerResult{{peer: onlinePeer()}},
		pingResult:      false,
	})
	s.Run = recordCommandRunner(&calls, nil)

	requireNoErr(t, s.RunCommand(context.Background(), []string{"ls"}))
	requireEqual(t, calls[0].name, "ssh")
}

func TestDoCommand_RepairsClosedPublicSSHWhenTailscaleUnreachable(t *testing.T) {
	repaired := false
	az := &fakeAzure{status: runningInfo(), runCommandFn: func(string) error {
		repaired = true
		return nil
	}}
	ts := &fakeTailscale{
		authKey:         "tskey-auth-good",
		findPeerResults: []peerResult{{peer: onlinePeer()}},
		pingPeerFn: func(*tailscale.Peer) bool {
			return repaired
		},
	}
	var calls []commandCall
	s := newTestService(t, az, ts)
	s.State.PublicSSHClosed = true
	s.Run = recordCommandRunner(&calls, nil)

	requireNoErr(t, s.RunCommand(context.Background(), []string{"ls"}))

	requireEqual(t, calls[0].name, "tailscale")
	if len(az.setPublicSSHCalls) != 0 {
		t.Fatalf("SetPublicSSH should not be called when repair succeeds, got %v", az.setPublicSSHCalls)
	}
}

func TestDoCommand_CommandFailure(t *testing.T) {
	s := newTestService(t, &fakeAzure{status: runningInfo()}, &fakeTailscale{
		findPeerResults: []peerResult{{err: &tailscale.PeerNotFoundError{Host: "rover-vm"}}},
	})
	s.Run = func(name string, _ ...string) error {
		return fmt.Errorf("%s: %w", name, errors.New("exit status 1"))
	}

	requireErrContains(t, s.RunCommand(context.Background(), []string{"false"}), "ssh: exit status 1")
}

func TestDoCommand_StatusError(t *testing.T) {
	s := newTestService(t, &fakeAzure{statusErr: errors.New("az cli not found")}, nil)

	requireErrContains(t, s.RunCommand(context.Background(), []string{"ls"}), "az cli not found")
}

func TestDoCommand_EmptyHost_SSHFallback(t *testing.T) {
	s := newTestService(t, &fakeAzure{status: azure.Info{Exists: true, PowerState: "VM running"}}, &fakeTailscale{
		findPeerResults: []peerResult{{err: &tailscale.PeerNotFoundError{Host: "rover-vm"}}},
	})

	requireErrContains(t, s.RunCommand(context.Background(), []string{"ls"}), "no connection target")
}

func TestDoCommand_TailscaleNotInstalled_FallsBackToSSH(t *testing.T) {
	var calls []commandCall
	s := newTestService(t, &fakeAzure{status: runningInfo()}, &fakeTailscale{
		findPeerResults: []peerResult{{err: tailscale.ErrNotInstalled}},
	})
	s.Run = recordCommandRunner(&calls, nil)

	requireNoErr(t, s.RunCommand(context.Background(), []string{"ls"}))
	requireEqual(t, calls[0].name, "ssh")
}

func TestDoCommand_EmptyArgs(t *testing.T) {
	var calls []commandCall
	s := newTestService(t, &fakeAzure{status: runningInfo()}, &fakeTailscale{
		findPeerResults: []peerResult{{err: &tailscale.PeerNotFoundError{Host: "rover-vm"}}},
	})
	s.Run = recordCommandRunner(&calls, nil)

	requireNoErr(t, s.RunCommand(context.Background(), nil))
	requireEqual(t, calls[0].args[len(calls[0].args)-1], "")
}

func TestDoConnect_PeerOnline(t *testing.T) {
	ts := &fakeTailscale{findPeerResults: []peerResult{{peer: onlinePeer()}}, pingResult: true}
	s := newTestService(t, nil, ts)

	requireNoErr(t, s.Connect(context.Background()))

	requireEqual(t, len(ts.connectCalls), 1)
	requireEqual(t, ts.connectCalls[0].user, "testuser")
	requireEqual(t, ts.connectCalls[0].host, "rover-vm.tail94a70e.ts.net")
}

func TestDoConnect_ReconnectsAfterDroppedSession(t *testing.T) {
	ts := &fakeTailscale{
		findPeerResults: []peerResult{{peer: onlinePeer()}},
		pingResult:      true,
		connectErrs:     []error{errors.New("connection timed out"), nil},
	}
	s := newTestService(t, nil, ts)

	requireNoErr(t, s.Connect(context.Background()))
	requireEqual(t, len(ts.connectCalls), 2)
}

func TestDoConnect_CapsRapidReconnectFailures(t *testing.T) {
	ts := &fakeTailscale{
		findPeerResults: []peerResult{{peer: onlinePeer()}},
		pingResult:      true,
		connectErrs:     []error{errors.New("permission denied")},
	}
	s := newTestService(t, nil, ts)

	requireErrContains(t, s.Connect(context.Background()), "after 2 reconnect attempts")
	requireEqual(t, len(ts.connectCalls), 3)
}

func TestDoConnect_ReconnectDoesNotOpenPublicSSH(t *testing.T) {
	repaired := false
	az := &fakeAzure{runCommandFn: func(string) error {
		repaired = true
		return nil
	}}
	var ts *fakeTailscale
	ts = &fakeTailscale{
		authKey:         "tskey-auth-good",
		findPeerResults: []peerResult{{peer: onlinePeer()}},
		connectErrs:     []error{errors.New("connection dropped"), nil},
		pingPeerFn: func(*tailscale.Peer) bool {
			if len(ts.connectCalls) == 0 {
				return true
			}
			return repaired
		},
	}
	s := newTestService(t, az, ts)
	s.State.PublicSSHClosed = true

	requireNoErr(t, s.Connect(context.Background()))
	requireEqual(t, len(ts.connectCalls), 2)
	if len(az.setPublicSSHCalls) != 0 {
		t.Fatalf("reconnect repair must not open public SSH, got %v", az.setPublicSSHCalls)
	}
}

func TestDoConnect_PeerOffline(t *testing.T) {
	s := newTestService(t, nil, &fakeTailscale{findPeerResults: []peerResult{{peer: offlinePeer()}}})

	requireErrContains(t, s.Connect(context.Background()), "offline")
}

func TestDoConnect_PeerOnlineButUnreachable(t *testing.T) {
	s := newTestService(t, nil, &fakeTailscale{
		findPeerResults: []peerResult{{peer: onlinePeer()}},
		pingResult:      false,
	})
	s.State.TailscaleClientID = ""
	s.State.TailscaleClientSecret = ""

	requireErrContains(t, s.Connect(context.Background()), "not reachable")
}

func TestDoConnect_RepairsOnlineButUnreachablePeer(t *testing.T) {
	repaired := false
	az := &fakeAzure{runCommandFn: func(string) error {
		repaired = true
		return nil
	}}
	ts := &fakeTailscale{
		authKey:         "tskey-auth-good",
		findPeerResults: []peerResult{{peer: onlinePeer()}},
		pingPeerFn: func(*tailscale.Peer) bool {
			return repaired
		},
	}
	s := newTestService(t, az, ts)

	requireNoErr(t, s.Connect(context.Background()))

	requireContains(t, az.runCommandScripts[0], "tailscale up")
	requireEqual(t, ts.connectCalls[0].user, "testuser")
	requireEqual(t, ts.connectCalls[0].host, "rover-vm.tail94a70e.ts.net")
}

func TestDoConnect_PeerNotFound(t *testing.T) {
	s := newTestService(t, nil, &fakeTailscale{
		findPeerResults: []peerResult{{err: &tailscale.PeerNotFoundError{Host: "rover-vm"}}},
	})

	requireErrContains(t, s.Connect(context.Background()), "not reachable")
}

func TestDoConnect_TailscaleNotInstalled(t *testing.T) {
	s := newTestService(t, nil, &fakeTailscale{findPeerResults: []peerResult{{err: tailscale.ErrNotInstalled}}})

	err := s.Connect(context.Background())
	if err == nil || err.Error() != tailscale.ErrNotInstalled.Error() {
		t.Fatalf("error = %v, want %q", err, tailscale.ErrNotInstalled.Error())
	}
}

func TestDoConnect_TailscaleNotRunning(t *testing.T) {
	s := newTestService(t, nil, &fakeTailscale{findPeerResults: []peerResult{{err: tailscale.ErrNotRunning}}})

	err := s.Connect(context.Background())
	if err == nil || err.Error() != tailscale.ErrNotRunning.Error() {
		t.Fatalf("error = %v, want %q", err, tailscale.ErrNotRunning.Error())
	}
}

func TestDoConnect_GenericError(t *testing.T) {
	s := newTestService(t, nil, &fakeTailscale{findPeerResults: []peerResult{{err: errors.New("unexpected network error")}}})

	requireErrContains(t, s.Connect(context.Background()), "unexpected network error")
}
