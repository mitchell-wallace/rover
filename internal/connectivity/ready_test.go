package connectivity

import (
	"errors"
	"testing"

	"github.com/mitchell-wallace/rover/internal/tailscale"
)

func TestTailscaleReady_NoCreds(t *testing.T) {
	ts := &fakeTailscale{findPeerFn: func(string) (*tailscale.Peer, error) {
		t.Fatal("FindPeer should not be called without credentials")
		return nil, nil
	}}
	s := newTestService(t, nil, ts)
	s.State.TailscaleClientID = ""
	s.State.TailscaleClientSecret = ""

	if s.Ready() {
		t.Fatal("Ready should return false with no credentials")
	}
}

func TestTailscaleReady_WithCreds_PeerOnline(t *testing.T) {
	s := newTestService(t, nil, &fakeTailscale{
		findPeerResults: []peerResult{{peer: onlinePeer()}},
	})

	if !s.Ready() {
		t.Fatal("Ready should return true when peer is online")
	}
}

func TestTailscaleReady_WithCreds_PeerNotFound(t *testing.T) {
	s := newTestService(t, nil, &fakeTailscale{
		findPeerResults: []peerResult{{err: &tailscale.PeerNotFoundError{Host: "rover-vm"}}},
	})

	if !s.Ready() {
		t.Fatal("Ready should return true when peer is not found")
	}
}

func TestTailscaleReady_WithCreds_GenericError(t *testing.T) {
	s := newTestService(t, nil, &fakeTailscale{
		findPeerResults: []peerResult{{err: errors.New("tailscale status failed")}},
	})

	if s.Ready() {
		t.Fatal("Ready should return false on generic peer lookup errors")
	}
}
