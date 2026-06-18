package tailscale

import "testing"

// Compile-time check that CLI implements Client and NewClient returns a Client.
var _ Client = CLI{}
var _ Client = NewClient()

func TestNewClientReturnsClient(t *testing.T) {
	c := NewClient()
	if _, ok := c.(CLI); !ok {
		t.Fatalf("NewClient() = %T, want CLI", c)
	}
}

// TestCLI_PingPeer_Delegates exercises real delegation through the package func
// without requiring the tailscale binary: PingPeer returns false for a nil or
// offline peer before touching the CLI, so CLI{}.PingPeer must match.
func TestCLI_PingPeer_Delegates(t *testing.T) {
	c := NewClient()
	if c.PingPeer(nil) != false {
		t.Error("PingPeer(nil) = true, want false")
	}
	if c.PingPeer(&Peer{Online: false}) != false {
		t.Error("PingPeer(offline) = true, want false")
	}
}
