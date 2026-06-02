// Package config owns Rover's local state/config file — the single, explicit
// source of truth for the one Rover-managed VM. No implicit magic: everything
// Rover knows lives in this JSON file.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

// Connection holds the last known reachability info for the VM. It is a cache
// for display/convenience; live Azure queries remain authoritative.
type Connection struct {
	Exists     bool   `json:"exists"`
	PowerState string `json:"powerState,omitempty"`
	VMSize     string `json:"vmSize,omitempty"`
	PublicIP   string `json:"publicIp,omitempty"`
	FQDN       string `json:"fqdn,omitempty"`
	PrivateIP  string `json:"privateIp,omitempty"`
	SSHTarget  string `json:"sshTarget,omitempty"`
}

// State is the persisted Rover configuration + last-known runtime state.
type State struct {
	Subscription   string     `json:"subscription,omitempty"`
	ResourceGroup  string     `json:"resourceGroup"`
	Location       string     `json:"location"`
	VMName         string     `json:"vmName"`
	Size           string     `json:"size"`
	AdminUsername  string     `json:"adminUsername"`
	SSHPublicKey   string     `json:"sshPublicKey"`
	SSHPrivateKey  string     `json:"sshPrivateKey,omitempty"`
	Connection     Connection `json:"connection,omitempty"`
	AnsibleApplied bool       `json:"ansibleApplied"`

	// Tailscale (optional). The auth key is never stored here — it is read from
	// the TS_AUTHKEY environment variable at provision time.
	TailscaleHostname string `json:"tailscaleHostname,omitempty"`
	TailscaleTags     string `json:"tailscaleTags,omitempty"`
}

// TSHostname returns the Tailscale node name, defaulting to the VM name.
func (s *State) TSHostname() string {
	if s.TailscaleHostname != "" {
		return s.TailscaleHostname
	}
	return s.VMName
}

// TSTags returns the Tailscale tags, defaulting to tag:rover.
func (s *State) TSTags() string {
	if s.TailscaleTags != "" {
		return s.TailscaleTags
	}
	return "tag:rover"
}

// Default returns a State pre-populated with sane defaults for a first run.
func Default() *State {
	home, _ := os.UserHomeDir()
	admin := "rover"
	if u, err := user.Current(); err == nil && u.Username != "" {
		admin = u.Username
	}
	return &State{
		ResourceGroup: "rover-rg",
		Location:      "australiaeast",
		VMName:        "rover-vm",
		Size:          "small",
		AdminUsername: admin,
		SSHPublicKey:  filepath.Join(home, ".ssh", "id_rsa.pub"),
		TailscaleTags: "tag:rover",
	}
}

// Path returns the location of the state file (honours XDG via UserConfigDir).
func Path() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "rover", "state.json"), nil
}

// Load reads the state file, returning defaults if it does not exist yet.
func Load() (*State, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return nil, err
	}
	st := Default()
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return st, nil
}

// Save writes the state file, creating parent directories as needed.
func (s *State) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o644)
}

// PrivateKeyPath returns the configured private key, deriving it from the
// public key path (strip .pub) when not set explicitly.
func (s *State) PrivateKeyPath() string {
	if s.SSHPrivateKey != "" {
		return s.SSHPrivateKey
	}
	if len(s.SSHPublicKey) > 4 && s.SSHPublicKey[len(s.SSHPublicKey)-4:] == ".pub" {
		return s.SSHPublicKey[:len(s.SSHPublicKey)-4]
	}
	return s.SSHPublicKey
}

// Env renders the state as ROVER_* environment variables for the shell scripts,
// appended onto the current process environment.
func (s *State) Env() []string {
	env := os.Environ()
	add := func(k, v string) {
		if v != "" {
			env = append(env, k+"="+v)
		}
	}
	add("ROVER_RESOURCE_GROUP", s.ResourceGroup)
	add("ROVER_LOCATION", s.Location)
	add("ROVER_VM_NAME", s.VMName)
	add("ROVER_ADMIN_USER", s.AdminUsername)
	add("ROVER_SSH_PUBKEY", s.SSHPublicKey)
	add("ROVER_SSH_KEY", s.PrivateKeyPath())
	add("ROVER_SUBSCRIPTION", s.Subscription)
	return env
}
