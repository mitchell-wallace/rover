package tailscale

import (
	"reflect"
	"testing"
)

func TestPeerTarget(t *testing.T) {
	tests := []struct {
		name string
		peer Peer
		want string
	}{
		{
			"DNSName preferred",
			Peer{DNSName: "rover-vm.tailnet.ts.net.", TailscaleIPs: []string{"100.64.0.1"}, HostName: "rover-vm"},
			"rover-vm.tailnet.ts.net",
		},
		{
			"IP fallback",
			Peer{TailscaleIPs: []string{"100.64.0.1"}, HostName: "rover-vm"},
			"100.64.0.1",
		},
		{
			"HostName last resort",
			Peer{HostName: "rover-vm"},
			"rover-vm",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.peer.Target(); got != tt.want {
				t.Errorf("Target() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPingPeerArgsAcceptsRelayReachability(t *testing.T) {
	got := pingPeerArgs("rover-vm.tailnet.ts.net")
	want := []string{
		"ping",
		"--timeout=3s",
		"--c",
		"1",
		"--until-direct=false",
		"rover-vm.tailnet.ts.net",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pingPeerArgs() = %v, want %v", got, want)
	}
}

func TestDeviceID(t *testing.T) {
	d := Device{ID: "old-id", NodeID: "node-123"}
	if got := d.DeviceID(); got != "node-123" {
		t.Errorf("DeviceID() = %q, want node-123 (NodeID preferred)", got)
	}
	d2 := Device{ID: "old-id"}
	if got := d2.DeviceID(); got != "old-id" {
		t.Errorf("DeviceID() = %q, want old-id (ID fallback)", got)
	}
}

func TestDisplayName(t *testing.T) {
	tests := []struct {
		name string
		d    Device
		want string
	}{
		{"Name preferred", Device{Name: "machine.tailnet.ts.net", Hostname: "machine"}, "machine.tailnet.ts.net"},
		{"Hostname fallback", Device{Hostname: "machine"}, "machine"},
		{"ID last resort", Device{ID: "abc123"}, "abc123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.DisplayName(); got != tt.want {
				t.Errorf("DisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatchesPeer(t *testing.T) {
	tests := []struct {
		name string
		peer *Peer
		want string
		m    bool
	}{
		{"nil peer", nil, "rover-vm", false},
		{"hostname match", &Peer{HostName: "rover-vm"}, "rover-vm", true},
		{"hostname case insensitive", &Peer{HostName: "Rover-VM"}, "rover-vm", true},
		{"DNS prefix match", &Peer{DNSName: "rover-vm.tailnet.ts.net."}, "rover-vm", true},
		{"no match", &Peer{HostName: "other-vm"}, "rover-vm", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesPeer(tt.peer, tt.want); got != tt.m {
				t.Errorf("matchesPeer() = %v, want %v", got, tt.m)
			}
		})
	}
}

func TestMatchesDevice(t *testing.T) {
	tags := map[string]bool{"tag:rover": true}
	tests := []struct {
		name     string
		d        Device
		hostname string
		m        bool
	}{
		{"external excluded", Device{IsExternal: true}, "rover-vm", false},
		{"hostname match", Device{Hostname: "rover-vm", Tags: []string{"tag:rover"}}, "rover-vm", true},
		{"hostname prefix", Device{Hostname: "rover-vm-2", Tags: []string{"tag:rover"}}, "rover-vm", true},
		{"name prefix", Device{Name: "rover-vm.tailnet.ts.net", Tags: []string{"tag:rover"}}, "rover-vm", true},
		{"no matching tags", Device{Hostname: "rover-vm", Tags: []string{"tag:other"}}, "rover-vm", false},
		{"no match", Device{Hostname: "other-vm", Tags: []string{"tag:rover"}}, "rover-vm", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesDevice(tt.d, tt.hostname, tags); got != tt.m {
				t.Errorf("matchesDevice() = %v, want %v", got, tt.m)
			}
		})
	}
}

func TestPeerNotFoundError(t *testing.T) {
	err := &PeerNotFoundError{Host: "test-vm"}
	if got := err.Error(); got != `"test-vm" is not in your tailnet` {
		t.Errorf("Error() = %q", got)
	}
}
