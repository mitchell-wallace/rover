// Package tailscale is Rover's local-side Tailscale integration: it inspects the
// tailnet via the `tailscale` CLI to find the Rover VM and connects to it over
// Tailscale SSH, independent of the Azure public IP.
package tailscale

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Peer is the subset of `tailscale status --json` we care about.
type Peer struct {
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	Online       bool     `json:"Online"`
	TailscaleIPs []string `json:"TailscaleIPs"`
}

type statusJSON struct {
	BackendState string           `json:"BackendState"`
	Peer         map[string]*Peer `json:"Peer"`
}

// Available reports whether the local `tailscale` CLI is installed.
func Available() bool {
	_, err := exec.LookPath("tailscale")
	return err == nil
}

// ErrNotInstalled is returned when the tailscale CLI is missing.
var ErrNotInstalled = fmt.Errorf("tailscale CLI not found; install it from https://tailscale.com/download and run 'tailscale up'")

// ErrNotRunning is returned when the local tailscale backend isn't connected.
var ErrNotRunning = fmt.Errorf("local Tailscale is not connected; run 'tailscale up'")

// PeerNotFoundError indicates the named host isn't in the tailnet.
type PeerNotFoundError struct{ Host string }

func (e *PeerNotFoundError) Error() string {
	return fmt.Sprintf("%q is not in your tailnet", e.Host)
}

// FindPeer returns the tailnet peer matching host (by short hostname or the
// leading label of its MagicDNS name).
func FindPeer(host string) (*Peer, error) {
	if !Available() {
		return nil, ErrNotInstalled
	}
	out, err := exec.Command("tailscale", "status", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("tailscale status: %w", err)
	}
	var st statusJSON
	if err := json.Unmarshal(out, &st); err != nil {
		return nil, fmt.Errorf("parse tailscale status: %w", err)
	}
	if st.BackendState != "Running" {
		return nil, ErrNotRunning
	}
	want := strings.ToLower(host)
	for _, p := range st.Peer {
		if strings.EqualFold(p.HostName, want) || strings.HasPrefix(strings.ToLower(p.DNSName), want+".") {
			return p, nil
		}
	}
	return nil, &PeerNotFoundError{Host: host}
}

// Target returns the best address to connect to (MagicDNS name, else IP).
func (p *Peer) Target() string {
	if p.DNSName != "" {
		return strings.TrimSuffix(p.DNSName, ".")
	}
	if len(p.TailscaleIPs) > 0 {
		return p.TailscaleIPs[0]
	}
	return p.HostName
}

// Connect opens an interactive Tailscale SSH session to user@host's peer.
func Connect(user, host string, extra ...string) error {
	if !Available() {
		return ErrNotInstalled
	}
	args := append([]string{"ssh", user + "@" + host}, extra...)
	cmd := exec.Command("tailscale", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
